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

// AppendIntent writes an intent under the cause in force at that moment.
//
// The cause is resolved inside the same transaction as the insert rather than
// derived afterwards, and that is the whole point of the shape. Deriving it later
// would mean comparing the intent's stamp against the causes' stamps - two
// clocks, the database's and the harness's, and a tie whenever a cause lands in
// the same instant as the intent it caused.
func (p *Postgres) AppendIntent(ctx context.Context, intent Intent) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO intents (recorded_at, turn_ref, cause_id, answers, thesis, structure, max_loss, underlying_price, envelope_checked)
		 VALUES (@at, @turn,
		         COALESCE(@cause_id, (SELECT max(id) FROM turn_causes WHERE turn_ref = @turn)),
		         @answers, @thesis, @structure, @max_loss, @underlying_price, @envelope_checked)`,
		pgx.NamedArgs{
			"at": intent.At, "turn": nullable(intent.TurnRef),
			"cause_id": intent.CauseID, "answers": intent.Answers,
			"thesis": intent.Thesis, "structure": intent.Structure, "max_loss": intent.MaxLoss,
			// NULL rather than "": the column is numeric, and an empty string is
			// not a number - Postgres refuses the whole row with "invalid input
			// syntax for type numeric". The session that stated no price would
			// then be told its intent was not recorded, on the one call this
			// system asks it to make before every order.
			"underlying_price": nullable(intent.UnderlyingPrice),
			"envelope_checked": intent.EnvelopeChecked,
		})
	if err != nil {
		return fmt.Errorf("record the intent: %w", err)
	}

	return nil
}

// TurnStarted records a turn. The agent's own identifier is the key: a turn
// written twice is the same turn, not a second one.
func (p *Postgres) AppendSaid(ctx context.Context, said Said) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO said (turn_ref, at, text) VALUES (@turn, @at, @text)`,
		pgx.NamedArgs{"turn": said.TurnRef, "at": said.At, "text": said.Text})
	if err != nil {
		return fmt.Errorf("record what was said: %w", err)
	}

	return nil
}

func (p *Postgres) TurnStarted(ctx context.Context, turn Turn, wokenBy, cause string) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("record the turn: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx,
		`INSERT INTO turns (turn_ref, thread_ref, started_at, model)
		 VALUES (@ref, @thread, @started, @model)
		 ON CONFLICT (turn_ref) DO NOTHING`,
		pgx.NamedArgs{
			"ref": turn.Ref, "thread": turn.ThreadRef, "started": turn.StartedAt,
			"model": turn.Model,
		})
	if err != nil {
		return fmt.Errorf("record the turn: %w", err)
	}
	// The same turn written twice is one turn, and it keeps the cause it opened
	// with. Appending a second opening cause here would make a retry look like a
	// steer.
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO turn_causes (turn_ref, at, woken_by, cause)
		 VALUES (@ref, @at, @woken_by, @cause)`,
		pgx.NamedArgs{
			"ref": turn.Ref, "at": turn.StartedAt, "woken_by": wokenBy, "cause": cause,
		}); err != nil {
		return fmt.Errorf("record what opened the turn: %w", err)
	}

	return tx.Commit(ctx)
}

func (p *Postgres) AppendTurnCause(ctx context.Context, cause TurnCause) (int64, error) {
	var id int64
	if err := p.pool.QueryRow(ctx,
		`INSERT INTO turn_causes (turn_ref, at, woken_by, cause)
		 VALUES (@ref, @at, @woken_by, @cause) RETURNING id`,
		pgx.NamedArgs{
			"ref": cause.TurnRef, "at": cause.At,
			"woken_by": cause.WokenBy, "cause": cause.Cause,
		}).Scan(&id); err != nil {
		return 0, fmt.Errorf("record what was said into the turn: %w", err)
	}

	return id, nil
}

func (p *Postgres) CausesOfTurn(ctx context.Context, turnRef string) ([]TurnCause, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, turn_ref, at, woken_by, cause FROM turn_causes
		 WHERE turn_ref = @ref ORDER BY id`,
		pgx.NamedArgs{"ref": turnRef})
	if err != nil {
		return nil, fmt.Errorf("read what was put in front of the turn: %w", err)
	}
	defer rows.Close()

	var causes []TurnCause
	for rows.Next() {
		var c TurnCause
		if err := rows.Scan(&c.ID, &c.TurnRef, &c.At, &c.WokenBy, &c.Cause); err != nil {
			return nil, fmt.Errorf("read what was put in front of the turn: %w", err)
		}
		causes = append(causes, c)
	}

	return causes, rows.Err()
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

