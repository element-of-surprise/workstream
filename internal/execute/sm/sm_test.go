package sm

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/element-of-surprise/workstream/internal/execute/sm/testing/plugins"
	"github.com/element-of-surprise/workstream/plugins/registry"
	"github.com/element-of-surprise/workstream/workflow"
	"github.com/element-of-surprise/workstream/workflow/builder"
	"github.com/gostdlib/ops/statemachine"
)

func TestBlockPreChecks(t *testing.T) {
	states := &States{}

	tests := []struct {
		name string
		block *workflow.Block
		checksRunner checksRunner
		wantBlockStatus workflow.Status
		wantNextState statemachine.State[Data]
	}{
		{
			name: "PreChecks and ContChecks are nil",
			block: &workflow.Block{},
			wantNextState: states.BlockStartContChecks,
		},
		{
			name: "PreChecks and ContChecks succeed",
			block: &workflow.Block{
				PreChecks: &workflow.Checks{},
				ContChecks: &workflow.Checks{},
			},
			checksRunner: func(ctx context.Context, checks *workflow.Checks) error {
				return nil
			},
			wantNextState: states.BlockStartContChecks,
		},
		{
			name: "PreChecks or ContChecks fail",
			block: &workflow.Block{
				PreChecks: &workflow.Checks{},
				ContChecks: &workflow.Checks{},
			},
			checksRunner: func(ctx context.Context, checks *workflow.Checks) error {
				return fmt.Errorf("error")
			},
			wantBlockStatus: workflow.Failed,
			wantNextState: states.BlockEnd,
		},
	}

	for _, test := range tests {
		req := statemachine.Request[Data]{
			Ctx: context.Background(),
			Data: Data{
				blocks: []block{{block: test.block}},
			},
		}
		test.block.State = &workflow.State{}

		states := &States{store: &fakeUpdater{}, checksRunner: test.checksRunner}
		req = states.BlockPreChecks(req)
		if test.wantBlockStatus != workflow.NotStarted {
			if req.Data.err == nil {
				t.Errorf("TestBlockPreChecks(%s): req.Data.err = nil, want error", test.name)
			}
			if req.Data.blocks[0].block.State.Status != test.wantBlockStatus {
				t.Errorf("TestBlockPreChecks(%s): got block status = %v, want %v", test.name, req.Data.blocks[0].block.State.Status, test.wantBlockStatus)
			}
		}
		if methodName(req.Next) != methodName(test.wantNextState) {
			t.Errorf("TestBlockPreChecks(%s): got next state = %v, want %v", test.name, methodName(req.Next), methodName(test.wantNextState))
		}
	}
}

func TestBlockStartContChecks(t *testing.T) {
	tests := []struct {
		name string
		action *workflow.Action
	}{
		{
			name: "ContChecks == nil",
		},
		{
			name: "ContChecks != nil",
			action: &workflow.Action{
				Plugin: plugins.Name,
				// This error forces a response on a channel that let's us know the action was executed.
				Req: plugins.Req{Arg: "error"},
				State: &workflow.State{},
			},
		},
	}

	plug := &plugins.Plugin{AlwaysRespond: true, IsCheckPlugin: true}
   	reg := registry.New()
    reg.Register(plug)

	for _, test := range tests {
		plug.ResetCounts()

		states := &States{store: &fakeUpdater{}}

		var contChecks *workflow.Checks
		if test.action != nil {
			contChecks = &workflow.Checks{
				Actions: []*workflow.Action{test.action},
				State: &workflow.State{},
			}
		}

		req := statemachine.Request[Data]{
			Ctx: context.Background(),
			Data: Data{
				blocks: []block{
					{
						block: &workflow.Block{
							ContChecks: contChecks,
						},
						contCheckResult: make(chan error, 1),
					},
				},
			},
		}

		req = states.BlockStartContChecks(req)
		if test.action != nil {
			<-req.Data.blocks[0].contCheckResult
		}
		if methodName(req.Next) != methodName(states.ExecuteSequences) {
			t.Errorf("TestBlockStartContChecks(%s): got req.Next == %s, want req.Next == %s", test.name, methodName(req.Next), methodName(states.ExecuteSequences))
		}
	}
}

