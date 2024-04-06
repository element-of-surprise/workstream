package sqlite

import (
	"context"
	"fmt"
	"time"

	"github.com/element-of-surprise/workstream/workflow"
	"github.com/google/uuid"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

// fieldToBlocks converts the "$blocks" field in a sqlite row to a list of workflow.Blocks.
func (p *planReader) fieldToBlocks(ctx context.Context, conn *sqlite.Conn, stmt *sqlite.Stmt) ([]*workflow.Block, error) {
	ids, err := fieldToIDs("$blocks", stmt)
	if err != nil {
		return nil, fmt.Errorf("couldn't read plan block ids: %w", err)
	}

	blocks := make([]*workflow.Block, 0, len(ids))
	for _, id := range ids {
		block, err := p.fetchBlockByID(ctx, conn, id)
		if err != nil {
			return nil, fmt.Errorf("couldn't fetch block(%s)by id: %w", id, err)
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

// fetchBlockByID fetches a block by its id.
func (p *planReader) fetchBlockByID(ctx context.Context, conn *sqlite.Conn, id uuid.UUID) (*workflow.Block, error) {
	block := &workflow.Block{}
	do := func(conn *sqlite.Conn) (err error) {
		err = sqlitex.Execute(
			conn,
			fetchBlocksByID,
			&sqlitex.ExecOptions{
				Named: map[string]any{
					"$id": id,
				},
				ResultFunc: func(stmt *sqlite.Stmt) error {
					block, err = p.blockRowToBlock(ctx, conn, stmt)
					if err != nil {
						return fmt.Errorf("couldn't convert row to block: %w", err)
					}
					return nil
				},
			},
		)
		if err != nil {
			return fmt.Errorf("couldn't fetch block by id: %w", err)
		}
		return nil
	}

	if err := do(conn); err != nil {
		return nil, fmt.Errorf("couldn't fetch block by id: %w", err)
	}
	return block, nil
}

// blockRowToBlock converts a sqlite row to a workflow.Block.
func (p *planReader) blockRowToBlock(ctx context.Context, conn *sqlite.Conn, stmt *sqlite.Stmt) (*workflow.Block, error) {
	var err error
	b := &workflow.Block{}
	b.ID, err = fieldToID("$id", stmt)
	if err != nil {
		return nil, fmt.Errorf("couldn't read block id: %w", err)
	}
	b.Name = stmt.GetText("$name")
	b.Descr = stmt.GetText("$descr")
	b.State = &workflow.State{
		Status: workflow.Status(stmt.GetInt64("$state_status")),
		Start:  time.Unix(0, stmt.GetInt64("$state_start")),
		End:    time.Unix(0, stmt.GetInt64("$state_end")),
	}
	b.PreChecks, err = p.fieldToCheck(ctx, "$prechecks", conn, stmt)
	if err != nil {
		return nil, fmt.Errorf("couldn't read block prechecks: %w", err)
	}
	b.ContChecks, err = p.fieldToCheck(ctx, "$contchecks", conn, stmt)
	if err != nil {
		return nil, fmt.Errorf("couldn't read block contchecks: %w", err)
	}
	b.PostChecks, err = p.fieldToCheck(ctx, "$postchecks", conn, stmt)
	if err != nil {
		return nil, fmt.Errorf("couldn't read block postchecks: %w", err)
	}

	b.Sequences, err = p.fieldToSequences(ctx, conn, stmt)
	if err != nil {
		return nil, fmt.Errorf("couldn't read block sequences: %w", err)
	}

	return b, nil
}