func (p *Postgres) AppendExecutionStep(ctx context.Context, step ExecutionStep) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO execution_steps (order_ref, at, action, was, became, showing, floor, replaced_by)
		 VALUES (@order, @at, @action, @was, @became, @showing, @floor, @replaced_by)`,
		pgx.NamedArgs{
			"order": step.OrderRef, "at": step.At, "action": step.Action,
			"was": step.Was, "became": step.Became,
			"showing": step.Showing, "floor": step.Floor,
			"replaced_by": step.ReplacedBy,
		})
	if err != nil {
		return fmt.Errorf("record the execution step on %s: %w", step.OrderRef, err)
	}

	return nil
}

func (p *Postgres) NoteFill(ctx context.Context, step ExecutionStep) (bool, error) {
	tag, err := p.pool.Exec(ctx,
		`INSERT INTO execution_steps (order_ref, at, action, was, became, quantity)
		 VALUES (@order, @at, 'filled', @was, @became, @quantity)
		 ON CONFLICT DO NOTHING`,
		pgx.NamedArgs{
			"order": step.OrderRef, "at": step.At,
			"was": step.Was, "became": step.Became, "quantity": step.Quantity,
		})
	if err != nil {
		return false, fmt.Errorf("record the fill on %s: %w", step.OrderRef, err)
	}

	return tag.RowsAffected() == 1, nil
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

func (p *Postgres) CallFinished(ctx context.Context, ref string, finishedAt time.Time, status, failure, answer string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tool_calls
		    SET finished_at = @finished, status = @status, failure = @failure, answer = @answer
		  WHERE call_ref = @ref`,
		pgx.NamedArgs{
			"finished": finishedAt, "status": status, "failure": nullable(failure),
			"answer": nullable(answer), "ref": ref,
		})
	if err != nil {
		return fmt.Errorf("close the call %s: %w", ref, err)
	}

	return nil
}