// TestExecuteSequences tests ExecuteSequences in a variety of scenarios with a concurrency of 1.
// We tests concurrency for this in TestExecuteConcurrentSequences.
func TestExecuteSequences(t *testing.T) {
	failedAction := &workflow.Action{Plugin: plugins.Name, Timeout: 10 * time.Second, Req: plugins.Req{Sleep: 10 * time.Millisecond, Arg: "error"}}
	sequenceWithFailure := &workflow.Sequence{Actions: []*workflow.Action{failedAction}}

	successAction := &workflow.Action{Plugin: plugins.Name, Timeout: 10 * time.Second, Req: plugins.Req{Sleep: 10 * time.Millisecond, Arg: "success"}}
	sequenceWithSuccess := &workflow.Sequence{Actions: []*workflow.Action{successAction}}

	tests := []struct{
		name string
		block *workflow.Block
		contCheckFail bool
		wantPluginCalls int
		wantStatus workflow.Status
		wantErr bool
	}{
		{
			name: "Success: Tolerated Failures are unlimited, everything fails",
			block: &workflow.Block{
				ToleratedFailures: -1,
				Concurrency: 1,
				Sequences: []*workflow.Sequence{
					sequenceWithFailure.Clone(),
					sequenceWithFailure.Clone(),
					sequenceWithFailure.Clone(),
				},
			},
			wantPluginCalls: 3,
		},
		{
			name: "Error: Exceed tolerated failures by after success 1",
			block: &workflow.Block{
				ToleratedFailures: 1,
				Concurrency: 1,
				Sequences: []*workflow.Sequence{
					sequenceWithFailure.Clone(),
					sequenceWithSuccess.Clone(),
					sequenceWithFailure.Clone(),
				},
			},
			wantPluginCalls: 3,
			wantStatus: workflow.Failed,
			wantErr: true,
		},
		{
			name: "Error: Exceed tolerated failures before success",
			block: &workflow.Block{
				ToleratedFailures: 1,
				Concurrency: 1,
				Sequences: []*workflow.Sequence{
					sequenceWithFailure.Clone(),
					sequenceWithFailure.Clone(), // We should die after this.
					sequenceWithSuccess.Clone(), // Never should be called.
				},
			},
			wantPluginCalls: 2,
			wantStatus: workflow.Failed,
			wantErr: true,
		},
		{
			name: "Error: Continuous Checks fail",
			block: &workflow.Block{
				ToleratedFailures: 1,
				Concurrency: 1,
				Sequences: []*workflow.Sequence{
					sequenceWithSuccess.Clone(), // Never should be called.
				},
			},
			contCheckFail: true,
			wantStatus: workflow.Failed,
			wantErr: true,
		},
		{
			name: "Success",
			block: &workflow.Block{
				ToleratedFailures: 0,
				Concurrency: 1,
				Sequences: []*workflow.Sequence{
					sequenceWithSuccess.Clone(),
					sequenceWithSuccess.Clone(),
					sequenceWithSuccess.Clone(),
				},
			},
			wantPluginCalls: 3,
		},
	}

	plug := &plugins.Plugin{AlwaysRespond: true}
   	reg := registry.New()
    reg.Register(plug)


	for _, test := range tests {
		plug.ResetCounts()

		states := States{
    		registry: reg,
      		store: &fakeUpdater{},
    	}

     	req := statemachine.Request[Data]{
      	 		Ctx: context.Background(),
      	}
       	req.Data.blocks = []block{{block: test.block}}
        test.block.State = &workflow.State{}
        if test.contCheckFail {
        	req.Data.contCheckResult = make(chan error, 1)
         	req.Data.contCheckResult <- fmt.Errorf("error")
          	close(req.Data.contCheckResult)
        }

	    for _, seq := range test.block.Sequences {
		    seq.State = &workflow.State{}
		    for _, action := range seq.Actions {
				action.State = &workflow.State{}
		  	}
	    }
     	req = states.ExecuteSequences(req)
      	if test.wantErr != (req.Data.err != nil) {
        	t.Errorf("TestExecuteSequences(%s): got err == %v, wantErr == %v", test.name, req.Data.err, test.wantErr)
	  	}
		if test.wantStatus != test.block.State.Status {
			t.Errorf("TestExecuteSequences(%s): got status == %v, wantStatus == %v", test.name, test.block.State.Status, test.wantStatus)
		}
		if plug.Calls.Load() != int64(test.wantPluginCalls) {
			t.Errorf("TestExecuteSequences(%s): got plugin calls == %v, want == %v", test.name, plug.Calls.Load(), test.wantPluginCalls)
		}
	}
}

