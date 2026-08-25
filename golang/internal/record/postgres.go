package record

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres keeps the record where a restart cannot reach it. The page a judge
// opens reads the same rows the session wrote.
type Postgres struct {
	pool *pgxpool.Pool
	// shows is how many of each kind the page carries. The record is a week long
	// by the end; a page is read in one screen.
	shows int
}

// NewPostgres keeps the record in the database behind pool. shows bounds what
// the page carries.
func NewPostgres(pool *pgxpool.Pool, shows int) (*Postgres, error) {
	if shows <= 0 {
		return nil, fmt.Errorf("the record must show at least one row of each kind")
	}

	return &Postgres{pool: pool, shows: shows}, nil
}

func (p *Postgres) AppendIntent(ctx context.Context, intent Intent) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO intents (recorded_at, session, thesis, structure, max_loss)
		 VALUES (@at, @session, @thesis, @structure, @max_loss)`,
		pgx.NamedArgs{
			"at": intent.At, "session": intent.Session, "thesis": intent.Thesis,
			"structure": intent.Structure, "max_loss": intent.MaxLoss,
		})
	if err != nil {
		return fmt.Errorf("record the intent: %w", err)
	}

	return nil
}

func (p *Postgres) AppendRefusal(ctx context.Context, refusal Refusal) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO refusals (refused_at, boundary, detail) VALUES (@at, @boundary, @detail)`,
		pgx.NamedArgs{"at": refusal.At, "boundary": refusal.Boundary, "detail": refusal.Detail})
	if err != nil {
		return fmt.Errorf("record the refusal: %w", err)
	}

	return nil
}

// TurnStarted records a turn. The agent's own identifier is the key: a turn
// written twice is the same turn, not a second one.
func (p *Postgres) TurnStarted(ctx context.Context, turn Turn) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO turns (turn_ref, thread_ref, started_at, woken_by, cause, model)
		 VALUES (@ref, @thread, @started, @woken_by, @cause, @model)
		 ON CONFLICT (turn_ref) DO NOTHING`,
		pgx.NamedArgs{
			"ref": turn.Ref, "thread": turn.ThreadRef, "started": turn.StartedAt,
			"woken_by": turn.WokenBy, "cause": turn.Cause, "model": turn.Model,
		})
	if err != nil {
		return fmt.Errorf("record the turn: %w", err)
	}

	return nil
}

func (p *Postgres) TurnFinished(ctx context.Context, ref string, finishedAt time.Time, failure string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE turns SET finished_at = @finished, failure = @failure WHERE turn_ref = @ref`,
		pgx.NamedArgs{"finished": finishedAt, "failure": nullable(failure), "ref": ref})
	if err != nil {
		return fmt.Errorf("close the turn: %w", err)
	}

	return nil
}

func (p *Postgres) CallStarted(ctx context.Context, call ToolCall) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO tool_calls (call_ref, turn_ref, server, tool, arguments, started_at, status)
		 VALUES (@ref, @turn, @server, @tool, @arguments, @started, @status)
		 ON CONFLICT (call_ref) DO NOTHING`,
		pgx.NamedArgs{
			"ref": call.Ref, "turn": call.TurnRef, "server": call.Server, "tool": call.Tool,
			"arguments": nullableJSON(call.Arguments), "started": call.StartedAt,
			"status": call.Status,
		})
	if err != nil {
		return fmt.Errorf("record the call to %s: %w", call.Tool, err)
	}

	return nil
}

func (p *Postgres) CallFinished(ctx context.Context, ref string, finishedAt time.Time, status, failure string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tool_calls SET finished_at = @finished, status = @status, failure = @failure
		  WHERE call_ref = @ref`,
		pgx.NamedArgs{"finished": finishedAt, "status": status, "failure": nullable(failure), "ref": ref})
	if err != nil {
		return fmt.Errorf("close the call %s: %w", ref, err)
	}

	return nil
}

