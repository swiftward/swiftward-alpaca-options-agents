package record

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// InFlightOrder is an order request that was in the air when a process died.
//
// The record says `unknown` about it rather than choosing, because for an order
// "we did not send it" and "we do not know" are different facts and only one of
// them is honest. Unknown is the right thing to write down and the wrong thing to
// leave written: the broker knows the answer, and every order this project sends
// carries a name of its own to ask by.
type InFlightOrder struct {
	CallRef   string
	TurnRef   string
	Name      string
	StartedAt time.Time
}

// OrdersLeftUnknown lists the order requests a dead process left unresolved,
// newest first, with the name each was sent under.
func (p *Postgres) OrdersLeftUnknown(ctx context.Context, since time.Time) ([]InFlightOrder, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT call_ref, turn_ref, arguments, started_at
		   FROM tool_calls
		  WHERE status = @unknown AND tool = 'place_option_order' AND started_at >= @since
		  ORDER BY started_at DESC`,
		pgx.NamedArgs{"unknown": StatusUnknown, "since": since})
	if err != nil {
		return nil, fmt.Errorf("read the orders left unknown: %w", err)
	}
	defer rows.Close()

	var found []InFlightOrder
	for rows.Next() {
		var one InFlightOrder
		var arguments []byte
		if err := rows.Scan(&one.CallRef, &one.TurnRef, &arguments, &one.StartedAt); err != nil {
			return nil, fmt.Errorf("read an order left unknown: %w", err)
		}

		// The name is what makes the question askable. Without one the request
		// cannot be matched to anything at the broker, and saying so is better
		// than guessing by time and symbol.
		var sent struct {
			ClientOrderID string `json:"client_order_id"`
		}
		if err := json.Unmarshal(arguments, &sent); err == nil {
			one.Name = sent.ClientOrderID
		}
		found = append(found, one)
	}

	return found, rows.Err()
}

// OrderResolved replaces `unknown` with what the broker turned out to know.
// Called only after asking the broker, never on a guess.
func (p *Postgres) OrderResolved(ctx context.Context, callRef, answer string) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE tool_calls SET status = 'completed', failure = NULL, answer = @answer
		  WHERE call_ref = @ref AND status = @unknown`,
		pgx.NamedArgs{"ref": callRef, "answer": answer, "unknown": StatusUnknown})
	if err != nil {
		return fmt.Errorf("resolve the order on call %s: %w", callRef, err)
	}

	return nil
}