// TestExecuteSequencesConcurrency test the concurrency limits for blocks to make sure it works.
func TestExecuteSequencesConcurrency(t *testing.T) {
	t.Parallel()

 	build, err := builder.New("test", "test")
    if err != nil {
    	panic(err)
    }

  	build.AddBlock(
   		builder.BlockArgs{
     		Name: "block0",
       		Descr: "block0",
         	Concurrency: 3,
     	},
   )

   for i := 0; i < 10; i++ {
   		build.AddSequence(
     		&workflow.Sequence{
       			Name: "seq",
          		Descr: "seq",
       		},
     	)
     	build.AddAction(
      		&workflow.Action{
        		Name: "action",
          		Descr: "action",
            	Plugin: plugins.Name,
             	Timeout: 10 * time.Second,
              	Req: plugins.Req{Sleep: 100 * time.Millisecond},
        	},
      	)
      	build.Up()
   }

   plug := &plugins.Plugin{AlwaysRespond: true}
   reg := registry.New()
   reg.Register(plug)

   states := States{
   		registry: reg,
     	store: &fakeUpdater{},
   }

   p, err := build.Plan()
   if err != nil {
   		panic(err)
   }

   for _, seq := range p.Blocks[0].Sequences {
   		seq.State = &workflow.State{}
     	for _, action := range seq.Actions {
      		action.State = &workflow.State{}
	  	}
   }

   req := statemachine.Request[Data]{
   		Ctx: context.Background(),
     	Data: Data{
      		Plan: p,
        	blocks: []block{{block: p.Blocks[0]}},
      	},
    }
   req = states.ExecuteSequences(req)

   if plug.MaxCount.Load() != 3 {
   		t.Errorf("TestExecuteSequencesConcurrency: expected MaxCount == 3, got %d", plug.MaxCount)
   }
}

func TestBlockPostChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name 	 string
		block block
		wantErr  bool
		wantStatus workflow.Status
	}{
		{
			name: "Success: No post checks",
			block: block{
				block: &workflow.Block{},
			},
			wantStatus: workflow.Running,
		},
		{
			name: "Error: PostChecks fail",
			block: block{
				block: &workflow.Block{
					PostChecks: &workflow.Checks{Actions: []*workflow.Action{{Name: "error"}}},
				},
			},
			wantStatus: workflow.Failed,
			wantErr: true,
		},
		{
			name: "Success: Post checks succeed",
			block: block{
				block: &workflow.Block{
					PostChecks: &workflow.Checks{Actions: []*workflow.Action{{Name: "success"}}},
				},
			},
			wantStatus: workflow.Running,
		},
	}

	for _, test := range tests {
		states := &States{
			checksRunner: fakeRunChecksOnce,
		}
		test.block.block.State = &workflow.State{Status: workflow.Running}

		req := statemachine.Request[Data]{
			Ctx: context.Background(),
			Data: Data{
				blocks: []block{test.block},
			},
		}

		req = states.BlockPostChecks(req)

		if test.wantErr != (req.Data.err != nil) {
			t.Errorf("TestBlockPostChecks(%s): got err == %v, want err == %v", test.name, req.Data.err, test.wantErr)
		}
		if req.Data.blocks[0].block.State.Status != test.wantStatus {
			t.Errorf("TestBlockPostChecks(%s): got status == %v, want status == %v", test.name, req.Data.blocks[0].block.State.Status, test.wantStatus)
		}
		if methodName(req.Next) != methodName(states.BlockEnd) {
			t.Errorf("TestBlockPostChecks(%s): got next == %v, want next == %v", test.name, methodName(req.Next), methodName(states.BlockEnd))
		}
	}
}

