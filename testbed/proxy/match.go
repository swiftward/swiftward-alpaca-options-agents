// The matcher: what makes an order an order rather than an instant debit.
//
// At a real broker an order STANDS and fills when the market reaches it. The
// earlier arena filled it inside the call, and because of that a laddering
// routine - the kind that walks the price and cancels what did not go through -
// ran on nothing: there was never anything to walk. Here the order stands, and
// once a tick we read the quotes of its legs in one batched request and fill
// whatever the market has reached.
//
// Three conditions for a fill, each one bought with a mistake:
//   - the price is no worse than the limit. Alpaca's paper account fills at the
//     limit without looking at the book, and that is how profit that does not
//     exist gets drawn;
//   - the size is no larger than what is shown on the opposite side. Past the
//     first level we see nothing at all, and pretending the same price continues
//     behind it is drawing a fill that would not have happened;
//   - the market is OPEN and the quote is fresh. A fill against a frozen evening
//     quote is a trade without risk: the price is guaranteed not to move until
//     morning.
package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"
)

type matcher struct {
	a     *arena
	every time.Duration
}

func (m *matcher) run(ctx context.Context) {
	tick := time.NewTicker(m.every)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			m.once(ctx)
		}
	}
}

// once is a single pass. Errors here are only logged: the matcher is required
// to survive both a dropped connection and a silent broker, or the arena quietly
// stops filling orders while participants go on trading into nothing.
func (m *matcher) once(ctx context.Context) {
	clock, err := m.a.clock(ctx, prioTrade)
	if err != nil {
		log.Printf("matcher: the clock was not read: %v", err)

		return
	}
	now := clock.Now

	books := m.a.allBooks()

	// Expiry first, fills second: an expired leg can release collateral, and an
	// order that was standing because of it goes through in the same tick.
	for _, b := range books {
		for _, gone := range b.ExpireDay(now) {
			log.Printf("book %s: order %s withdrawn at the end of the session", short(b.Hash), gone.ID)
		}
		if err := m.a.settle(ctx, b, now); err != nil {
			log.Printf("book %s: expiry settlement: %v", short(b.Hash), err)
		}
	}

	// Equity at the day's close. The agent counts today's result from it, and it
	// has to be written exactly once a day - otherwise last_equity crawls along
	// behind current equity and the day looks flat.
	if !clock.IsOpen {
		day := now.In(exchangeZone).Format(time.DateOnly)
		for _, b := range books {
			if b.ClosedOn == day {
				continue
			}
			cash, _, _, held := b.Snapshot()
			v, err := m.a.value(ctx, prioAccount, held)
			if err != nil {
				log.Printf("book %s: closing equity was not counted: %v", short(b.Hash), err)

				continue
			}
			if b.MarkClose(day, cash+v.Market) {
				log.Printf("book %s: closing equity on %s is %.2f", short(b.Hash), day, cash+v.Market)
			}
		}
	}

	type standing struct {
		book  *Book
		order Order
	}
	var open []standing
	for _, b := range books {
		for _, o := range b.OpenOrders() {
			open = append(open, standing{book: b, order: o})
		}
	}
	if len(open) == 0 {
		return
	}

	// Orders fill in the order they were submitted, across every book at once:
	// the size shown on the book is one size for everyone, and whoever queued
	// first takes it.
	sort.SliceStable(open, func(i, j int) bool {
		return open[i].order.SubmittedAt.Before(open[j].order.SubmittedAt)
	})

	if !clock.IsOpen && !m.a.ignoreSession {
		for _, s := range open {
			s.book.Note(s.order.ID, "the market is closed: the order waits for the open")
		}

		return
	}

	symbols := map[string]bool{}
	for _, s := range open {
		for _, leg := range s.order.Legs {
			symbols[leg.Symbol] = true
		}
	}
	quotes, err := m.a.optionQuotes(ctx, prioTrade, keys(symbols))
	if err != nil {
		log.Printf("matcher: the quotes were not read: %v", err)

		return
	}

	// shown is how much is left on the book in THIS tick. Two orders on one
	// contract cannot both take the same shown lot.
	shown := map[string]int{}
	for symbol, q := range quotes {
		shown[symbol+"|buy"] = q.AskSize
		shown[symbol+"|sell"] = q.BidSize
	}

	for _, s := range open {
		m.fill(s.book, s.order, quotes, shown, now)
	}
}

