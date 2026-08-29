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

// NameFor is the name a session gives an order that states the worst price it
// accepts. It exists here so the one format has one author.
func NameFor(worst float64) string {
	return fmt.Sprintf("%s%.2f", reservationPrefix, worst)
}

// NameCarrying is the name a replacement gets: the same floor, and something the
// broker has not seen before. Measured on the account - the broker refuses an
// order whose name it already knows ("client_order_id must be unique"), so
// passing the old name through refuses every step of the walk.
func NameCarrying(worst float64, at time.Time) string {
	return fmt.Sprintf("%s;%d", NameFor(worst), at.UnixNano())
}

// Reservation reads the worst price out of an order's name. An order that names
// none is left where it stands: there is no honest way to invent this number
// here. A share of the price placed cannot serve as one either - recomputed from
// the price it just moved to, it ratchets, and gives away the whole credit a cent
// at a time.
func Reservation(order marketdata.Order) (float64, bool) {
	name := order.ClientID
	at := strings.Index(name, reservationPrefix)
	if at < 0 {
		return 0, false
	}

	rest := name[at+len(reservationPrefix):]
	if cut := strings.IndexAny(rest, " ;,"); cut >= 0 {
		rest = rest[:cut]
	}

	worst, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		return 0, false
	}

	return round(worst), true
}