func TestBlockEnd(t *testing.T) {
	t.Parallel()

	// This is simply used to get the name next State we expect.
	// We create new ones in the tests to avoid having a shared one.
	states := &States{}

	tests := []struct {
		name     string
		data    Data
		contCheckResult error
		wantErr  bool
		wantBlockStatus workflow.Status
		wantNextState statemachine.State[Data]
		wantBlocksLen int
	}{
		{
			name: "Error: contchecks failure",
			data: Data{
				blocks: []block{{}},
			},
			contCheckResult: fmt.Errorf("error"),
			wantErr: true,
			wantBlockStatus: workflow.Failed,
			wantNextState: states.End,
			wantBlocksLen: 1,
		},
		{
			name: "Success: no more blocks",
			data: Data{
				blocks: []block{{}},
			},
			wantBlockStatus: workflow.Completed,
			wantNextState: states.ExecuteBlock,
			wantBlocksLen: 0,
		},
		{
			name: "Success: more blocks",
			data: Data{
				blocks: []block{{}, {}},
			},
			wantBlockStatus: workflow.Completed,
			wantNextState: states.ExecuteBlock,
			wantBlocksLen: 1,
		},
	}

	for _, test := range tests {
		states := &States{
			store: &fakeUpdater{},
		}
		for i, block := range test.data.blocks {
			block.block = &workflow.Block{State: &workflow.State{Status: workflow.Running}}
			test.data.blocks[i] = block
		}
		var ctx context.Context
		ctx, test.data.blocks[0].contCancel = context.WithCancel(context.Background())

		req := statemachine.Request[Data]{
			Ctx: context.Background(),
			Data: test.data,
		}

		req.Data.blocks[0].contCheckResult = make(chan error, 1)
		if test.contCheckResult != nil {
			req.Data.blocks[0].contCheckResult = make(chan error, 1)
			req.Data.blocks[0].contCheckResult <- test.contCheckResult
		}
		close(req.Data.blocks[0].contCheckResult)

		// We store this here because blocks is shrunk after the call.
		block := req.Data.blocks[0].block

		req = states.BlockEnd(req)

		if test.wantErr != (req.Data.err != nil) {
			t.Errorf("TestBlockEnd(%s): got err == %v, want err == %v", test.name, req.Data.err, test.wantErr)
		}
		if block.State.Status != test.wantBlockStatus {
			t.Errorf("TestBlockEnd(%s): got block status == %v, want block status == %v", test.name, block.State.Status, test.wantBlockStatus)
		}
		if methodName(req.Next) != methodName(test.wantNextState) {
			t.Errorf("TestBlockEnd(%s): got next state == %v, want next state == %v", test.name, methodName(req.Next), methodName(test.wantNextState))
		}
		if len(req.Data.blocks) != test.wantBlocksLen {
			t.Errorf("TestBlockEnd(%s): got blocks len == %v, want blocks len == %v", test.name, len(req.Data.blocks), test.wantBlocksLen)
		}
		if ctx.Err() == nil {
			t.Errorf("TestBlockEnd(%s): context for continuous checks should have been cancelled", test.name)
		}
		if states.store.(*fakeUpdater).calls != 1 {
			t.Errorf("TestBlockEnd(%s): got store calls == %v, want store calls == 1", test.name, states.store.(*fakeUpdater).calls)
		}
	}
}

func TestPlanPostChecks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name 	 string
		plan 	 *workflow.Plan
		contCheckResult error
		wantErr  bool
	}{
		{
			name: "Success: No post checks",
			plan: &workflow.Plan{},
		},
		{
			name: "Error: Continuous checks fail",
			plan: &workflow.Plan{
				ContChecks: &workflow.Checks{},
			},
			contCheckResult:  fmt.Errorf("error"),
			wantErr: true,
		},
		{
			name: "Error: PostChecks fail",
			plan: &workflow.Plan{
				PostChecks: &workflow.Checks{Actions: []*workflow.Action{{Name: "error"}}},
			},
			wantErr: true,
		},
		{
			name: "Success: Cont and Post checks succeed",
			plan: &workflow.Plan{
				ContChecks: &workflow.Checks{},
				PostChecks: &workflow.Checks{Actions: []*workflow.Action{{Name: "success"}}},
			},
		},
	}

	for _, test := range tests {
		states := &States{
			checksRunner: fakeRunChecksOnce,
		}
		// We cancel a context for continuous checks that are running. This
		// is used to simulate that we signal the continuous checks to stop.
		ctx, cancel := context.WithCancel(context.Background())

		// Simulates that we are done waiting for the continuous checks.`
		var results chan error
		if test.plan.ContChecks != nil {
			results = make(chan error, 1)
			if test.contCheckResult != nil {
				results <- test.contCheckResult
			}
			close(results)
		}

		req := statemachine.Request[Data]{
			Ctx: context.Background(),
			Data: Data{
				Plan: test.plan,
				contCheckResult: results,
				contCancel: cancel,
			},
		}

		req = states.PlanPostChecks(req)

		if test.wantErr != (req.Data.err != nil) {
			t.Errorf("TestPlanPostChecks(%s): got err == %v, want err == %v", test.name, req.Data.err, test.wantErr)
		}
		if test.plan.ContChecks != nil {
			if ctx.Err() == nil {
				t.Errorf("TestPlanPostChecks(%s): continuous checks ctx.Err() == nil, want ctx.Err() != nil", test.name)
			}
		}
	}
}

// methodName returns the name of the method of the given value.
func methodName(method any) string {
	if method == nil {
		return "<nil>"
	}
	valueOf := reflect.ValueOf(method)
	switch valueOf.Kind() {
	case reflect.Func:
		return strings.TrimSuffix(strings.TrimSuffix(runtime.FuncForPC(valueOf.Pointer()).Name(), "-fm"), "[...]")
	default:
		return "<not a function>"
	}
}
