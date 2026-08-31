// Rehearse puts the system into the state a trading day puts it in, and does it
// while nothing is at stake.
//
// It exists because of 31 August. Everything was checked over the weekend and
// everything passed: the gate was green, the stack came up, every agent answered.
// Then the market opened and the screener could not read a single price, because
// a rate limit inside our own platform had been left on its default and nothing
// had ever asked it for a burst. Two entry windows were gone before the cause was
// found.
//
// The lesson is not "check more". It is that a green gate proves the code agrees
// with itself, and proves nothing about the load the code will meet. The market
// being closed does not stop the reads: this can run on a Sunday and give the
// same answer Monday would have given at a cost of nothing.
//
// It is a REPORT and it writes nothing. It sends the reads a trading day sends,
// from as many callers as trade, and prints what came back.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// pricesPerCall is the screener's own batch size. Rehearsing with a different one
// would rehearse a different burst, which is the only thing being measured here.
const pricesPerCall = 20

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "rehearse:", err)
		os.Exit(1)
	}
}

// caller is one agent as the gateway sees it. The endpoint is part of it: each
// agent has its own, because one entry point serves exactly one account.
type caller struct {
	name  string
	url   string
	token string
}

func run(ctx context.Context) error {
	// The agent presents two things on every call: the credential for its own
	// entry point, and WHO it is acting as. Sending only the first is a different
	// caller than the one that trades, and the gateway says so - "Identity
	// unresolved" - which is what this rehearsal answered the first time it ran.
	header := os.Getenv("USER_HEADER_NAME")
	if header == "" {
		header = "X-Swiftward-User"
	}
	user := os.Getenv("USER_TOKEN")
	if user == "" {
		return fmt.Errorf("USER_TOKEN: without it this rehearses a caller nobody recognises")
	}

	universe := strings.Split(os.Getenv("REHEARSE_UNDERLYINGS"), ",")
	if len(universe) < 2 {
		return fmt.Errorf("REHEARSE_UNDERLYINGS: give the screener's own list, or the burst is not the burst")
	}

	var callers []caller
	for _, name := range strings.Split(os.Getenv("REHEARSE_CALLERS"), ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		key := strings.ToUpper(strings.ReplaceAll(name, "-", "_"))
		token := os.Getenv("REHEARSE_TOKEN_" + key)
		url := os.Getenv("REHEARSE_URL_" + key)
		if token == "" || url == "" {
			return fmt.Errorf("caller %q has no url or no token: a rehearsal missing a caller is not the load", name)
		}
		callers = append(callers, caller{name: name, url: url, token: token})
	}
	if len(callers) == 0 {
		return fmt.Errorf("REHEARSE_CALLERS: name the agents that trade")
	}

	fmt.Printf("rehearsing %d callers over %d underlyings, %d per call\n",
		len(callers), len(universe), pricesPerCall)

	started := time.Now()
	var wg sync.WaitGroup
	results := make([]map[string]int, len(callers))
	prices := make([]int, len(callers))
	for i, who := range callers {
		wg.Add(1)
		go func(i int, who caller) {
			defer wg.Done()
			results[i], prices[i] = sweep(ctx, who, header, user, universe)
		}(i, who)
	}
	wg.Wait()

	failed := false
	for i, who := range callers {
		fmt.Printf("\n%s: %d prices read\n", who.name, prices[i])
		if len(results[i]) == 0 {
			continue
		}
		failed = true
		reasons := make([]string, 0, len(results[i]))
		for reason := range results[i] {
			reasons = append(reasons, reason)
		}
		sort.Strings(reasons)
		for _, reason := range reasons {
			fmt.Printf("  REFUSED %3d  %s\n", results[i][reason], reason)
		}
	}

	fmt.Printf("\ntook %s\n", time.Since(started).Round(time.Millisecond))
	if failed {
		return fmt.Errorf("the reads a trading day sends do not all get through")
	}
	fmt.Println("every read got through: this load is survivable")

	return nil
}

// sweep sends one caller's whole pass and answers what came back, counted by
// reason rather than listed: a wall of identical refusals says one thing, and
// saying it once is what makes it readable.
func sweep(ctx context.Context, who caller, header, user string, universe []string) (map[string]int, int) {
	broker := marketdata.NewBrokerWithToken(who.url, who.token).ActingFor(header, user)
	refused := map[string]int{}
	read := 0
	for start := 0; start < len(universe); start += pricesPerCall {
		end := start + pricesPerCall
		if end > len(universe) {
			end = len(universe)
		}
		got, err := broker.LastTrades(ctx, universe[start:end])
		if err != nil {
			refused[err.Error()] += end - start

			continue
		}
		read += len(got)
	}

	return refused, read
}
