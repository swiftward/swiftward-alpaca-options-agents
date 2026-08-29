// Package dbtest gives a test a database of its own, carrying the schema the
// stack ships. It exists so that no test hand-writes a table: a copy of the
// schema drifts from the migrations and then proves the copy.
package dbtest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// Fresh creates a database named after the test's own moment, applies every
// migration in name order, and drops it when the test ends. It reads the server
// to connect to from DATABASE_URL and fails without one: a tier that skips
// itself reports success having proved nothing.
func Fresh(t *testing.T) string {
	t.Helper()

	admin := os.Getenv("DATABASE_URL")
	require.NotEmpty(t, admin, "DATABASE_URL: this tier has nothing to say without a database")

	ctx := context.Background()
	name := fmt.Sprintf("test_%d", time.Now().UnixNano())
	server, err := pgx.Connect(ctx, admin)
	require.NoError(t, err)
	defer func() { _ = server.Close(ctx) }()

	_, err = server.Exec(ctx, "CREATE DATABASE "+name)
	require.NoError(t, err)
	t.Cleanup(func() {
		drop, err := pgx.Connect(ctx, admin)
		require.NoError(t, err)
		defer func() { _ = drop.Close(ctx) }()
		_, err = drop.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)")
		require.NoError(t, err)
	})

	url := named(t, admin, name)
	fresh, err := pgx.Connect(ctx, url)
	require.NoError(t, err)
	defer func() { _ = fresh.Close(ctx) }()

	files, err := filepath.Glob(migrations())
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

// migrations finds them whether the tier runs from this checkout or from the
// stack, where the repository root is not above the module.
func migrations() string {
	if _, err := os.Stat("/postgres/migrations"); err == nil {
		return filepath.Join("/postgres", "migrations", "*.sql")
	}

	return filepath.Join("..", "..", "..", "postgres", "migrations", "*.sql")
}

// named points the same connection string at another database on that server.
func named(t *testing.T, url, name string) string {
	t.Helper()

	cut := strings.LastIndex(url, "/")
	require.Positive(t, cut, "DATABASE_URL names no database")
	rest := ""
	if query := strings.Index(url[cut:], "?"); query >= 0 {
		rest = url[cut+query:]
	}

	return url[:cut+1] + name + rest
}
