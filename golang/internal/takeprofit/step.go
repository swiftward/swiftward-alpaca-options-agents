package takeprofit

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/execution"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
)

func (w *Watch) step(ctx context.Context) {
	held, err := w.Broker.Positions(ctx)
	if err != nil {
		w.Log.Error("could not read what is held", zap.Error(err))
		return
	}
	structures, ambiguous := Group(held)
	if w.declined == nil {
		// A pass may be driven without Run, and a nil map reads but does not write.
		w.declined = map[string]bool{}
	}
	still := make(map[string]bool, len(ambiguous))
	for _, name := range ambiguous {
		still[name] = true
		if w.declined[name] {
			continue
		}
		w.declined[name] = true
		w.Log.Info("leaving a holding this watch cannot read as one structure",
			zap.String("holding", name),
			zap.String("why", "one underlying, expiry and type hold more than one sold-and-bought pair, "+
				"so what the credit belonged to cannot be told from what the broker reports"))
	}
	for name := range w.declined {
		if !still[name] {
			delete(w.declined, name)
		}
	}
	if len(structures) == 0 {
		return
	}

	// An order already walking toward the book is the same structure being
	// closed. Sending a second would double the close and leave the account short
	// what it never held.
	orders, err := w.Broker.Orders(ctx, 100)
	if err != nil {
		w.Log.Error("could not read the orders in flight", zap.Error(err))
		return
	}
	// The LEGS, not the order's own symbol: a multi-leg order carries no symbol of
	// its own, so a map built from that field held one empty string and matched
	// nothing. Every close this watch sends is multi-leg, which made the guard
	// against sending a second one inert exactly where it was needed.
	walking := map[string]bool{}
	for _, order := range orders {
		// What the BROKER still has in play, by its own list of statuses. Naming
		// the finished ones instead - filled and canceled - counted `replaced`,
		// `expired` and `rejected` as orders still walking, and the order list
		// goes back far enough that a structure could be blocked from closing for
		// the rest of the week by an order that ended days ago.
		if !order.Active() {
			continue
		}
		if order.Symbol != "" {
			walking[strings.ToUpper(order.Symbol)] = true
		}
		for _, leg := range order.Legs {
			if leg.Symbol != "" {
				walking[strings.ToUpper(leg.Symbol)] = true
			}
		}
	}

	// Asked once for the pass, like every other number this project reads from a
	// declaration: it is the same answer for every structure in it, and an
	// unreadable one closes nothing rather than closing everything at zero.
	share, err := w.share()
	if err != nil {
		w.Log.Error("could not read the take-profit share; nothing is closed this pass", zap.Error(err))
		return
	}

	for _, structure := range structures {
		w.consider(ctx, structure, walking, share)
	}
}

