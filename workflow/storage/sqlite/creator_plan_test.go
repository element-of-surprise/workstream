package sqlite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluglib "github.com/element-of-surprise/coercion/plugins"
	"github.com/element-of-surprise/coercion/plugins/registry"
	"github.com/element-of-surprise/coercion/workflow"
	"github.com/element-of-surprise/coercion/workflow/builder"
	"github.com/element-of-surprise/coercion/workflow/storage/sqlite/testing/plugins"
	"github.com/element-of-surprise/coercion/workflow/utils/clone"
	"github.com/element-of-surprise/coercion/workflow/utils/walk"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

var plan *workflow.Plan

type setters interface {
	SetID(uuid.UUID)
	SetState(*workflow.State)
}

func init() {
	ctx := context.Background()

	build, err := builder.New("test", "test", builder.WithGroupID(mustUUID()))
	if err != nil {
		panic(err)
	}

	checkAction1 := &workflow.Action{Name: "action", Descr: "action", Plugin: plugins.CheckPluginName, Req: nil}
	checkAction2 := &workflow.Action{Name: "action", Descr: "action", Plugin: plugins.CheckPluginName, Req: nil}
	checkAction3 := &workflow.Action{Name: "action", Descr: "action", Plugin: plugins.CheckPluginName, Req: nil}
	seqAction1 := &workflow.Action{
		Name:   "action",
		Descr:  "action",
		Plugin: plugins.HelloPluginName,
		Req:    plugins.HelloReq{Say: "hello"},
		Attempts: []*workflow.Attempt{
			{
				Err:   &pluglib.Error{Message: "internal error"},
				Start: time.Now().Add(-1 * time.Minute),
				End:   time.Now(),
			},
			{
				Resp:  plugins.HelloResp{Said: "hello"},
				Start: time.Now().Add(-1 * time.Second),
				End:   time.Now(),
			},
		},
	}

	build.AddChecks(builder.PreChecks, &workflow.Checks{})
	build.AddAction(clone.Action(ctx, checkAction1))
	build.Up()

	build.AddChecks(builder.ContChecks, &workflow.Checks{Delay: 32 * time.Second})
	build.AddAction(clone.Action(ctx, checkAction2))
	build.Up()

	build.AddChecks(builder.PostChecks, &workflow.Checks{})
	build.AddAction(clone.Action(ctx, checkAction3))
	build.Up()

	build.AddBlock(builder.BlockArgs{
		Name:              "block",
		Descr:             "block",
		EntranceDelay:     1 * time.Second,
		ExitDelay:         1 * time.Second,
		ToleratedFailures: 1,
		Concurrency:       1,
	})

	build.AddChecks(builder.PreChecks, &workflow.Checks{})
	build.AddAction(checkAction1)
	build.Up()

	build.AddChecks(builder.ContChecks, &workflow.Checks{Delay: 1 * time.Minute})
	build.AddAction(checkAction2)
	build.Up()

	build.AddChecks(builder.PostChecks, &workflow.Checks{})
	build.AddAction(checkAction3)
	build.Up()

	build.AddSequence(&workflow.Sequence{Name: "sequence", Descr: "sequence"})
	build.AddAction(seqAction1)
	build.Up()

	plan, err = build.Plan()
	if err != nil {
		panic(err)
	}

	for item := range walk.Plan(context.Background(), plan) {
		setter := item.Value.(setters)
		setter.SetID(mustUUID())
		setter.SetState(
			&workflow.State{
				Status: workflow.Running,
				Start:  time.Now(),
				End:    time.Now(),
			},
		)
	}
}

func mustUUID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return id
}

func dbSetup() (path string, pool *sqlitex.Pool, err error) {
	tmpDir := os.TempDir()
	id := uuid.New()
	path = filepath.Join(tmpDir, id.String())
	pool, err = sqlitex.NewPool(
		path,
		sqlitex.PoolOptions{
			Flags:    sqlite.OpenReadWrite | sqlite.OpenCreate,
			PoolSize: 1,
		},
	)
	if err != nil {
		return "", nil, err
	}

	conn, err := pool.Take(context.Background())
	if err != nil {
		return "", nil, err
	}
	defer pool.Put(conn)

	if err := createTables(context.Background(), conn); err != nil {
		return "", nil, err
	}

	return path, pool, nil
}

func TestCommitPlan(t *testing.T) {
	t.Parallel()

	path, pool, err := dbSetup()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(path)
	defer pool.Close()

	conn, err := pool.Take(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if err := commitPlan(context.Background(), conn, plan); err != nil {
		t.Fatal(err)
	}
	pool.Put(conn)

	reg := registry.New()
	reg.Register(&plugins.CheckPlugin{})
	reg.Register(&plugins.HelloPlugin{})

	// TODO(element-of-surprise): Add checks to verify the data in the database
	reader := &reader{
		pool: pool,
		reg:  reg,
	}

	storedPlan, err := reader.Read(context.Background(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}

	if diff := cmp.Diff(plan, storedPlan, cmp.AllowUnexported(workflow.Action{})); diff != "" {
		t.Fatalf("Read plan does not match the original plan: -want/+got:\n%s", diff)
	}
}
