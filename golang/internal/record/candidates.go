package record

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
)

// ReplaceCandidates puts one sweep's findings in place of the last one.
//
// Replacing rather than appending is the point: a candidate is a price, and a
// price from the previous sweep is not history worth keeping beside a fresh one,
// it is a wrong answer sitting next to a right one. What the sweep found before
// is already in the log if anyone wants it.
func (p *Postgres) ReplaceCandidates(ctx context.Context, at time.Time, found []screener.Candidate) error {
	batch := &pgx.Batch{}
	batch.Queue(`DELETE FROM candidates`)
	for _, one := range found {
		batch.Queue(
			`INSERT INTO candidates (swept_at, underlying, kind, expiration,
			     short_symbol, long_symbol, short_strike, long_strike, underlying_price,
			     out_of_the_money_percent, credit, risk, credit_to_risk_percent,
			     cost, cost_share_percent, credit_after_cost, short_delta, edge_points, edge_from)
			 VALUES (@at, @underlying, @kind, @expiration,
			     @short, @long, @shortStrike, @longStrike, @price,
			     @out, @credit, @risk, @toRisk, @cost, @share, @net, @delta, @edge, @edgeFrom)`,
			pgx.NamedArgs{
				"at": at, "underlying": one.Underlying, "kind": one.Type,
				"expiration": one.Expiration, "short": one.Short, "long": one.Long,
				"shortStrike": one.ShortStrike, "longStrike": one.LongStrike,
				"price": one.Price, "out": one.OutOfTheMoney,
				"credit": one.Credit, "risk": one.Risk, "toRisk": one.CreditToRisk,
				"cost": one.Cost, "share": one.CostShare, "net": one.CreditAfterCost,
				"delta":    one.Delta,
				"edge":     one.Edge,
				"edgeFrom": one.EdgeFrom,
			})
	}

	if err := p.pool.SendBatch(ctx, batch).Close(); err != nil {
		return fmt.Errorf("replace the candidates: %w", err)
	}

	return nil
}

// Candidates reads the last sweep, richest first.
func (p *Postgres) Candidates(ctx context.Context, most int) ([]screener.Candidate, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT underlying, kind, expiration, short_symbol, long_symbol,
		        short_strike, long_strike, underlying_price, out_of_the_money_percent,
		        credit, risk, credit_to_risk_percent, cost, cost_share_percent,
		        credit_after_cost, short_delta, edge_points, edge_from
		   FROM candidates
		  ORDER BY edge_points DESC NULLS LAST, credit_to_risk_percent DESC
		  LIMIT @most`,
		pgx.NamedArgs{"most": most})
	if err != nil {
		return nil, fmt.Errorf("read the candidates: %w", err)
	}
	defer rows.Close()

	var found []screener.Candidate
	for rows.Next() {
		var one screener.Candidate
		if err := rows.Scan(&one.Underlying, &one.Type, &one.Expiration,
			&one.Short, &one.Long, &one.ShortStrike, &one.LongStrike,
			&one.Price, &one.OutOfTheMoney, &one.Credit, &one.Risk,
			&one.CreditToRisk, &one.Cost, &one.CostShare, &one.CreditAfterCost,
			&one.Delta, &one.Edge, &one.EdgeFrom); err != nil {
			return nil, fmt.Errorf("read a candidate: %w", err)
		}
		found = append(found, one)
	}

	return found, rows.Err()
}

// AskedInTurn reports whether a tool was called during one turn.
//
// The session lives as turns on ONE conversation, so a limit read on an early
// turn is still sitting in the model's context on a later one - and from inside,
// that is not a stale cache, it is memory. Asking the session not to cache is
// therefore useless: it does not think it is caching. What can be checked is
// whether the call happened in THIS turn, and the record already knows.
func (p *Postgres) AskedInTurn(ctx context.Context, turnRef, tool string) (bool, error) {
	var asked bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (
		     SELECT 1 FROM tool_calls
		      WHERE turn_ref = @turn AND tool = @tool AND status <> 'failed')`,
		pgx.NamedArgs{"turn": turnRef, "tool": tool}).Scan(&asked)
	if err != nil {
		return false, fmt.Errorf("ask whether %s was called in turn %s: %w", tool, turnRef, err)
	}

	return asked, nil
}
