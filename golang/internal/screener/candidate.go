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
	// Cost is what crossing the book costs once: both legs' bid-ask added. It is
	// what a fill at the far side would give up against the midpoint.
	Cost float64 `json:"cost"`
	// CreditAfterCost is the credit with half that crossing taken out - what an
	// order sent at the midpoint and walked toward the book is worth in
	// expectation, rather than what the screen displays.
	//
	// Every measure below is computed from this and not from Credit. A structure
	// paying 0.20 with a 0.16 spread and one paying 0.12 with a 0.02 spread look
	// like a clear win for the first and are the opposite.
	CreditAfterCost float64 `json:"credit_after_cost"`
	// Delta is the broker's own reading of how likely the sold strike is to finish
	// in the money. Nil where the broker computes none, which is the day the
	// contract expires.
	Delta *float64 `json:"short_delta,omitempty"`
	// Edge is what the structure pays against what it must survive, in percentage
	// points: the chance the broker's own delta gives the sold strike of expiring
	// worthless, less the share of the time CreditAfterCost has to win to break
	// even.
	//
	// Positive means the market is paying more for this risk than the same market
	// says the risk is worth. It is a SCREEN and not a promise: delta is a
	// risk-neutral probability rather than a real one, and a spread does not lose
	// its whole width when the strike is touched. But it ranks on both halves at
	// once, which neither credit-to-risk nor delta does alone - on 26 August a
	// delta ceiling threw away ORCL at +3.5, TSLA at +1.6 and INTC at +1.5 for
	// being 0.30, while keeping nothing better.
	Edge *float64 `json:"edge_points,omitempty"`
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
	// MostCreditToRisk is the most a structure may pay before it is read as a
	// broken quote rather than an opportunity. A vertical sold OUT of the money
	// cannot honestly pay more than its width: at 100 percent the credit already
	// equals the risk, which would mean the market thinks a strike it placed out
	// of the money is more likely than not to be crossed.
	//
	// This matters because the list is ranked by exactly the number bad data
	// inflates. The first live sweep put a TSLA call paying 468 percent at the
	// top - a spread two and a half dollars wide quoted at a credit of 2.06 -
	// and without this the session would have been shown garbage first.
	MostCreditToRisk float64
	// MaxCostShare is the most crossing the book may cost, as a percent of credit.
	//
	// This is a bound on nonsense, not a way of choosing: what the crossing costs
	// is already taken out of the credit before anything is measured, so an
	// expensive structure loses on Edge without needing a threshold. Set it wide.
	// It was 20 for one afternoon and threw away 833 structures in a single sweep
	// against 96 for the next filter - a number picked from one good trade and
	// three bad ones, deciding the day.
	MaxCostShare float64
	// MostDelta is how likely the sold strike may be to finish in the money, as
	// the broker's own delta, absolute. Distance in percent is not the same
	// measure: a strike one percent away is far on a quiet index and near on a
	// share that moves five percent a day. Ranking by distance alone offered
	// structures at delta 0.47 to a rule that wants 0.15, and the session threw
	// every one of them away - a list nobody can act on is worse than no list,
	// because it costs a turn to reject.
	//
	// Zero leaves delta unchecked.
	MostDelta float64
	// LeastEdge is the least a structure may pay above what it must survive, in
	// percentage points. Zero leaves it unchecked.
	LeastEdge float64
}

// Refused counts why structures did not make the list, by the filter that
// stopped each one.
//
// A sweep that reads 284 underlyings and returns one structure says nothing
// about which of its own filters did that, and the difference decides what to
// change: a market with no opportunity in it and a threshold set too tight look
// identical from the outside. This is what tells them apart.
type Refused map[string]int

func (r Refused) note(reason string) { r[reason]++ }

// The reasons, one per place a structure can be dropped.
const (
	RefusedNoQuote       = "no two-sided quote"
	RefusedNoCredit      = "no credit or no risk"
	RefusedDistance      = "distance from the price"
	RefusedPaysTooLittle = "pays too little for the risk"
	RefusedPaysTooMuch   = "pays more than its width, so the quote is broken"
	RefusedNoDelta       = "the broker computes no delta"
	RefusedDelta         = "too likely to be crossed"
	RefusedCost          = "the crossing costs more of the credit than the sanity bound"
	RefusedEatenByCost   = "the crossing eats the whole credit"
	RefusedEdge          = "pays less than what it must survive"
)

// Best returns the best put spread and the best call spread this underlying
// offers on one expiration, or nothing where the book will not price one.
//
// "Best" is the highest credit for the risk among those that clear Wanted. A
// structure whose legs lack a two-sided quote is not ranked low, it is absent:
// half a price is worse than no price.
func Best(underlying string, price float64, contracts []marketdata.Contract,
	quotes map[string]marketdata.Quote, want Wanted, refused Refused) []Candidate {

	if price <= 0 {
		return nil
	}

	// Grouped by type AND expiration. Two legs of different expirations are a
	// different structure entirely - a calendar, not a vertical - and pricing one
	// as the other gives a risk that does not exist: the width between strikes
	// bounds the loss only when both legs die on the same day.
	type series struct {
		kind    string
		expires string
	}
	bySeries := map[series][]marketdata.Contract{}
	for _, contract := range contracts {
		key := series{contract.Type, contract.Expiration.Format(time.DateOnly)}
		bySeries[key] = append(bySeries[key], contract)
	}

	var found []Candidate
	best := map[string]Candidate{}
	for key, list := range bySeries {
		kind := key.kind
		sort.Slice(list, func(i, j int) bool { return list[i].Strike < list[j].Strike })

		for i := 1; i < len(list); i++ {
			// A put spread sells the higher strike and buys the lower; a call
			// spread the other way round. Both sell the leg nearer the money.
			short, long := list[i], list[i-1]
			if kind == "call" {
				short, long = list[i-1], list[i]
			}

			candidate, ok := price_(underlying, kind, price, short, long, quotes, want, refused)
			if !ok {
				continue
			}
			// One best per side across every expiration: the session wants the best
			// put and the best call, not one of each per day.
			if kept, have := best[kind]; !have || richer(candidate, kept) {
				best[kind] = candidate
			}
		}
	}
	for _, candidate := range best {
		found = append(found, candidate)
	}

	sort.Slice(found, func(i, j int) bool { return richer(found[i], found[j]) })

	return found
}

