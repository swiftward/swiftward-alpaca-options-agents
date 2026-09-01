package execution

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// The worst price a structure may be filled at is the session's decision, made
// when it decided to trade at all. It travels with the order: the session writes
// it into the name it gives the order, which the broker carries untouched and
// returns on every read.
//
// The book is not that bound. A book can move away while an order rests in it,
// and a ladder that follows the book wherever it goes gives away the whole
// credit one tick at a time - slowly enough that nobody notices until the week
// is over.
const reservationPrefix = "worst="

// nameLimit is the longest name Alpaca accepts for an order. A replacement past
// it is refused, and a refused replacement reads as an order that will not walk -
// which patience then cancels.
const nameLimit = 128

// NameFor is the name a session gives an order that states the worst price it
// accepts. It exists here so the one format has one author.
func NameFor(worst float64) string {
	return fmt.Sprintf("%s%.2f", reservationPrefix, worst)
}

// NameCarrying is the name a replacement gets: everything the session declared
// about this order, and something the broker has not seen before. Measured on
// the account - the broker refuses an order whose name it already knows
// ("client_order_id must be unique"), so passing the old name through refuses
// every step of the walk.
//
// The whole name is kept and only the uniqueness tail is replaced. Rebuilding it
// from the fields this package knows drops everything else the session wrote -
// including `turn=`, which is the only thing joining a filled order back to the
// intent behind it, and which every replacement therefore used to lose.
//
// It is bounded, because a broker refuses a name past its own limit and the
// refusal reads as an order that will not walk. What is kept when it does not fit
// is the floor: that is the only field anything reads, and a name without it
// leaves the order resting where it was placed.
func NameCarrying(order marketdata.Order, at time.Time) string {
	fresh := strconv.FormatInt(at.UnixNano(), 10)

	// The stamp this package added last time is dropped, or an order walked twenty
	// times carries twenty of them. Only the LAST one, and only if it is a bare
	// whole number: a session's own trailing field can be a date, and dropping
	// every one of them would eat it too.
	fields := strings.Split(order.ClientID, ";")
	if last := len(fields) - 1; last >= 0 {
		if _, err := strconv.ParseInt(strings.TrimSpace(fields[last]), 10, 64); err == nil {
			fields = fields[:last]
		}
	}

	kept := strings.Join(fields, ";")
	if kept != "" && len(kept)+1+len(fresh) <= nameLimit {
		return kept + ";" + fresh
	}
	// Too long to keep whole. The floor is the only field anything reads, and an
	// order that loses it stops walking altogether.
	if floor, named := Reservation(order); named {
		if short := NameFor(floor); len(short)+1+len(fresh) <= nameLimit {
			return short + ";" + fresh
		}
	}

	return fresh
}

// Reservation reads the worst price out of an order's name. An order that names
// none is left where it stands: there is no honest way to invent this number
// here. A share of the price placed cannot serve as one either - recomputed from
// the price it just moved to, it ratchets, and gives away the whole credit a cent
// at a time.
func Reservation(order marketdata.Order) (float64, bool) {
	return stated(order.ClientID, reservationPrefix)
}

// stated reads one number a session wrote into an order's name. The rest of the
// name is the session's own text, so the field is found by splitting on the
// separators a session writes between its words and matching a WHOLE field - not
// by searching for the prefix anywhere in the string. A search would read
// `min_edge=3` as the edge and walk to a floor this refuses.
//
// A value that is not a finite number is no value: `strconv.ParseFloat` accepts
// "NaN" and "Inf", and a NaN compares false against every bound, so it would not
// pass a check, it would fail every one of them - an order stranded until
// patience by a word in its own name.
func stated(name, prefix string) (float64, bool) {
	for _, field := range strings.FieldsFunc(name, func(r rune) bool {
		return r == ';' || r == ',' || r == ' '
	}) {
		if !strings.HasPrefix(field, prefix) {
			continue
		}
		value, err := strconv.ParseFloat(field[len(prefix):], 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}

		return round(value), true
	}

	return 0, false
}