// CloseCallsLeftOpen marks what was in flight when a process died. An order in
// that state may or may not have reached the broker; the record says unknown
// rather than choosing.
func (p *Postgres) CloseCallsLeftOpen(ctx context.Context, at time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx,
		`UPDATE tool_calls SET finished_at = @finished, status = @status, failure = @failure
		  WHERE finished_at IS NULL`,
		pgx.NamedArgs{"finished": at, "status": StatusUnknown, "failure": RestartedFailure})
	if err != nil {
		return 0, fmt.Errorf("close the calls left open: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

// CloseTurnsLeftOpen closes every turn no process is running any more. Only the
// harness opens a turn, and only one harness runs at a time, so an open turn at
// startup belongs to a process that is gone.
func (p *Postgres) CloseTurnsLeftOpen(ctx context.Context, at time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx,
		`UPDATE turns SET finished_at = @finished, failure = @failure WHERE finished_at IS NULL`,
		pgx.NamedArgs{"finished": at, "failure": RestartedFailure})
	if err != nil {
		return 0, fmt.Errorf("close the turns left open: %w", err)
	}

	return int(tag.RowsAffected()), nil
}

func (p *Postgres) Read(ctx context.Context) (State, error) {
	state := State{Turns: []Turn{}, Calls: []ToolCall{}, Intents: []Intent{}, Refusals: []Refusal{}}

	turns, err := p.pool.Query(ctx,
		`SELECT turn_ref, thread_ref, started_at, finished_at, woken_by, cause, model, failure
		   FROM turns ORDER BY started_at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the turns: %w", err)
	}
	defer turns.Close()

	for turns.Next() {
		var turn Turn
		var failure *string
		if err := turns.Scan(&turn.Ref, &turn.ThreadRef, &turn.StartedAt, &turn.FinishedAt,
			&turn.WokenBy, &turn.Cause, &turn.Model, &failure); err != nil {
			return State{}, fmt.Errorf("read a turn: %w", err)
		}
		if failure != nil {
			turn.Failure = *failure
		}
		state.Turns = append(state.Turns, turn)
	}
	if err := turns.Err(); err != nil {
		return State{}, fmt.Errorf("read the turns: %w", err)
	}

	calls, err := p.pool.Query(ctx,
		`SELECT call_ref, turn_ref, server, tool, arguments, started_at, finished_at, status, failure
		   FROM tool_calls ORDER BY started_at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the calls: %w", err)
	}
	defer calls.Close()

	for calls.Next() {
		var call ToolCall
		var failure *string
		if err := calls.Scan(&call.Ref, &call.TurnRef, &call.Server, &call.Tool, &call.Arguments,
			&call.StartedAt, &call.FinishedAt, &call.Status, &failure); err != nil {
			return State{}, fmt.Errorf("read a call: %w", err)
		}
		if failure != nil {
			call.Failure = *failure
		}
		state.Calls = append(state.Calls, call)
	}
	if err := calls.Err(); err != nil {
		return State{}, fmt.Errorf("read the calls: %w", err)
	}

	intents, err := p.pool.Query(ctx,
		`SELECT recorded_at, session, thesis, structure, max_loss
		   FROM intents ORDER BY recorded_at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the intents: %w", err)
	}
	defer intents.Close()

	for intents.Next() {
		var intent Intent
		if err := intents.Scan(&intent.At, &intent.Session, &intent.Thesis, &intent.Structure, &intent.MaxLoss); err != nil {
			return State{}, fmt.Errorf("read an intent: %w", err)
		}
		state.Intents = append(state.Intents, intent)
	}
	if err := intents.Err(); err != nil {
		return State{}, fmt.Errorf("read the intents: %w", err)
	}

	refusals, err := p.pool.Query(ctx,
		`SELECT refused_at, boundary, detail FROM refusals ORDER BY refused_at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the refusals: %w", err)
	}
	defer refusals.Close()

	for refusals.Next() {
		var refusal Refusal
		if err := refusals.Scan(&refusal.At, &refusal.Boundary, &refusal.Detail); err != nil {
			return State{}, fmt.Errorf("read a refusal: %w", err)
		}
		state.Refusals = append(state.Refusals, refusal)
	}
	if err := refusals.Err(); err != nil {
		return State{}, fmt.Errorf("read the refusals: %w", err)
	}

	return state, nil
}

// nullable keeps an empty failure out of the column: absent means the turn ended
// well, and an empty string would read as a failure with no message.
func nullable(value string) any {
	if value == "" {
		return nil
	}

	return value
}

// nullableJSON keeps an absent argument list out of the column. A tool called
// with no arguments and a tool whose arguments were not reported are different
// facts, and "{}" would make them look the same.
func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}

	return []byte(raw)
}
