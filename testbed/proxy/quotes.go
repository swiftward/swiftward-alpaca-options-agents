// Reading the market upstream: quotes, the clock, the price of the underlying.
//
// Two things live here that were missing, and without which the instrument lied.
// The first is checking the SHAPE: json.Unmarshal parses somebody else's answer
// into an empty struct without a single error, and a field renamed upstream then
// looks exactly like "the market is quiet". The second is FRESHNESS: a quote
// carries a timestamp, and an order filled against a two-hour-old quote is a
// trade without risk rather than a measurement.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Quote is a two-sided quote of a contract or a share.
type Quote struct {
	Symbol  string
	Bid     float64
	Ask     float64
	BidSize int
	AskSize int
	At      time.Time
}

// side gives the price and the shown size of the side we fill against. A buy
// pays the ask, a sell takes the bid - and there is no reason to demand both
// sides: buying back a worthless short option needs the ask alone, and a zero
// bid on a far out-of-the-money contract is the market being normal, not
// broken.
func (q Quote) side(buy bool) (price float64, size int) {
	if buy {
		return q.Ask, q.AskSize
	}

	return q.Bid, q.BidSize
}

// mark is the price the broker values a position at: the middle of the market.
// A one-sided quote is valued at half of the side that exists, which is the same
// as the middle with a zero on the other side, and is how Alpaca counts it.
func (q Quote) mark() float64 {
	switch {
	case q.Bid > 0 && q.Ask > 0:
		return (q.Bid + q.Ask) / 2
	case q.Ask > 0:
		return q.Ask / 2
	case q.Bid > 0:
		return q.Bid / 2
	}

	return 0
}

// liquidation is what a position would actually close at: a long one at the
// bid, a short one at the ask. The gap from mark is the cost of getting out, and
// hiding it is not allowed.
func (q Quote) liquidation(long bool) float64 {
	if long {
		return q.Bid
	}

	return q.Ask
}

type snapRow struct {
	LatestQuote struct {
		AskPrice float64   `json:"ap"`
		AskSize  int       `json:"as"`
		BidPrice float64   `json:"bp"`
		BidSize  int       `json:"bs"`
		At       time.Time `json:"t"`
	} `json:"latestQuote"`
	// The volatility is read for ONE purpose: an overlay reprices each contract
	// from the market's own view of it rather than from a number we chose. Where
	// the broker sends none - and on index contracts it sends none at all - the
	// overlay says so and refuses rather than substituting a plausible 0.20.
	ImpliedVolatility float64 `json:"impliedVolatility"`
	Greeks            struct {
		Delta float64 `json:"delta"`
	} `json:"greeks"`
}

// snapshotAnswer is the shape of the get_option_snapshot answer, taken off the
// live server on 28 August rather than invented. The argument there is named
// `symbols`, not `symbol_or_symbols`: with the second the server answers 400.
//
// Snapshots is a POINTER on purpose: an empty snapshot and a missing field are
// different events. The first means "the market is quiet", the second means "the
// shape upstream has changed", and parsing that cannot tell them apart quietly
// turns the second into the first.
type snapshotAnswer struct {
	Data struct {
		Snapshots     *map[string]snapRow `json:"snapshots"`
		NextPageToken string              `json:"next_page_token"`
	} `json:"data"`
}

// symbolsPerCall is how many symbols go upstream at once. The server itself
// allows a hundred, but its limit counts DATA POINTS rather than symbols and
// defaults to a hundred: a snapshot of six blocks per symbol would be cut off at
// the seventeenth, silently. We ask for fewer symbols and a larger limit, and
// read the pages through regardless.
const symbolsPerCall = 20

