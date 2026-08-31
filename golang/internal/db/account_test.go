//go:build db

package db_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/db/dbtest"
)

// A record is of one account, and a process of another must not serve it.
//
// The separation between the two agents is the database name and nothing else.
// On 31 August two read-only pages were pointed at the first agent's database:
// each showed its own account's money beside the first agent's equity line and
// intents, and neither the page nor the log said the two were different accounts.
func TestARecordRefusesAProcessOfAnotherAccount(t *testing.T) {
	ctx := context.Background()
	pool, err := db.Open(ctx, dbtest.Fresh(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Nobody has claimed it yet, so a reader is let through - a page may be
	// started before its agent has ever run.
	require.NoError(t, db.Check(ctx, pool, "alpaca-agent-tikhon"),
		"a record naming no account cannot contradict anybody")

	require.NoError(t, db.Claim(ctx, pool, "alpaca-agent-1"))
	assert.NoError(t, db.Claim(ctx, pool, "alpaca-agent-1"), "the same account claims it again on every restart")
	assert.NoError(t, db.Check(ctx, pool, "alpaca-agent-1"), "and its own page reads it")

	// The case that was live: the page of a different account, pointed here.
	err = db.Check(ctx, pool, "alpaca-agent-tikhon")
	require.Error(t, err, "a page of another account must not serve this record")
	assert.Contains(t, err.Error(), "alpaca-agent-1", "the message names the account the record is of")
	assert.Contains(t, err.Error(), "DATABASE_URL", "and where to look")

	// And a second agent pointed at the first one's database does not take it
	// over, which would move the record from under the agent already keeping it.
	err = db.Claim(ctx, pool, "alpaca-agent-2")
	require.Error(t, err)
	assert.NoError(t, db.Check(ctx, pool, "alpaca-agent-1"), "the record is still of the account that claimed it")

	// A process that cannot say who it is may not do either.
	assert.Error(t, db.Claim(ctx, pool, ""))
	assert.Error(t, db.Check(ctx, pool, ""))
}
