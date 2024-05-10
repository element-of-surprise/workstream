package sm

import (
	"fmt"
	"log"

	"github.com/element-of-surprise/coercion/workflow"
	"github.com/gostdlib/ops/statemachine"
)

// finalStates is used to set the finalStates states on the Plan by examining the Plan's object states.
type finalStates struct{}

// start is simply the starting place for the statemachine. It does nothing.
func (f finalStates) start(req statemachine.Request[Data]) statemachine.Request[Data] {
	req.Next = f.planChecks
	return req
}

// planChecks looks through all the checks in the Plan and fails the Plan if any of the checks failed
// and records the failure reason.
func (f finalStates) planChecks(req statemachine.Request[Data]) statemachine.Request[Data] {
	plan := req.Data.Plan

	r, err := f.examineChecks([3]*workflow.Checks{plan.PreChecks, plan.ContChecks, plan.PostChecks})
	if err == nil {
		req.Next = f.blocks
		return req
	}
	plan.State.Status = workflow.Failed
	plan.Reason = r
	req.Err = err
	req.Next = f.end
	return req
}

// blocks checks the state of the block and fails the Plan if any of the blocks failed. If a block is not in a
// state we should be in, it generates an ErrInternalFailure.
func (f finalStates) blocks(req statemachine.Request[Data]) statemachine.Request[Data] {
	plan := req.Data.Plan
	for _, block := range req.Data.Plan.Blocks {
		switch block.State.Status {
		case workflow.Completed:
		case workflow.Failed:
			plan.State.Status = workflow.Failed
			plan.Reason = workflow.FRBlock
			req.Err = fmt.Errorf("block failure")
			return req
		default:
			plan.State.Status = workflow.Failed
			plan.Reason = workflow.FRBlock
			req.Err = fmt.Errorf("block End state reached in %s state, which is invalid: %w", block.State.Status, ErrInternalFailure)
			return req
		}
	}
	req.Next = f.end
	return req
}

// end records a Plan as Completed.
func (f finalStates) end(req statemachine.Request[Data]) statemachine.Request[Data] {
	plan := req.Data.Plan
	plan.State.Status = workflow.Completed
	return req
}

// examineChecks Pre/Cont/Post checks passed and returns a failure reason and an error if one of them failed.
// If nothing failed (or checks are nil) this returns workflow.FRUnknown and a nil error.
func (f finalStates) examineChecks(checks [3]*workflow.Checks) (workflow.FailureReason, error) {
	for i, check := range checks {
		if check == nil {
			continue
		}

		var t string
		var r workflow.FailureReason
		switch i {
		case 0:
			t = "PreChecks"
			r = workflow.FRPreCheck
		case 1:
			t = "ContChecks"
			r = workflow.FRContCheck
		case 2:
			t = "PostChecks"
			r = workflow.FRPostCheck
		}

		switch check.State.Status {
		case workflow.Completed:
			continue
		case workflow.Failed:
			return r, fmt.Errorf("%s failure", t)
		default:
			err := fmt.Errorf("plan End state reached with a %s in %s state, which is invalid: %w", t, check.State.Status, ErrInternalFailure)
			log.Println(err)
			return r, err
		}
	}
	return workflow.FRUnknown, nil
}
