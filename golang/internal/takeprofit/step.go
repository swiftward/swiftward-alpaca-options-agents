package takeprofit

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

func (w *Watch) step(ctx context.Context) {
	held, err := w.Broker.Positions(ctx)
	if err != nil {
		w.Log.Error("could not read what is held", zap.Error(err))
		return
	}
	structures := Group(held)
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
	walking := map[string]bool{}
	for _, order := range orders {
		if strings.EqualFold(order.Status, "filled") || strings.EqualFold(order.Status, "canceled") {
			continue
		}
		walking[strings.ToUpper(order.Symbol)] = true
	}

	for _, structure := range structures {
		w.consider(ctx, structure, walking)
	}
}

func (w *Watch) consider(ctx context.Context, s Structure, walking map[string]bool) {
	if s.Credit <= 0 {
		// A structure entered for a debit. What "giving back the credit" means
		// there is a different question with a different answer, and guessing it
		// here would close the convexity layer at the wrong moment.
		return
	}
	if w.expired(s) {
		// Истёкшее закрыть нельзя: оно ждёт расчёта, и заявка по нему не
		// исполнится никогда. Без этой проверки сторож бился в одну и ту же
		// конструкцию каждые десять минут - 29 августа он отправил ПЯТЬ заявок на
		// спред QQQ, истёкший накануне, и лестница отменяла каждую по терпению.
		//
		// Условие про кредит его не останавливало, а наоборот подгоняло: у
		// истёкшей конструкции выкуп стремится к нулю, то есть она выглядит
		// идеально созревшей для закрытия.
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
	if cost > s.Credit*w.At {
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

	if err := w.Broker.CloseStructure(ctx, legs, s.Sets, -cost,
		fmt.Sprintf("tp-%s-%d", name, w.Now().Unix())); err != nil {
		w.Log.Error("the close was refused", zap.String("structure", name), zap.Error(err))
		return
	}
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
		// Дату не разобрали - не трогаем: закрыть то, чего не понимаешь, хуже,
		// чем не закрыть.
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
func Group(held []marketdata.Position) []Structure {
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
				credit += leg.AverageEntryPrice * ratio // продали: деньги пришли
			} else {
				credit -= leg.AverageEntryPrice * ratio // купили: деньги ушли
			}
		}
		out = append(out, Structure{
			Underlying: parts[0], Expiration: parts[1], Kind: parts[2],
			Legs: legs, Credit: credit, Sets: sets,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })

	return out
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
			cost += quote.Ask * ratio // выкупаем проданное по ask
		} else {
			cost -= quote.Bid * ratio // продаём купленное по bid
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