func (m *matcher) fill(book *Book, o Order, quotes map[string]Quote, shown map[string]int, now time.Time) {
	price, sets, legPrice, err := executable(o.Legs, quotes, shown, now, m.a.maxQuoteAge, m.a.ignoreSession)
	if err != nil {
		book.Note(o.ID, err.Error())

		return
	}
	if !o.Market && price > o.Limit+1e-9 {
		book.Note(o.ID, fmt.Sprintf("the market offers %.2f, %.2f was asked", price, o.Limit))

		return
	}
	if sets > o.remaining() {
		sets = o.remaining()
	}
	if sets < 1 {
		book.Note(o.ID, "the opposite side of the book holds not one set")

		return
	}

	if _, err := book.Fill(o.ID, sets, price, legPrice, now); err != nil {
		log.Printf("book %s: order %s was not booked: %v", short(book.Hash), o.ID, err)

		return
	}

	// A fill that happened in bench mode is marked IN THE BOOK, not only in the
	// log. Logs rotate and are lost, the book stays, and whoever reads it later
	// must be able to see that this price was never asked of a live market: at a
	// weekend the book is frozen, and a trade against it carries no risk.
	if m.a.ignoreSession {
		book.MarkStand(o.ID)
	}
	for _, leg := range o.Legs {
		shown[leg.Symbol+"|"+leg.Side] -= leg.Ratio * sets
	}
	log.Printf("book %s: order %s filled for %d sets at %.4f", short(book.Hash), o.ID, sets, price)
}

// executable is the price of one set against the opposite sides of the book,
// and how many sets are standing there.
//
// Only THE side we fill against is required: a buy needs the ask, a sell the
// bid. Earlier code demanded a two-sided quote on every leg, and a participant
// who had sold an option could not buy it back once the bid went to zero - which
// is exactly when buying it back becomes worth doing.
func executable(legs []Leg, quotes map[string]Quote, shown map[string]int, now time.Time, maxAge time.Duration, ignoreAge bool) (price float64, sets int, legPrice map[string]float64, err error) {
	legPrice = make(map[string]float64, len(legs))
	sets = -1

	for _, leg := range legs {
		q, ok := quotes[leg.Symbol]
		if !ok {
			return 0, 0, nil, fmt.Errorf("%s has no quote: there is nothing to fill against", leg.Symbol)
		}
		if !ignoreAge {
			if q.At.IsZero() {
				return 0, 0, nil, fmt.Errorf("the quote for %s carries no timestamp: its freshness cannot be checked", leg.Symbol)
			}
			if age := now.Sub(q.At); age > maxAge {
				return 0, 0, nil, fmt.Errorf("the quote for %s is older than %s: we do not fill against a frozen price", leg.Symbol, maxAge)
			}
		}

		// A crossed quote - the bid above the ask - is not a market, it is a
		// broken reading. Filling against it hands out money that does not exist:
		// the sell side is paid MORE than the buy side costs, so the same set can
		// be opened and closed at a profit with no risk taken at all. It happens:
		// their gateway met eleven crossed quotes in two hours on 29 August. The
		// instrument refuses rather than prints, and says which contract.
		if q.Bid > 0 && q.Ask > 0 && q.Bid > q.Ask {
			return 0, 0, nil, fmt.Errorf(
				"the quote for %s is crossed: the bid %.2f stands above the ask %.2f. That is a broken reading rather than a market, and filling against it would pay out money the market never offered",
				leg.Symbol, q.Bid, q.Ask)
		}

		buy := leg.Side == "buy"
		p, size := q.side(buy)
		if p <= 0 {
			return 0, 0, nil, fmt.Errorf("%s has no price on the %s side", leg.Symbol, leg.Side)
		}
		if have, ok := shown[leg.Symbol+"|"+leg.Side]; ok {
			size = have
		}

		ratio := float64(leg.Ratio)
		if buy {
			price += p * ratio
		} else {
			price -= p * ratio
		}
		legPrice[leg.Symbol] = p

		fits := size / leg.Ratio
		if sets < 0 || fits < sets {
			sets = fits
		}
	}

	if sets < 0 {
		sets = 0
	}

	return price, sets, legPrice, nil
}

func keys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)

	return out
}
