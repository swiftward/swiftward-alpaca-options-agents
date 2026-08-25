package account

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres keeps the line across restarts, which is the whole point of keeping
// it at all.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) Append(ctx context.Context, snapshot Snapshot) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO account_snapshots
		   (recorded_at, equity, equity_yesterday, cash, buying_power,
		    options_buying_power, position_market_value)
		 VALUES (@recorded_at, @equity, @equity_yesterday, @cash, @buying_power,
		         @options_buying_power, @position_market_value)
		 ON CONFLICT (recorded_at) DO NOTHING`,
		pgx.NamedArgs{
			"recorded_at": snapshot.RecordedAt, "equity": snapshot.Equity,
			"equity_yesterday": snapshot.EquityYesterday, "cash": snapshot.Cash,
			"buying_power":          snapshot.BuyingPower,
			"options_buying_power":  snapshot.OptionsBuyingPower,
			"position_market_value": snapshot.PositionMarketValue,
		})
	if err != nil {
		return fmt.Errorf("record the account: %w", err)
	}

	return nil
}

func (p *Postgres) Since(ctx context.Context, since time.Time) ([]Snapshot, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT recorded_at, equity, equity_yesterday, cash, buying_power,
		        options_buying_power, position_market_value
		   FROM account_snapshots
		  WHERE recorded_at >= @since
		  ORDER BY recorded_at`,
		pgx.NamedArgs{"since": since})
	if err != nil {
		return nil, fmt.Errorf("read the account history: %w", err)
	}
	defer rows.Close()

	snapshots := []Snapshot{}
	for rows.Next() {
		var snapshot Snapshot
		if err := rows.Scan(&snapshot.RecordedAt, &snapshot.Equity, &snapshot.EquityYesterday,
			&snapshot.Cash, &snapshot.BuyingPower, &snapshot.OptionsBuyingPower,
			&snapshot.PositionMarketValue); err != nil {
			return nil, fmt.Errorf("read an account snapshot: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the account history: %w", err)
	}

	return snapshots, nil
}
