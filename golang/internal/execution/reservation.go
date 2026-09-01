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

// The edge the session says its worst price still clears, travelling the same
// way and for a reason the floor alone cannot serve.
//
// A floor is a price, and a price says nothing about whether the trade is still
// worth taking there. On 1 September a session entered on "edge at least +3" and
// named a worst price whose edge was +2.53; the ladder walked to it in
// forty-five seconds and was saved only by a book that stood eight cents away.
// The ladder cannot recompute this - it sees an order, never a structure, so it
// has neither the width nor the delta - so the party holding the quotes states
// the claim and the ladder holds it to the declaration.
const edgePrefix = "edge="

// NameFor is the name a session gives an order that states the worst price it
// accepts. It exists here so the one format has one author.
func NameFor(worst float64) string {
	return fmt.Sprintf("%s%.2f", reservationPrefix, worst)
}

// NameStating adds the edge at that worst price to the name above.
func NameStating(worst, edge float64) string {
	return fmt.Sprintf("%s;%s%.2f", NameFor(worst), edgePrefix, edge)
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
func NameCarrying(order marketdata.Order, at time.Time) string {
	fresh := strconv.FormatInt(at.UnixNano(), 10)

	// The tail this package added last time is dropped rather than kept, or an
	// order walked twenty times carries twenty timestamps and runs past what the
	// broker will hold. It is recognised by being the only field that is a bare
	// whole number: everything a session writes is `key=value` or words.
	fields := strings.Split(order.ClientID, ";")
	for len(fields) > 0 {
		last := strings.TrimSpace(fields[len(fields)-1])
		if _, err := strconv.ParseInt(last, 10, 64); err != nil && last != "" {
			break
		}
		fields = fields[:len(fields)-1]
	}
	if len(fields) == 0 {
		return fresh
	}

	return strings.Join(fields, ";") + ";" + fresh
}

// Reservation reads the worst price out of an order's name. An order that names
// none is left where it stands: there is no honest way to invent this number
// here. A share of the price placed cannot serve as one either - recomputed from
// the price it just moved to, it ratchets, and gives away the whole credit a cent
// at a time.
func Reservation(order marketdata.Order) (float64, bool) {
	return stated(order.ClientID, reservationPrefix)
}

// EdgeAt reads the edge the session says its worst price still clears.
func EdgeAt(order marketdata.Order) (float64, bool) {
	return stated(order.ClientID, edgePrefix)
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
