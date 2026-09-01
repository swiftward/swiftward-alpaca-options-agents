package execution

import (
	"fmt"
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
// It is built from the ORDER rather than from a floor handed in, because a
// replacement that keeps the floor and drops the edge would pass the gate below
// on its first step and every step after it.
func NameCarrying(order marketdata.Order, at time.Time) string {
	worst, named := Reservation(order)
	if !named {
		return fmt.Sprintf("%d", at.UnixNano())
	}
	name := NameFor(worst)
	if edge, stated := EdgeAt(order); stated {
		name = NameStating(worst, edge)
	}

	return fmt.Sprintf("%s;%d", name, at.UnixNano())
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

// stated reads one number a session wrote into an order's name. The name is the
// session's own text with these segments in it, so it is read from wherever the
// prefix appears and ends at whatever separates the session's words.
func stated(name, prefix string) (float64, bool) {
	at := strings.Index(name, prefix)
	if at < 0 {
		return 0, false
	}

	rest := name[at+len(prefix):]
	if cut := strings.IndexAny(rest, " ;,"); cut >= 0 {
		rest = rest[:cut]
	}

	value, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		return 0, false
	}

	return round(value), true
}