func (w *Watch) consider(ctx context.Context, s Structure, walking map[string]bool, share float64) {
	if s.Credit <= 0 {
		// A structure entered for a debit. What "giving back the credit" means
		// there is a different question with a different answer, and guessing it
		// here would close the convexity layer at the wrong moment.
		return
	}
	if w.expired(s) {
		// An expired structure cannot be closed: it is waiting to settle, and an
		// order on it will never fill. Without this check the watch battered the
		// same structure every ten minutes - on 29 August it sent FIVE orders on a
		// QQQ spread that had expired the day before, and the ladder cancelled each
		// one on patience.
		//
		// The credit condition did not stop it but urged it on: on an expired
		// structure the buy-back tends to zero, so it looks perfectly ripe to
		// close.
		return
	}
	name := s.key()
	if at, seen := w.sent[name]; seen && w.Now().Sub(at) < standing {
		return
	}
	for _, leg := range s.Legs {
		if walking[strings.ToUpper(leg.Symbol)] {
			return
		}
	}

	symbols := make([]string, 0, len(s.Legs))
	for _, leg := range s.Legs {
		symbols = append(symbols, leg.Symbol)
	}
	quotes, err := w.Broker.Quotes(ctx, symbols)
	if err != nil {
		w.Log.Error("could not price the structure", zap.String("structure", name), zap.Error(err))
		return
	}

	cost, ok := BuyBack(s, quotes)
	if !ok {
		// A leg the book is not standing on both sides of has no closing price.
		// Sending an order against half a quote is how a close becomes a gift.
		return
	}
	if cost < 0 {
		// Closing something opened for a CREDIT cannot pay us: such a structure is
		// worth between nothing and its own width, and no one buys it back for
		// less than nothing. A negative price means the book is inverted - the
		// further leg quoted above the nearer one, which does not happen - and one
		// of the two quotes is stale.
		//
		// Measured on QQQ 725/726 on 28 August 2026: the 726 call bid stood at
		// 0.03 against the 725 call ask at 0.02. The watch priced the buy-back at
		// minus a cent, sent an order at a price that cannot exist, and had it
		// cancelled on patience. Eleven times in two hours, and every cancellation
		// is a line in the record a judge reads.
		//
		// The screener refuses on the same grounds and in nearly the same words: a
		// structure paying more than its width is not a find, it is a broken quote.
		w.Log.Warn("the book is inverted, so this cannot be priced to close",
			zap.String("structure", name), zap.Float64("apparent_buy_back", cost))

		return
	}
	if cost > s.Credit*share {
		return
	}

	legs := make([]marketdata.Leg, 0, len(s.Legs))
	for _, leg := range s.Legs {
		ratio := int(math.Round(math.Abs(leg.Quantity))) / s.Sets
		if ratio < 1 {
			return
		}
		legs = append(legs, marketdata.Leg{Symbol: leg.Symbol, Ratio: ratio, Buy: leg.Quantity < 0})
	}

	// The limit is what the book is showing, not better: this is a structure we
	// have already won, and haggling over a cent risks the whole tail it is being
	// closed to escape. The ladder walks it from here if the book moves away.
	kept := s.Credit - cost
	w.Log.Info("closing a structure that has given back its credit",
		zap.String("structure", name),
		zap.Float64("credit_per_set", s.Credit),
		zap.Float64("buy_back", cost),
		zap.Float64("share_given_back", 1-cost/s.Credit),
		zap.Int("sets", s.Sets),
		zap.Float64("keeping_dollars", kept*100*float64(s.Sets)))

	// POSITIVE, because buying a structure back costs money and the broker prices
	// a debit positive - the same convention the opening orders use with the sign
	// the other way round. It was sent negative until 29 August 2026, and the
	// broker never filled one: every close the watch sent, twelve of them across
	// two days, was cancelled at a price that asked to be PAID for closing. The
	// watch looked like it was working, in the log and in the record both.
	// The name states the worst price this close accepts, or the ladder cannot
	// walk it and the comment above is a lie: `Reservation` finds no floor and
	// leaves the order exactly where it was sent, until patience cancels it. Every
	// close this watch has ever sent was in that state.
	//
	// The bound is the price that made it close. Above it the buy-back costs more
	// than the share of the credit this structure was being closed to keep, and it
	// is no longer taking a profit.
	sent, err := w.Broker.CloseStructure(ctx, legs, s.Sets, cost,
		fmt.Sprintf("%s;tp-%s-%d", execution.NameFor(s.Credit*share), name, w.Now().Unix()))
	if err != nil {
		w.Log.Error("the close was refused", zap.String("structure", name), zap.Error(err))
		return
	}
	w.wroteDown(ctx, sent, cost)
	w.sent[name] = w.Now()
}

// expired says whether the day of this structure's expiration is already behind
// us on the exchange's calendar. The day itself is not expired: a position can be
// closed right up to the bell.
func (w *Watch) expired(s Structure) bool {
	where := w.Where
	if where == nil {
		where = time.UTC
	}
	day, err := time.Parse(time.DateOnly, s.Expiration)
	if err != nil {
		// If the date did not parse, leave it alone: closing what you do not
		// understand is worse than not closing.
		return true
	}
	here := w.Now().In(where)
	today := time.Date(here.Year(), here.Month(), here.Day(), 0, 0, 0, 0, time.UTC)

	return day.Before(today)
}

func (s Structure) key() string {
	return fmt.Sprintf("%s-%s-%s", s.Underlying, s.Expiration, s.Kind)
}

