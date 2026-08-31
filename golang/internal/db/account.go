package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// One record holds one account. The separation is by database - `agents` and
// `agents_near` - and until the record said so out loud, a process pointed at the
// wrong one served another account's equity line and intents beside a money panel
// reading its own. Nothing disagreed visibly; the two panels simply told
// different stories about the same page.
//
// So the record carries the name of the account it is of, and a process compares
// its own name against it before it serves anything.

// Claim names this process's account as the one the record is of, and fails if
// the record already belongs to another. It is for the process that KEEPS the
// record: the first to run stamps the database.
func Claim(ctx context.Context, pool *pgxpool.Pool, account string) error {
	if account == "" {
		return errors.New("this process cannot say which account it is, so it may not keep a record")
	}

	var owner string
	err := pool.QueryRow(ctx,
		`INSERT INTO record_account (account) VALUES (@account)
		 ON CONFLICT (one_row) DO UPDATE SET account = record_account.account
		 RETURNING account`,
		pgx.NamedArgs{"account": account}).Scan(&owner)
	if err != nil {
		return fmt.Errorf("name the account this record is of: %w", err)
	}
	if owner != account {
		return mismatch(account, owner)
	}

	return nil
}

// Check refuses a record that is of another account. It is for a process that
// only READS - the page - which must never stamp a database it does not keep.
//
// A record naming no account yet is allowed through: a page can be started before
// its agent has ever run, and refusing there would cost a deployment more than the
// silence costs. The stamp appears the moment the agent starts, and every start
// after that is checked.
func Check(ctx context.Context, pool *pgxpool.Pool, account string) error {
	if account == "" {
		return errors.New("this process cannot say which account it is, so it may not read a record")
	}

	var owner string
	err := pool.QueryRow(ctx, `SELECT account FROM record_account`).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read which account this record is of: %w", err)
	}
	if owner != account {
		return mismatch(account, owner)
	}

	return nil
}

func mismatch(account, owner string) error {
	return fmt.Errorf(
		"this record is of %q and this process is %q: check DATABASE_URL, one account keeps one database",
		owner, account)
}