func price_(underlying, kind string, price float64,
	short, long marketdata.Contract, quotes map[string]marketdata.Quote,
	want Wanted, refused Refused) (Candidate, bool) {

	shortQuote, haveShort := quotes[short.Symbol]
	longQuote, haveLong := quotes[long.Symbol]
	if !haveShort || !haveLong {
		refused.note(RefusedNoQuote)
		return Candidate{}, false
	}
	if shortQuote.Bid <= 0 || shortQuote.Ask <= 0 || longQuote.Bid <= 0 || longQuote.Ask <= 0 {
		refused.note(RefusedNoQuote)
		return Candidate{}, false
	}

	width := math.Abs(short.Strike - long.Strike)
	if width <= 0 {
		refused.note(RefusedNoCredit)
		return Candidate{}, false
	}

	// Sold at what a buyer pays, bought at what a seller asks: the credit the
	// book would actually give, not the midpoint it displays.
	credit := (shortQuote.Bid+shortQuote.Ask)/2 - (longQuote.Bid+longQuote.Ask)/2
	risk := width - credit
	if credit <= 0 || risk <= 0 {
		refused.note(RefusedNoCredit)
		return Candidate{}, false
	}

	out := (price - short.Strike) / price * 100
	if kind == "call" {
		out = (short.Strike - price) / price * 100
	}
	if out < want.MinOutOfTheMoney || out > want.MaxOutOfTheMoney {
		refused.note(RefusedDistance)
		return Candidate{}, false
	}

	toRisk := credit / risk * 100
	if toRisk < want.MinCreditToRisk {
		refused.note(RefusedPaysTooLittle)
		return Candidate{}, false
	}
	if want.MostCreditToRisk > 0 && toRisk > want.MostCreditToRisk {
		refused.note(RefusedPaysTooMuch)
		return Candidate{}, false
	}

	if want.MostDelta > 0 {
		// Absent delta is not "within the limit": the broker computes none on the
		// day a contract expires, and that is exactly when the sold strike is most
		// likely to be crossed.
		if shortQuote.Delta == nil {
			refused.note(RefusedNoDelta)
			return Candidate{}, false
		}
		if math.Abs(*shortQuote.Delta) > want.MostDelta {
			refused.note(RefusedDelta)
			return Candidate{}, false
		}
	}

	cost := (shortQuote.Ask - shortQuote.Bid) + (longQuote.Ask - longQuote.Bid)
	share := cost / credit * 100
	if share > want.MaxCostShare {
		refused.note(RefusedCost)
		return Candidate{}, false
	}

	// An order goes out at the midpoint and is walked toward the book, so half the
	// crossing is what it gives up in expectation. Taking it out here is what makes
	// the cost part of the measure instead of a threshold beside it.
	//
	// Half is not a guess. Measured over this project's own 34 fills on 26 August:
	// 0.0229 conceded on average against the price first asked, and 20 of the 34
	// filled at that price exactly. Half a typical spread on these structures is
	// two to four cents, so the charge matches what the book actually took.
	net := credit - cost/2
	netRisk := width - net
	if net <= 0 || netRisk <= 0 {
		refused.note(RefusedEatenByCost)
		return Candidate{}, false
	}

	var edge *float64
	if shortQuote.Delta != nil {
		survives := 1 - math.Abs(*shortQuote.Delta)
		breakEven := netRisk / (net + netRisk)
		points := round((survives - breakEven) * 100)
		edge = &points
		if want.LeastEdge != 0 && points < want.LeastEdge {
			refused.note(RefusedEdge)
			return Candidate{}, false
		}
	} else if want.LeastEdge != 0 {
		// No delta, no way to weigh what it pays against what it survives.
		refused.note(RefusedNoDelta)
		return Candidate{}, false
	}

	return Candidate{
		Underlying: underlying, Type: kind, Expiration: short.Expiration,
		Short: short.Symbol, Long: long.Symbol,
		ShortStrike: short.Strike, LongStrike: long.Strike,
		Price: price, OutOfTheMoney: round(out),
		Credit: round(credit), Risk: round(risk), CreditToRisk: round(toRisk),
		Cost: round(cost), CostShare: round(share), CreditAfterCost: round(net),
		Delta: shortQuote.Delta, Edge: edge,
	}, true
}

// richer compares on what a structure pays against what it must survive, and
// falls back to what it pays against what it risks where no delta was given.
func richer(one, than Candidate) bool {
	if one.Edge != nil && than.Edge != nil {
		return *one.Edge > *than.Edge
	}
	if one.Edge != nil {
		return true
	}
	if than.Edge != nil {
		return false
	}

	return one.CreditToRisk > than.CreditToRisk
}

func round(value float64) float64 { return math.Round(value*100) / 100 }
