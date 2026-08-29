package volatility

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres keeps the series. It outlives every process that writes to it, which
// is the whole point: the history is worth what its length is.
type Postgres struct {
	pool *pgxpool.Pool
}

func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) Append(ctx context.Context, sample Sample) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO volatility_samples
		   (underlying, contract, recorded_at, expiration, strike, option_type,
		    implied_volatility, delta, bid, ask, underlying_price)
		 VALUES (@underlying, @contract, @recorded_at, @expiration, @strike, @option_type,
		         @implied_volatility, @delta, @bid, @ask, @underlying_price)
		 ON CONFLICT (contract, recorded_at) DO NOTHING`,
		pgx.NamedArgs{
			"underlying": sample.Underlying, "contract": sample.Contract,
			"recorded_at": sample.RecordedAt, "expiration": sample.Expiration,
			"strike": sample.Strike, "option_type": sample.OptionType,
			"implied_volatility": sample.ImpliedVolatility, "delta": sample.Delta,
			"bid": sample.Bid, "ask": sample.Ask, "underlying_price": sample.UnderlyingPrice,
		})
	if err != nil {
		return fmt.Errorf("record the volatility of %s: %w", sample.Contract, err)
	}

	return nil
}

func (p *Postgres) Summarise(ctx context.Context, underlying string, since time.Time) (Summary, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT recorded_at, implied_volatility
		   FROM volatility_samples
		  WHERE underlying = @underlying AND recorded_at >= @since
		  ORDER BY recorded_at`,
		pgx.NamedArgs{"underlying": underlying, "since": since})
	if err != nil {
		return Summary{}, fmt.Errorf("read the volatility of %s: %w", underlying, err)
	}
	defer rows.Close()

	var readings []Reading
	for rows.Next() {
		var reading Reading
		if err := rows.Scan(&reading.At, &reading.ImpliedVolatility); err != nil {
			return Summary{}, fmt.Errorf("read a volatility sample: %w", err)
		}
		readings = append(readings, reading)
	}
	if err := rows.Err(); err != nil {
		return Summary{}, fmt.Errorf("read the volatility of %s: %w", underlying, err)
	}

	return Summarise(underlying, since, readings), nil
}