// LastRuns reads when each waker last had its task put in front of the agent.
// The harness asks at start, so a restart inside a session's window does not run
// that session twice.
//
// It counts causes rather than turns on purpose: a session that steered into a
// turn already running did run, and reading only the turns that a session OPENED
// would wake it a second time the same day.
func (p *Postgres) LastRuns(ctx context.Context, since time.Time) (map[string]time.Time, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT woken_by, max(at) FROM turn_causes
		  WHERE at >= @since GROUP BY woken_by`,
		pgx.NamedArgs{"since": since})
	if err != nil {
		return nil, fmt.Errorf("read when each session last ran: %w", err)
	}
	defer rows.Close()

	last := map[string]time.Time{}
	for rows.Next() {
		var wokenBy string
		var at time.Time
		if err := rows.Scan(&wokenBy, &at); err != nil {
			return nil, fmt.Errorf("read when a session last ran: %w", err)
		}
		last[wokenBy] = at
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read when each session last ran: %w", err)
	}

	return last, nil
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
	state := State{Turns: []Turn{}, Calls: []ToolCall{}, Steps: []ExecutionStep{}, Intents: []Intent{}, Said: []Said{}}

	turns, err := p.pool.Query(ctx,
		`SELECT turn_ref, thread_ref, started_at, finished_at, model, failure
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
			&turn.Model, &failure); err != nil {
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

	// Only the causes of the turns just read: the point of the limit is that a
	// session sees a recent window, and causes of turns outside it explain nothing
	// it can see.
	refs := make([]string, 0, len(state.Turns))
	for i := range state.Turns {
		refs = append(refs, state.Turns[i].Ref)
	}
	causes, err := p.pool.Query(ctx,
		`SELECT id, turn_ref, at, woken_by, cause FROM turn_causes
		  WHERE turn_ref = ANY(@refs) ORDER BY id`,
		pgx.NamedArgs{"refs": refs})
	if err != nil {
		return State{}, fmt.Errorf("read what was put in front of the turns: %w", err)
	}
	defer causes.Close()

	state.Causes = []TurnCause{}
	for causes.Next() {
		var c TurnCause
		if err := causes.Scan(&c.ID, &c.TurnRef, &c.At, &c.WokenBy, &c.Cause); err != nil {
			return State{}, fmt.Errorf("read what was put in front of a turn: %w", err)
		}
		state.Causes = append(state.Causes, c)
	}
	if err := causes.Err(); err != nil {
		return State{}, fmt.Errorf("read what was put in front of the turns: %w", err)
	}

	calls, err := p.pool.Query(ctx,
		`SELECT call_ref, turn_ref, server, tool, arguments, started_at, finished_at,
		        status, failure, answer
		   FROM tool_calls ORDER BY started_at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the calls: %w", err)
	}
	defer calls.Close()

	for calls.Next() {
		var call ToolCall
		var failure, answer *string
		if err := calls.Scan(&call.Ref, &call.TurnRef, &call.Server, &call.Tool, &call.Arguments,
			&call.StartedAt, &call.FinishedAt, &call.Status, &failure, &answer); err != nil {
			return State{}, fmt.Errorf("read a call: %w", err)
		}
		if failure != nil {
			call.Failure = *failure
		}
		if answer != nil {
			call.Answer = *answer
		}
		state.Calls = append(state.Calls, call)
	}
	if err := calls.Err(); err != nil {
		return State{}, fmt.Errorf("read the calls: %w", err)
	}

	steps, err := p.pool.Query(ctx,
		`SELECT order_ref, at, action, was, became, showing, floor, quantity, replaced_by
		   FROM execution_steps ORDER BY at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the execution steps: %w", err)
	}
	defer steps.Close()

	for steps.Next() {
		var step ExecutionStep
		if err := steps.Scan(&step.OrderRef, &step.At, &step.Action, &step.Was,
			&step.Became, &step.Showing, &step.Floor, &step.Quantity,
			&step.ReplacedBy); err != nil {
			return State{}, fmt.Errorf("read an execution step: %w", err)
		}
		state.Steps = append(state.Steps, step)
	}
	if err := steps.Err(); err != nil {
		return State{}, fmt.Errorf("read the execution steps: %w", err)
	}

	intents, err := p.pool.Query(ctx,
		`SELECT recorded_at, turn_ref, cause_id, answers, thesis, structure, max_loss,
		        underlying_price, envelope_checked
		   FROM intents ORDER BY recorded_at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read the intents: %w", err)
	}
	defer intents.Close()

	for intents.Next() {
		var intent Intent
		var turn *string
		var price *string
		if err := intents.Scan(&intent.At, &turn, &intent.CauseID, &intent.Answers,
			&intent.Thesis, &intent.Structure, &intent.MaxLoss, &price,
			&intent.EnvelopeChecked); err != nil {
			return State{}, fmt.Errorf("read an intent: %w", err)
		}
		if turn != nil {
			intent.TurnRef = *turn
		}
		// Rows written before the column existed have no price, and the window
		// that needs one is told by its absence rather than by a zero.
		if price != nil {
			intent.UnderlyingPrice = *price
		}
		state.Intents = append(state.Intents, intent)
	}
	if err := intents.Err(); err != nil {
		return State{}, fmt.Errorf("read the intents: %w", err)
	}

	// The agent's words. They were written from the start and nobody read them:
	// State built an empty list, no query was made, and the page honestly showed
	// zero lines against sixteen rows in the table. The turn headers were drawn all
	// the same, so the gap read as "the agent is silent" rather than "we did not
	// ask".
	said, err := p.pool.Query(ctx,
		`SELECT turn_ref, at, text
		   FROM said ORDER BY at DESC LIMIT @shows`,
		pgx.NamedArgs{"shows": p.shows})
	if err != nil {
		return State{}, fmt.Errorf("read what was said: %w", err)
	}
	defer said.Close()

	for said.Next() {
		var was Said
		if err := said.Scan(&was.TurnRef, &was.At, &was.Text); err != nil {
			return State{}, fmt.Errorf("read something said: %w", err)
		}
		state.Said = append(state.Said, was)
	}
	if err := said.Err(); err != nil {
		return State{}, fmt.Errorf("read what was said: %w", err)
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
