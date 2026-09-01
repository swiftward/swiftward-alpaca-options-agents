package record

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
)

// RecordCandidates adds one sweep's findings to the ones already kept.
//
// A candidate is a price, so a price from an earlier sweep must never be offered
// beside a fresh one - but that is a rule for the READER, which takes the newest
// sweep and nothing else, not a reason to destroy what was measured. Kept, the
// sweeps are the only record we have of what the option book offered: the broker
// publishes no history of two-sided option quotes, so a change to the thresholds
// can be answered from these rows instead of from the next open market.
//
// PurgeCandidates bounds what this accumulates.
func (p *Postgres) RecordCandidates(ctx context.Context, at time.Time, found []screener.Candidate) error {
	batch := &pgx.Batch{}
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
		return fmt.Errorf("record the candidates: %w", err)
	}

	return nil
}

// Candidates reads the NEWEST sweep, richest first, and WHEN it was taken.
//
// The time is not decoration. Rows outlive the sweep that wrote them: if the
// screener stops, the table keeps its last answer for as long as the process
// lives, and a list an hour old looks exactly like one a minute old. Seven
// minutes was already enough to turn +7.5 points of edge into -7.2 on 26 August,
// so a reader that cannot see the age cannot judge what it is holding.
//
// A zero time means the table is empty - there is no sweep to date.
func (p *Postgres) Candidates(ctx context.Context, most int) ([]screener.Candidate, time.Time, error) {
	var takenAt time.Time
	if err := p.pool.QueryRow(ctx, `SELECT COALESCE(MAX(swept_at), 'epoch') FROM candidates`).
		Scan(&takenAt); err != nil {
		return nil, time.Time{}, fmt.Errorf("read when the sweep was taken: %w", err)
	}

	rows, err := p.pool.Query(ctx,
		`SELECT underlying, kind, expiration, short_symbol, long_symbol,
		        short_strike, long_strike, underlying_price, out_of_the_money_percent,
		        credit, risk, credit_to_risk_percent, cost, cost_share_percent,
		        credit_after_cost, short_delta, edge_points, edge_from
		   FROM candidates
		  WHERE swept_at = (SELECT MAX(swept_at) FROM candidates)
		  ORDER BY edge_points DESC NULLS LAST, credit_to_risk_percent DESC
		  LIMIT @most`,
		pgx.NamedArgs{"most": most})
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("read the candidates: %w", err)
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
			return nil, time.Time{}, fmt.Errorf("read a candidate: %w", err)
		}
		found = append(found, one)
	}

	return found, takenAt, rows.Err()
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

// TriedInTurn reports whether a tool was CALLED during one turn, answered or not.
//
// It is the difference between a service that is down and a session that skipped
// the step. Both leave the same absence in AskedInTurn, and only one of them is a
// reason to let anything through.
func (p *Postgres) TriedInTurn(ctx context.Context, turnRef, tool string) (bool, error) {
	var tried bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS (
		     SELECT 1 FROM tool_calls
		      WHERE turn_ref = @turn AND tool = @tool)`,
		pgx.NamedArgs{"turn": turnRef, "tool": tool}).Scan(&tried)
	if err != nil {
		return false, fmt.Errorf("ask whether %s was tried in turn %s: %w", tool, turnRef, err)
	}

	return tried, nil
}

// PurgeCandidates drops sweeps older than the given moment and says how many
// rows went.
//
// The caller owns the age: how long the sweeps are worth keeping is a decision
// an operator makes, not one this package can hold.
func (p *Postgres) PurgeCandidates(ctx context.Context, before time.Time) (int64, error) {
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM candidates WHERE swept_at < @before`,
		pgx.NamedArgs{"before": before})
	if err != nil {
		return 0, fmt.Errorf("purge the candidates: %w", err)
	}

	return tag.RowsAffected(), nil
}
