// Reconcile answers one question with a number: does our record hold every
// order the broker holds?
//
// It is a REPORT, never part of the trading path: it reads the broker and reads
// the database, and it writes nothing to either.
//
// Why reconciliation rather than interception. Evidence of what passed through
// the gateway says nothing about whether everything passed through it - an
// order sent around the gateway is absent from the evidence and from the
// question alike. The broker's own list cannot be gone around: it is the
// account, and the account id is what a judge is given. Comparing the two is
// the only claim about completeness that can be checked by somebody who does
// not trust us.
package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// ordersRead is how many of the broker's orders to ask for. The busiest day
// measured held 225 order ids on one account, and the ladder mints a new one on
// every price move, so this is generous rather than tuned.
const ordersRead = 500

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "reconcile:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	brokerURL := os.Getenv("BROKER_MCP_URL")
	if brokerURL == "" {
		return fmt.Errorf("BROKER_MCP_URL is empty: there is nothing to reconcile against")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is empty: there is no record to reconcile")
	}

	broker := marketdata.NewBrokerWithToken(brokerURL, os.Getenv("BROKER_MCP_TOKEN")).
		ActingFor(os.Getenv("USER_HEADER_NAME"), os.Getenv("USER_TOKEN"))

	orders, err := broker.Orders(ctx, ordersRead)
	if err != nil {
		return fmt.Errorf("read the broker's orders: %w", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("open the record: %w", err)
	}
	defer pool.Close()

	known, err := whatTheRecordHolds(ctx, pool)
	if err != nil {
		return err
	}

	// A window back from now, not a calendar day. A US session runs 13:30-20:00
	// UTC, so at any hour after midnight "today" holds no orders while the
	// session that matters was yesterday - the first run of this said "0 of 0"
	// and meant nothing. RECONCILE_SINCE overrides it.
	window := 24 * time.Hour
	if raw := os.Getenv("RECONCILE_SINCE"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("RECONCILE_SINCE %q: %w", raw, err)
		}
		window = parsed
	}
	since := time.Now().UTC().Add(-window)

	var missing []marketdata.Order
	counted := 0
	for _, order := range orders {
		if order.SubmittedAt == nil || order.SubmittedAt.UTC().Before(since) {
			continue
		}
		counted++
		if !known[order.ID] {
			missing = append(missing, order)
		}
	}

	sort.Slice(missing, func(i, j int) bool {
		return missing[i].SubmittedAt.Before(*missing[j].SubmittedAt)
	})

	// Which record was read, and how much it holds. Without this a zero reads as
	// "the record lost everything" when it more often means the wrong database:
	// run through the `tests` service, this compares the live account against an
	// empty test schema and reports every order as a hole. Measured on 29 August,
	// by me, for several minutes.
	fmt.Printf("record: %s, holding %d order references\n", recordName(databaseURL), len(known))
	fmt.Printf("%d of %d orders the broker holds for the last %s are in our record\n",
		counted-len(missing), counted, window)

	if len(missing) == 0 {
		return nil
	}
	if len(known) == 0 && counted > 0 {
		fmt.Printf("\nThe record holds no order references at all while the broker holds %d.\n"+
			"That is what an empty or wrong database looks like; a record that lost orders\n"+
			"holds some. Check DATABASE_URL before reading the list below as holes.\n", counted)
	}

	fmt.Printf("\n%d are not, and each one is a hole in the record:\n", len(missing))
	for _, order := range missing {
		fmt.Printf("  %s  %s  %s  qty %.0f  %s\n",
			order.SubmittedAt.UTC().Format("15:04:05"),
			order.ID, order.Status, order.Quantity, order.ClientID)
	}

	return nil
}

// recordName is the database a connection string points at, without the
// credentials in it: a report that names its source can be checked, and one that
// prints a password cannot be pasted anywhere.
func recordName(databaseURL string) string {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "unreadable DATABASE_URL"
	}

	return parsed.Host + strings.TrimSuffix(parsed.Path, "/")
}

// whatTheRecordHolds is every broker order id our record knows, from both sides
// it can know one: an id the ladder saw, and an id a replacement was given.
// Reading only the first would count every replaced order as a hole.
func whatTheRecordHolds(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx,
		`SELECT order_ref FROM execution_steps
		  UNION
		 SELECT replaced_by FROM execution_steps WHERE replaced_by IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("read the execution steps: %w", err)
	}
	defer rows.Close()

	known := map[string]bool{}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("read an order reference: %w", err)
		}
		known[ref] = true
	}

	return known, rows.Err()
}
