// Package db opens the one connection pool the process uses. Everything that
// stores anything - the record of the agent's work, the volatility history -
// borrows it, so a restart opens one pool and not one per subject.
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open connects and checks the database answers, so a bad address is a startup
// error rather than a page that is empty for reasons nobody can see.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open the database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("reach the database: %w", err)
	}

	return pool, nil
}
