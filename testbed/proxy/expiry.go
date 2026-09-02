// Expiry: the reason positions must go out, and the reason a book has to be able
// to hold SHARES.
//
// An option that ends in the money by as little as a cent is exercised by
// itself - that is the OCC's exercise by exception, and it happens without any
// instruction from the holder. An option out of the money expires empty.
//
// The strategy risk this was written for: the price closes BETWEEN the legs of a
// sold spread. The short leg is assigned, the long leg expires empty, and
// instead of a bounded loss the account holds a position in shares - on SPY that
// is seventy-odd thousand dollars per contract. An instrument that does not
// reproduce this shows the strategy as safer than it is, in exactly the place
// where it is dangerous.
//
// Options on shares and ETFs are PHYSICALLY SETTLED: exercise turns the contract
// into a hundred shares. Index options (SPX, SPXW, NDX, RUT) settle in cash: the
// intrinsic value is credited or debited.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	_ "time/tzdata" // the exchange zone has to be in the binary: a machine may carry no zone database
)

// exchangeZone is the exchange's time. Expiry, the end of the session and the
// closing date are counted in it, not in the machine's local time.
var exchangeZone = mustZone()

func mustZone() *time.Location {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		panic("the exchange time zone was not loaded: " + err.Error())
	}

	return zone
}

// settlementHour is when expiry counts as having happened. Options trading ends
// at 16:00 New York time; settlement goes by the underlying's closing price.
const settlementHour = 16

// cashSettled holds the roots that settle in cash and the name of the underlying
// for each. Everything else is physically settled.
var cashSettled = map[string]string{
	"SPX":  "SPX",
	"SPXW": "SPX",
	"XSP":  "XSP",
	"NDX":  "NDX",
	"RUT":  "RUT",
	"VIX":  "VIX",
	"VIXW": "VIX",
}

// settledAt is the moment a contract counts as settled.
func settledAt(c contract) time.Time {
	y, m, d := c.Expires.Date()

	return time.Date(y, m, d, settlementHour, 0, 0, 0, exchangeZone)
}

// settle settles everything that has already expired. Called on every tick of
// the matcher: expiry is not an event anybody notifies us about.
func (a *arena) settle(ctx context.Context, book *Book, now time.Time) error {
	due := book.Due(now)
	if len(due) == 0 {
		return nil
	}

	// The underlying's price is the last trade. Alpaca publishes no separate
	// settlement price; at the edge of a strike the difference is cents, and it
	// is said out loud rather than hidden.
	need := map[string]bool{}
	for _, c := range due {
		if _, cash := cashSettled[c.Root]; cash {
			// For indices the free data feed carries no price at all: checked
			// with a live request on 28 August, get_stock_latest_trade on SPX
			// answers with an empty list of trades. Guessing is not allowed, and
			// quietly writing the position off even less so - we leave it and
			// say so.
			continue
		}
		need[c.Root] = true
	}

	prices := map[string]float64{}
	if len(need) > 0 {
		var err error
		prices, err = a.lastTrades(ctx, prioTrade, keys(need))
		if err != nil {
			return fmt.Errorf("the underlying's price was not read: %w", err)
		}
	}

	for _, c := range due {
		symbol := c.Symbol
		under, cash := cashSettled[c.Root]
		if cash {
			a.complain(symbol, "%s has expired: it settles in cash against %s, and the free feed carries no index price - the position is left as it is", symbol, under)

			continue
		}
		price, ok := prices[strings.ToUpper(c.Root)]
		if !ok || price <= 0 {
			a.complain(symbol, "%s has expired: there is no price for %s, settlement is held over", symbol, c.Root)

			continue
		}
		e, err := book.Settle(symbol, price, now)
		if err != nil {
			log.Printf("book %s: %s was not settled: %v", short(book.Hash), symbol, err)

			continue
		}
		log.Printf("book %s: %s with %s=%.2f - %s", short(book.Hash), symbol, c.Root, price, e.Why)
	}
	return nil
}

// complain reports a trouble once per symbol. The matcher ticks every two
// seconds, and an expiry that can never be settled would flood the log until
// nothing else in it could be seen.
func (a *arena) complain(key, format string, args ...any) {
	a.mu.Lock()
	first := !a.said[key]
	a.said[key] = true
	a.mu.Unlock()

	if first {
		log.Printf(format, args...)
	}
}

// dueContract is an expired position together with its parsed symbol.
type dueContract struct {
	contract
	Symbol string
}

// Due says which option positions have already expired.
func (b *Book) Due(now time.Time) []dueContract {
	b.mu.Lock()
	defer b.mu.Unlock()

	var out []dueContract
	for symbol, p := range b.Positions {
		if p.Class == classEquity || p.Qty == 0 {
			continue
		}
		c, err := parseOCC(symbol)
		if err != nil {
			continue
		}
		if now.Before(settledAt(c)) {
			continue
		}
		out = append(out, dueContract{contract: c, Symbol: symbol})
	}

	return out
}

// Settle books the expiry of one position against the underlying price `under`.
//
// The threshold is a cent: the OCC exercises everything in the money by 0.01 or
// more, and the holder is not asked. Below the threshold the contract simply
// disappears.
func (b *Book) Settle(symbol string, under float64, now time.Time) (Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	p := b.Positions[symbol]
	if p == nil {
		return Event{}, fmt.Errorf("there is no position in %s", symbol)
	}
	c, err := parseOCC(symbol)
	if err != nil {
		return Event{}, err
	}

	qty := p.Qty
	// Rounded to the cent BEFORE the comparison: in binary 640.01 is slightly
	// less than itself, and a contract in the money by exactly a cent would
	// otherwise expire empty. The OCC's threshold is stated in cents, so the
	// arithmetic has to be in cents too.
	value := math.Round(c.intrinsic(under)*100) / 100
	e := Event{Kind: "expiry", Symbol: symbol, At: now, Sets: qty, Price: under}

	switch {
	case value < 0.01:
		e.Why = fmt.Sprintf("expired empty: %s with the underlying at %.2f", symbol, under)
		delete(b.Positions, symbol)

	case cashSettled[c.Root] != "":
		// Cash settlement: the intrinsic value times the multiplier.
		b.Cash += value * float64(qty) * multiplier
		e.Kind = "settlement"
		e.Why = fmt.Sprintf("settled in cash: %.2f x %d x %d", value, qty, multiplier)
		delete(b.Positions, symbol)

	default:
		// Physical settlement. The signs: a call brings shares and takes the
		// strike, a put the other way round; for a short position it is all the
		// same with the sign flipped, because qty is negative. That is what
		// assignment is - nobody chooses it.
		shares := qty * multiplier
		money := -c.Strike * float64(qty) * multiplier
		if !c.Call {
			shares, money = -shares, -money
		}
		delete(b.Positions, symbol)
		b.move(c.Root, classEquity, shares, c.Strike)
		if held := b.Positions[c.Root]; held != nil {
			held.Mark = under
		}
		b.Cash += money

		e.Kind = "exercise"
		if qty < 0 {
			e.Kind = "assignment"
		}
		e.Why = fmt.Sprintf("%s in the money by %.2f: %+d shares of %s and %+.2f in cash",
			symbol, value, shares, c.Root, money)
	}

	b.Events = append(b.Events, e)

	return e, b.persist()
}