// Group pairs held legs into the structures they were opened as: one underlying,
// one expiration, one type. It is a guess made from what the broker holds, and it
// is the honest one available - the broker forgets that four legs arrived as one
// order the moment they fill.
//
// A group is only returned where the guess is UNAMBIGUOUS: exactly one leg sold
// and one bought. Anything else on one underlying, expiry and type is two
// structures the broker has merged - a premium spread beside a convexity
// backspread, say - and closing "it" would price a thing that was never opened
// and take the hedge away with the winner. Those are returned separately so the
// caller can say what it is declining rather than passing over them in silence.
func Group(held []marketdata.Position) (structures []Structure, ambiguous []string) {
	by := map[string][]marketdata.Position{}
	for _, p := range held {
		if !strings.EqualFold(p.AssetClass, "us_option") && !strings.EqualFold(p.AssetClass, "option") {
			continue
		}
		under, expiry, kind, ok := readSymbol(p.Symbol)
		if !ok {
			continue
		}
		by[under+"|"+expiry+"|"+kind] = append(by[under+"|"+expiry+"|"+kind], p)
	}

	out := make([]Structure, 0, len(by))
	for key, legs := range by {
		parts := strings.Split(key, "|")
		sort.Slice(legs, func(i, j int) bool { return legs[i].Symbol < legs[j].Symbol })
		sets := setsIn(legs)
		if sets < 1 {
			continue
		}
		credit := 0.0
		for _, leg := range legs {
			ratio := math.Abs(leg.Quantity) / float64(sets)
			if leg.Quantity < 0 {
				credit += leg.AverageEntryPrice * ratio // sold: money came in
			} else {
				credit -= leg.AverageEntryPrice * ratio // bought: money went out
			}
		}
		structure := Structure{
			Underlying: parts[0], Expiration: parts[1], Kind: parts[2],
			Legs: legs, Credit: credit, Sets: sets,
		}
		if !oneForOne(legs) {
			ambiguous = append(ambiguous, structure.key())

			continue
		}
		out = append(out, structure)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	sort.Strings(ambiguous)

	return out, ambiguous
}

// oneForOne says whether these legs can only have come from one credit vertical:
// one leg sold, one leg bought, and the SAME number of each. Two sold legs are
// two structures merged; three legs are the same; one leg alone is half of
// something whose other half has gone.
//
// The quantities matter as much as the count, and leaving them out is what this
// watch got wrong until 31 August. A backspread sells one and buys two: it has a
// sold leg and a bought leg, so counting legs calls it a vertical - and then the
// watch buys it back the moment its small credit is recovered. That is the whole
// structure destroyed for a few dollars, because a backspread is not paid by
// decay at all; it is paid by a move that has not happened yet. Whoever opened it
// said how long to hold it, and it is theirs to close.
func oneForOne(legs []marketdata.Position) bool {
	if len(legs) != 2 {
		return false
	}
	sold, bought := 0.0, 0.0
	for _, leg := range legs {
		if leg.Quantity < 0 {
			sold += math.Abs(leg.Quantity)
		} else {
			bought += leg.Quantity
		}
	}

	return sold > 0 && sold == bought
}

// setsIn is the largest whole number of sets the held quantities all divide by.
// Six short and twelve long is six sets of one and two, not twelve of a half.
func setsIn(legs []marketdata.Position) int {
	sets := 0
	for _, leg := range legs {
		q := int(math.Round(math.Abs(leg.Quantity)))
		if q == 0 {
			return 0
		}
		sets = gcd(sets, q)
	}

	return sets
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}

	return a
}

// BuyBack is what one set costs to close at the sides of the book an order would
// actually cross: what was sold is bought back on the ASK, what was bought is
// sold on the BID. Never the midpoint - a midpoint close is a price nobody pays.
func BuyBack(s Structure, quotes map[string]marketdata.Quote) (float64, bool) {
	cost := 0.0
	for _, leg := range s.Legs {
		quote, known := quotes[leg.Symbol]
		if !known || quote.Bid <= 0 || quote.Ask <= 0 || quote.Ask < quote.Bid {
			return 0, false
		}
		ratio := math.Abs(leg.Quantity) / float64(s.Sets)
		if leg.Quantity < 0 {
			cost += quote.Ask * ratio // we buy back what was sold, at the ask
		} else {
			cost -= quote.Bid * ratio // we sell what was bought, at the bid
		}
	}

	return cost, true
}

// readSymbol takes the underlying, the expiry and the type out of an OCC symbol:
// SPY260902C00777000 is SPY, 2026-09-02, call.
func readSymbol(symbol string) (string, string, string, bool) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if len(symbol) < 15 {
		return "", "", "", false
	}
	tail := symbol[len(symbol)-15:]
	under := symbol[:len(symbol)-15]
	if under == "" {
		return "", "", "", false
	}
	day, err := time.Parse("060102", tail[:6])
	if err != nil {
		return "", "", "", false
	}
	kind := "call"
	switch tail[6] {
	case 'C':
	case 'P':
		kind = "put"
	default:
		return "", "", "", false
	}

	return under, day.Format(time.DateOnly), kind, true
}

// wroteDown keeps a sent order at the moment it is sent. A record that cannot be
// written is said out loud and does not stop the close: the position matters
// more than the note about it.
func (w *Watch) wroteDown(ctx context.Context, orderRef string, limit float64) {
	if w.Record == nil || orderRef == "" {
		return
	}
	step := record.ExecutionStep{
		OrderRef: orderRef, At: w.Now(), Action: "placed", Was: limit,
	}
	if err := w.Record.AppendExecutionStep(ctx, step); err != nil {
		w.Log.Error("could not write down the order that was sent",
			zap.String("order", orderRef), zap.Error(err))
	}
}
