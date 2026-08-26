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
			     cost, cost_share_percent)
			 VALUES (@at, @underlying, @kind, @expiration,
			     @short, @long, @shortStrike, @longStrike, @price,
			     @out, @credit, @risk, @toRisk, @cost, @share)`,
			pgx.NamedArgs{
				"at": at, "underlying": one.Underlying, "kind": one.Type,
				"expiration": one.Expiration, "short": one.Short, "long": one.Long,
				"shortStrike": one.ShortStrike, "longStrike": one.LongStrike,
				"price": one.Price, "out": one.OutOfTheMoney,
				"credit": one.Credit, "risk": one.Risk, "toRisk": one.CreditToRisk,
				"cost": one.Cost, "share": one.CostShare,
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
		        credit, risk, credit_to_risk_percent, cost, cost_share_percent
		   FROM candidates ORDER BY credit_to_risk_percent DESC LIMIT @most`,
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
			&one.CreditToRisk, &one.Cost, &one.CostShare); err != nil {
			return nil, fmt.Errorf("read a candidate: %w", err)
		}
		found = append(found, one)
	}

	return found, rows.Err()
}