// optionQuotes reads contract quotes. A missing symbol is not an error: the
// contract may have expired, and the place to learn that is expiry settlement,
// not the parsing of an answer.
func (a *arena) optionQuotes(ctx context.Context, prio int, symbols []string) (map[string]Quote, error) {
	// Under a staged market the book fills against the SAME prices the agent
	// read. Anything else would be a bench in which the agent sees one market
	// and trades another.
	if a.staged.staging() {
		now := a.staged.now()
		stagedOut := make(map[string]Quote, len(symbols))
		for _, symbol := range symbols {
			if q, ok := now.Quotes[symbol]; ok {
				stagedOut[symbol] = q
			}
		}

		return stagedOut, nil
	}

	// Under an overlay every contract in this answer is repriced against ONE
	// spot, read once here. Reading it per contract would price the two legs of a
	// spread a few cents apart and call the difference the scenario's.
	shift, spot := 0.0, 0.0
	if a.staged.overlaying() {
		if shift = a.staged.shiftNow(); shift != 0 {
			var err error
			if spot, err = a.realSpot(ctx, prio); err != nil {
				return nil, fmt.Errorf("the overlay could not read the real price of %s, and without it nothing can be repriced: %w",
					a.staged.underlying(), err)
			}
		}
	}

	out := make(map[string]Quote, len(symbols))
	for from := 0; from < len(symbols); from += symbolsPerCall {
		to := min(from+symbolsPerCall, len(symbols))
		page := ""
		for {
			args := map[string]any{
				"symbols": strings.Join(symbols[from:to], ","),
				"limit":   1000,
			}
			if page != "" {
				args["page_token"] = page
			}

			var answer snapshotAnswer
			if err := a.up.CallJSON(ctx, prio, "get_option_snapshot", args, &answer); err != nil {
				return nil, err
			}
			if answer.Data.Snapshots == nil {
				return nil, fmt.Errorf("the get_option_snapshot answer has no snapshots field: the shape upstream has changed")
			}
			for symbol, row := range *answer.Data.Snapshots {
				q := Quote{
					Symbol:  symbol,
					Bid:     row.LatestQuote.BidPrice,
					Ask:     row.LatestQuote.AskPrice,
					BidSize: row.LatestQuote.BidSize,
					AskSize: row.LatestQuote.AskSize,
					At:      row.LatestQuote.At,
				}
				if shift != 0 {
					c, err := parseOCC(symbol)
					if err != nil {
						return nil, fmt.Errorf("the overlay cannot reprice %s: %w", symbol, err)
					}
					if row.ImpliedVolatility <= 0 {
						return nil, fmt.Errorf("the overlay cannot reprice %s: the broker sends no implied volatility for it, "+
							"and a volatility of our own would make the answer ours rather than the market's", symbol)
					}
					q = shiftQuote(q, c, spot, shift, row.ImpliedVolatility, time.Now())
				}
				out[symbol] = q
			}
			page = answer.Data.NextPageToken
			if page == "" {
				break
			}
		}
	}

	return out, nil
}

type stockQuoteAnswer struct {
	Data struct {
		Quotes *map[string]struct {
			AskPrice float64   `json:"ap"`
			AskSize  int       `json:"as"`
			BidPrice float64   `json:"bp"`
			BidSize  int       `json:"bs"`
			At       time.Time `json:"t"`
		} `json:"quotes"`
	} `json:"data"`
}

// stockQuotes reads share quotes. Shares appear in the book not from an order
// but from an option being exercised at expiry - and they still have to be
// valued.
func (a *arena) stockQuotes(ctx context.Context, prio int, symbols []string) (map[string]Quote, error) {
	if len(symbols) == 0 {
		return map[string]Quote{}, nil
	}

	var answer stockQuoteAnswer
	if err := a.up.CallJSON(ctx, prio, "get_stock_latest_quote",
		map[string]any{"symbols": strings.Join(symbols, ",")}, &answer); err != nil {
		return nil, err
	}
	if answer.Data.Quotes == nil {
		return nil, fmt.Errorf("the get_stock_latest_quote answer has no quotes field: the shape upstream has changed")
	}

	out := make(map[string]Quote, len(symbols))
	for symbol, row := range *answer.Data.Quotes {
		out[strings.ToUpper(symbol)] = Quote{
			Symbol: symbol, Bid: row.BidPrice, Ask: row.AskPrice,
			BidSize: row.BidSize, AskSize: row.AskSize, At: row.At,
		}
	}

	return out, nil
}

type tradeAnswer struct {
	Data struct {
		Trades *map[string]struct {
			Price float64   `json:"p"`
			At    time.Time `json:"t"`
		} `json:"trades"`
	} `json:"data"`
}

