// Package screener finds, in code, the structures worth a session's attention.
//
// A session that hunts for itself can look at six underlyings before its turn
// runs out, and the six are whichever the schedule handed it - so an opportunity
// on the seventh is not rejected, it is never seen. What decides whether a credit
// spread earns is measurable without judgement: what it pays against what it
// risks, and what the round trip costs against what it pays. Both are arithmetic
// over quotes.
//
// So the arithmetic runs here, over hundreds of names, and the session is handed
// a ranked shortlist. It still decides what to take, how much, and whether to
// trade at all: nothing here sends an order or knows what an account holds.
package screener

import (
	"math"
	"sort"
	"time"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// Candidate is one vertical credit spread, priced.
type Candidate struct {
	Underlying string    `json:"underlying"`
	Type       string    `json:"type"`
	Expiration time.Time `json:"expiration"`
	// Short is the leg sold, Long the leg bought one strike further out.
	Short       string  `json:"short"`
	Long        string  `json:"long"`
	ShortStrike float64 `json:"short_strike"`
	LongStrike  float64 `json:"long_strike"`
	Price       float64 `json:"underlying_price"`
	// OutOfTheMoney is how far the sold strike sits from the price, in percent.
	OutOfTheMoney float64 `json:"out_of_the_money_percent"`
	Credit        float64 `json:"credit"`
	Risk          float64 `json:"risk"`
	// CreditToRisk is what the structure pays for what it risks, in percent.
	CreditToRisk float64 `json:"credit_to_risk_percent"`
	// Cost is the round trip: both legs' bid-ask added.
	Cost float64 `json:"cost"`
	// CostShare is that cost as a percent of the credit. This is the number that
	// separated what earned from what lost on 25-26 August: the structures that
	// paid had it near 10, the ones that lost had it above 100.
	CostShare float64 `json:"cost_share_percent"`
}

// Wanted is what a candidate has to clear to be worth listing. These are not the
// session's rules - the session applies its own, and stricter. They only keep the
// list short enough to read.
type Wanted struct {
	// MinOutOfTheMoney and MaxOutOfTheMoney bound how far the sold strike sits
	// from the price, in percent.
	MinOutOfTheMoney, MaxOutOfTheMoney float64
	// MinCreditToRisk is the least a structure may pay, in percent.
	MinCreditToRisk float64
	// MaxCostShare is the most the round trip may cost, as a percent of credit.
	MaxCostShare float64
}

// Best returns the best put spread and the best call spread this underlying
// offers on one expiration, or nothing where the book will not price one.
//
// "Best" is the highest credit for the risk among those that clear Wanted. A
// structure whose legs lack a two-sided quote is not ranked low, it is absent:
// half a price is worse than no price.
func Best(underlying string, price float64, contracts []marketdata.Contract,
	quotes map[string]marketdata.Quote, want Wanted) []Candidate {

	if price <= 0 {
		return nil
	}

	byKind := map[string][]marketdata.Contract{}
	for _, contract := range contracts {
		byKind[contract.Type] = append(byKind[contract.Type], contract)
	}

	var found []Candidate
	for kind, list := range byKind {
		sort.Slice(list, func(i, j int) bool { return list[i].Strike < list[j].Strike })

		var best Candidate
		haveBest := false
		for i := 1; i < len(list); i++ {
			// A put spread sells the higher strike and buys the lower; a call
			// spread the other way round. Both sell the leg nearer the money.
			short, long := list[i], list[i-1]
			if kind == "call" {
				short, long = list[i-1], list[i]
			}

			candidate, ok := price_(underlying, kind, price, short, long, quotes, want)
			if !ok {
				continue
			}
			if !haveBest || candidate.CreditToRisk > best.CreditToRisk {
				best, haveBest = candidate, true
			}
		}
		if haveBest {
			found = append(found, best)
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].CreditToRisk > found[j].CreditToRisk })

	return found
}

func price_(underlying, kind string, price float64,
	short, long marketdata.Contract, quotes map[string]marketdata.Quote, want Wanted) (Candidate, bool) {

	shortQuote, haveShort := quotes[short.Symbol]
	longQuote, haveLong := quotes[long.Symbol]
	if !haveShort || !haveLong {
		return Candidate{}, false
	}
	if shortQuote.Bid <= 0 || shortQuote.Ask <= 0 || longQuote.Bid <= 0 || longQuote.Ask <= 0 {
		return Candidate{}, false
	}

	width := math.Abs(short.Strike - long.Strike)
	if width <= 0 {
		return Candidate{}, false
	}

	// Sold at what a buyer pays, bought at what a seller asks: the credit the
	// book would actually give, not the midpoint it displays.
	credit := (shortQuote.Bid+shortQuote.Ask)/2 - (longQuote.Bid+longQuote.Ask)/2
	risk := width - credit
	if credit <= 0 || risk <= 0 {
		return Candidate{}, false
	}

	out := (price - short.Strike) / price * 100
	if kind == "call" {
		out = (short.Strike - price) / price * 100
	}
	if out < want.MinOutOfTheMoney || out > want.MaxOutOfTheMoney {
		return Candidate{}, false
	}

	toRisk := credit / risk * 100
	if toRisk < want.MinCreditToRisk {
		return Candidate{}, false
	}

	cost := (shortQuote.Ask - shortQuote.Bid) + (longQuote.Ask - longQuote.Bid)
	share := cost / credit * 100
	if share > want.MaxCostShare {
		return Candidate{}, false
	}

	return Candidate{
		Underlying: underlying, Type: kind, Expiration: short.Expiration,
		Short: short.Symbol, Long: long.Symbol,
		ShortStrike: short.Strike, LongStrike: long.Strike,
		Price: price, OutOfTheMoney: round(out),
		Credit: round(credit), Risk: round(risk), CreditToRisk: round(toRisk),
		Cost: round(cost), CostShare: round(share),
	}, true
}

func round(value float64) float64 { return math.Round(value*100) / 100 }
