package placement

import (
	"errors"
	"math"
	"sort"
	"time"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// leg is one contract worth building on: it expires when we asked, it is the
// type we asked for, and the book is standing on both sides of it.
type leg struct {
	symbol string
	strike float64
	bid    float64
	ask    float64
}

// usable keeps the contracts that can actually be traded. A contract quoted on
// one side only is not a price - nobody is standing behind the missing half -
// and a placement built on it would report a credit that cannot be collected.
func usable(contracts []marketdata.Contract, quotes map[string]marketdata.Quote, ask Ask) []leg {
	out := make([]leg, 0, len(contracts))
	for _, contract := range contracts {
		if contract.Type != ask.Kind {
			continue
		}
		if !sameDay(contract.Expiration, ask.Expiration) {
			continue
		}
		quote, known := quotes[contract.Symbol]
		if !known || quote.Bid <= 0 || quote.Ask <= 0 || quote.Ask < quote.Bid {
			continue
		}
		out = append(out, leg{symbol: contract.Symbol, strike: contract.Strike, bid: quote.Bid, ask: quote.Ask})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].strike < out[j].strike })

	return out
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.UTC().Date()
	by, bm, bd := b.UTC().Date()

	return ay == by && am == bm && ad == bd
}

// band is the stretch of strikes worth reading. It is the caller's own limits
// widened a little, so the enumeration has somewhere to walk without pulling a
// chain of two hundred and forty-five strikes when twenty-two are permitted.
func (s Scorer) band(ask Ask, spot, sigma float64) (float64, float64) {
	most := ask.ShortMostSigma
	if most <= 0 {
		most = 4
	}
	reach := math.Max(most, ask.ValleyLeastSigma) + 3

	if ask.Kind == "call" {
		return spot + ask.ShortLeastSigma*sigma - sigma, spot + reach*sigma
	}

	return spot - reach*sigma, spot - ask.ShortLeastSigma*sigma + sigma
}

// windows turns the closes into the moves the replay stands on, keeping only
// those that happened in weather like today's. It returns the moves as ratios of
// end over start, and today's annualised volatility.
//
// The regime filter is the whole point. Unfiltered, the same structure scored
// +625 dollars on 28 August and +84 in the volatility actually standing; the
// difference is April 2025, and April 2025 is not this week.
func windows(closes []float64, days int) ([]float64, float64, error) {
	if len(closes) < volWindow+days+30 {
		return nil, 0, errors.New("not enough history to replay: ask for more days")
	}

	logs := make([]float64, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		logs[i-1] = math.Log(closes[i] / closes[i-1])
	}

	rolling := make([]float64, len(logs))
	for i := range logs {
		if i+1 < volWindow {
			rolling[i] = math.NaN()
			continue
		}
		rolling[i] = deviation(logs[i+1-volWindow:i+1]) * math.Sqrt(252)
	}

	now := rolling[len(rolling)-1]
	if math.IsNaN(now) || now <= 0 {
		return nil, 0, errors.New("the history gives no volatility to compare against")
	}

	moves := make([]float64, 0, len(closes))
	for i := 0; i+days < len(closes); i++ {
		// The volatility as it stood when the window OPENED - rolling[i-1], which
		// is built from returns up to the close of day i and is the last reading a
		// session standing there could have had.
		//
		// It said i+days-1 until review on 28 August 2026, and that is the twenty
		// days ending on the window's LAST day - the window judged by its own
		// outcome. The bias is not academic and it runs the wrong way for exactly
		// this measurement: a calm week that ends in a jump has a loud closing
		// volatility, so every window where the big move actually HAPPENED was
		// thrown out, while windows that opened in a panic and settled were kept.
		// The tail a backspread is bought for was being filtered out of the sample
		// used to price it.
		at := i - 1
		if at < 0 || at >= len(rolling) || math.IsNaN(rolling[at]) {
			continue
		}
		if rolling[at] < now*(1-near) || rolling[at] > now*(1+near) {
			continue
		}
		moves = append(moves, closes[i+days]/closes[i])
	}
	if len(moves) < 60 {
		return nil, 0, errors.New("too few windows in weather like today's to say anything: widen the history")
	}

	return moves, now, nil
}

func deviation(of []float64) float64 {
	if len(of) < 2 {
		return 0
	}
	mean := 0.0
	for _, x := range of {
		mean += x
	}
	mean /= float64(len(of))

	sum := 0.0
	for _, x := range of {
		sum += (x - mean) * (x - mean)
	}

	return math.Sqrt(sum / float64(len(of)-1))
}

// tradingDaysUntil counts weekdays from now to expiry, the day itself included.
//
// Compared as DATES and not as instants. Both come from different places - one
// is a clock reading with an afternoon in it, the other a calendar day at
// midnight - and comparing them whole dropped the expiration itself, because
// three in the afternoon is after midnight of the same day. Sigma came out a
// quarter short and every distance with it.
//
// And the date is the EXCHANGE'S, not the machine's. This team works from UTC+8:
// Thursday evening in New York is already Friday morning here, so a Friday
// expiration counted from the local date left no days at all and the tool
// refused every American afternoon. Mid-week the same shift lost a day, which
// makes sigma smaller and every distance measured in it LARGER - so a leg
// standing closer than the declaration allows would pass as permitted. That is
// the mistake of 28 August coming back through the clock.
//
// Holidays are not in it: the broker's calendar would answer better, and being
// one day long on Labor Day week moves sigma by three percent, which does not
// change which placement wins.
func tradingDaysUntil(now, expiry time.Time, where *time.Location) int {
	if where == nil {
		where = time.UTC
	}
	here := now.In(where)
	from := time.Date(here.Year(), here.Month(), here.Day(), 0, 0, 0, 0, time.UTC)
	until := time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, time.UTC)

	days := 0
	for day := from.AddDate(0, 0, 1); !day.After(until); day = day.AddDate(0, 0, 1) {
		switch day.Weekday() {
		case time.Saturday, time.Sunday:
		default:
			days++
		}
	}

	return days
}