// lastTrades is the last trade in an underlying. It is the settlement price at
// expiry: Alpaca publishes no separate call for the official close, and the last
// trade of the day is the nearest thing there is. The difference is cents, and
// it is said out loud, because at the edge of a strike cents decide whether an
// option is exercised at all.
func (a *arena) lastTrades(ctx context.Context, prio int, symbols []string) (map[string]float64, error) {
	if a.staged.staging() {
		now := a.staged.now()
		stagedOut := map[string]float64{}
		for _, symbol := range symbols {
			if strings.EqualFold(symbol, a.staged.underlying()) {
				stagedOut[strings.ToUpper(symbol)] = now.Price
			}
		}

		return stagedOut, nil
	}
	if len(symbols) == 0 {
		return map[string]float64{}, nil
	}

	var answer tradeAnswer
	if err := a.up.CallJSON(ctx, prio, "get_stock_latest_trade",
		map[string]any{"symbols": strings.Join(symbols, ",")}, &answer); err != nil {
		return nil, err
	}
	if answer.Data.Trades == nil {
		return nil, fmt.Errorf("the get_stock_latest_trade answer has no trades field: the shape upstream has changed")
	}

	shift := 0.0
	if a.staged.overlaying() {
		shift = a.staged.shiftNow()
	}
	underlying := strings.ToUpper(a.staged.underlying())

	out := make(map[string]float64, len(symbols))
	for symbol, row := range *answer.Data.Trades {
		if row.Price <= 0 {
			continue
		}
		name := strings.ToUpper(symbol)
		// Only the overlay's own underlying moves. A run that displaces SPY has
		// said nothing about QQQ, and moving it too would be the instrument
		// inventing a correlation it was never given.
		if shift != 0 && name == underlying {
			out[name] = round2(row.Price + shift)

			continue
		}
		out[name] = row.Price
	}

	return out, nil
}

// Clock is the broker's clock. We take its time rather than our own: expiry,
// the end of the session and the freshness of a quote are all counted in its
// time, and a machine whose clock drifts from the exchange would be a book that
// drifts from the market.
type Clock struct {
	IsOpen    bool
	Now       time.Time
	NextOpen  time.Time
	NextClose time.Time
}

type clockAnswer struct {
	Data struct {
		IsOpen    *bool  `json:"is_open"`
		Timestamp string `json:"timestamp"`
		NextOpen  string `json:"next_open"`
		NextClose string `json:"next_close"`
	} `json:"data"`
}

// clockHeldFor is how long one reading of the broker's clock is reused by
// default. The reading is reused; the TIME in it is not - it is advanced by
// however long ago the reading was taken.
//
// A bench sets it to zero: a test that moves the fake server's clock between
// ticks would otherwise be answered from the reading taken before it moved. The
// cache above carries the same switch for the same reason.
const clockHeldFor = 15 * time.Second

// clock is the broker's clock, with "now" advanced to now.
//
// This does not cache to save calls - the shared cache upstream already would.
// It exists because that cache CANNOT serve this answer: it hands back the
// timestamp exactly as it was fetched, so "now" arrives stale by the age of the
// entry, well-formed and wrong. Measured on 29 August: an order was stamped 9.4
// seconds before the intent that preceded it, and the judge duly reported an
// agent that had done everything in the right order as one that wrote its
// intent afterwards. The session boundaries do not have this problem - they are
// still true whenever they were read - so only Now is advanced.
func (a *arena) clock(ctx context.Context, prio int) (Clock, error) {
	// A staged market carries its own clock, and it is not held for a while and
	// advanced: the scenario itself decides how fast its time runs.
	if a.staged.staging() {
		now := a.staged.now()

		return Clock{IsOpen: now.Open, Now: now.Now, NextClose: a.staged.closeAt()}, nil
	}

	a.clockMu.Lock()
	if held := a.heldClock; !held.Now.IsZero() && a.clockFor > 0 {
		if age := time.Since(a.heldAt); age >= 0 && age < a.clockFor {
			a.clockMu.Unlock()
			held.Now = held.Now.Add(age)

			return held, nil
		}
	}
	a.clockMu.Unlock()

	var answer clockAnswer
	if err := a.up.CallJSONDirect(ctx, prio, "get_clock", map[string]any{}, &answer); err != nil {
		return Clock{}, err
	}
	if answer.Data.IsOpen == nil {
		return Clock{}, fmt.Errorf("the get_clock answer has no is_open field: the shape upstream has changed")
	}

	c := Clock{IsOpen: *answer.Data.IsOpen}
	c.Now = parseStamp(answer.Data.Timestamp)
	c.NextOpen = parseStamp(answer.Data.NextOpen)
	c.NextClose = parseStamp(answer.Data.NextClose)
	if c.Now.IsZero() {
		return Clock{}, fmt.Errorf("the broker's clock came with no time: %q", answer.Data.Timestamp)
	}

	a.clockMu.Lock()
	a.heldClock, a.heldAt = c, time.Now()
	a.clockMu.Unlock()

	return c, nil
}

func parseStamp(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}

	return t
}
