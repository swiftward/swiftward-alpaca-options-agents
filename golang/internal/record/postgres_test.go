//go:build db

package record

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The record is proved against a real Postgres, on its own database, under the
// migrations the stack applies. A pool the test built itself would prove that
// the test can write rows, not that the schema the stack ships accepts them.
func TestPostgresKeepsTheRecord(t *testing.T) {
	admin := os.Getenv("DATABASE_URL")
	require.NotEmpty(t, admin, "DATABASE_URL: this tier has nothing to say without a database")

	url := freshDatabase(t, admin)
	kept, err := Connect(context.Background(), url, 2)
	require.NoError(t, err)
	t.Cleanup(kept.Close)

	ctx := context.Background()
	started := time.Date(2026, 9, 3, 18, 20, 0, 0, time.UTC)
	turn := Turn{
		Ref: "turn-1", ThreadRef: "thread-1", StartedAt: started,
		WokenBy: "entry", Cause: "declaration: entry", Model: "gpt-5.6",
	}
	require.NoError(t, kept.TurnStarted(ctx, turn))
	require.NoError(t, kept.TurnStarted(ctx, turn), "the same turn twice is one row")
	require.NoError(t, kept.TurnFinished(ctx, "turn-1", started.Add(90*time.Second), ""))
	require.NoError(t, kept.AppendIntent(ctx, Intent{
		At: started.Add(time.Minute), Session: "entry",
		Thesis: "premium is rich into the close", Structure: "put spread on SPY expiring today",
		MaxLoss: "1% of capital",
	}))
	require.NoError(t, kept.AppendRefusal(ctx, Refusal{
		At: started.Add(2 * time.Minute), Boundary: "max_loss_per_position",
		Detail: "structure risks 1.4% of capital, ceiling is 1%",
	}))

	state, err := kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Turns, 1)
	assert.Equal(t, "entry", state.Turns[0].WokenBy)
	assert.Equal(t, started.UTC(), state.Turns[0].StartedAt.UTC())
	require.NotNil(t, state.Turns[0].FinishedAt)
	assert.Equal(t, started.Add(90*time.Second).UTC(), state.Turns[0].FinishedAt.UTC())
	require.Len(t, state.Intents, 1)
	assert.Equal(t, "1% of capital", state.Intents[0].MaxLoss)
	require.Len(t, state.Refusals, 1)
	assert.Equal(t, "max_loss_per_position", state.Refusals[0].Boundary)

	// The page carries the newest rows, and only as many as it was told to show.
	for i := range 3 {
		require.NoError(t, kept.TurnStarted(ctx, Turn{
			Ref: fmt.Sprintf("turn-later-%d", i), ThreadRef: "thread-1",
			StartedAt: started.Add(time.Duration(i+1) * time.Hour),
			WokenBy:   "clock", Cause: "declaration: defend",
		}))
	}
	state, err = kept.Read(ctx)
	require.NoError(t, err)
	require.Len(t, state.Turns, 2)
	assert.Equal(t, "turn-later-2", state.Turns[0].Ref)
	assert.Equal(t, "turn-later-1", state.Turns[1].Ref)

	// The three later turns were never closed: this is what a process dying
	// mid-turn leaves behind, and startup has to answer for it.
	left, err := kept.CloseTurnsLeftOpen(ctx, started.Add(4*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3, left)

	state, err = kept.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, state.Turns[0].FinishedAt)
	assert.Equal(t, RestartedFailure, state.Turns[0].Failure)

	again, err := kept.CloseTurnsLeftOpen(ctx, started.Add(5*time.Hour))
	require.NoError(t, err)
	assert.Zero(t, again, "a closed turn is not closed twice")
}

// freshDatabase gives the test a database of its own, carrying the schema from
// the migrations the stack ships, and takes it away afterwards.
func freshDatabase(t *testing.T, admin string) string {
	t.Helper()

	ctx := context.Background()
	name := fmt.Sprintf("record_test_%d", time.Now().UnixNano())
	server, err := pgx.Connect(ctx, admin)
	require.NoError(t, err)
	defer server.Close(ctx)

	_, err = server.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		drop, err := pgx.Connect(ctx, admin)
		require.NoError(t, err)
		defer drop.Close(ctx)
		_, err = drop.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		require.NoError(t, err)
	})

	url := replaceDatabase(t, admin, name)
	fresh, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	defer fresh.Close(ctx)

	files, err := filepath.Glob(migrationsDir())
	require.NoError(t, err)
	require.NotEmpty(t, files, "no migrations found: the tier would prove nothing")
	for _, file := range files {
		sql, err := os.ReadFile(file)
		require.NoError(t, err)
		_, err = fresh.Exec(ctx, string(sql))
		require.NoError(t, err, file)
	}

	return url
}

// migrationsDir finds the migrations whether the tier runs from this checkout
// or from the stack, where the repository root is not above the module.
func migrationsDir() string {
	if _, err := os.Stat("/postgres/migrations"); err == nil {
		return filepath.Join("/postgres", "migrations", "*.sql")
	}

	return filepath.Join("..", "..", "..", "postgres", "migrations", "*.sql")
}

func replaceDatabase(t *testing.T, url, name string) string {
	t.Helper()

	cut := strings.LastIndex(url, "/")
	require.Positive(t, cut, "DATABASE_URL names no database")
	rest := ""
	if query := strings.Index(url[cut:], "?"); query >= 0 {
		rest = url[cut+query:]
	}

	return url[:cut+1] + name + rest
}
