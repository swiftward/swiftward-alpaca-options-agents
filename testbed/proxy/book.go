// A participant's book in the arena: the paper account the broker does not have.
//
// The arena does not trade on a real account and cannot: orders here never reach
// Alpaca at all. The reads, meanwhile, are real - quotes, chains and the clock
// go straight through. That is the whole point of the instrument: the agent sees
// a real market and believes it is trading, while we keep the book, one
// participant per token.
//
// There is one measure of success: behaving like a REAL exchange rather than
// like Alpaca's paper account. The paper account fills an order at the limit
// without looking at the book, and that is how profit that does not exist gets
// drawn. Here the order stands until the market reaches it.
package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// perContract is the broker's fee per contract per leg. Measured by the team on
// live orders on 25-27 August; it is here so that small credits do not look more
// profitable than they are.
const perContract = 0.025

// multiplier is how much of the underlying one contract carries. A hundred
// everywhere we trade; non-standard contracts left behind by splits and mergers
// do not reach here, and that is a limitation taken deliberately rather than a
// case forgotten.
const multiplier = 100

// Asset classes in Alpaca's words. Shares appear in the book not from an order
// but from an option being exercised at expiry - which is exactly why the book
// has to be able to hold them.
const (
	classOption = "us_option"
	classEquity = "us_equity"
)

// The order states are Alpaca's vocabulary, not ours. An agent reads them with
// its own code, written against a real broker, and any word of our own here
// would break that parsing silently.
const (
	statusNew      = "new"
	statusPartial  = "partially_filled"
	statusFilled   = "filled"
	statusCanceled = "canceled"
	statusExpired  = "expired"
	statusReplaced = "replaced"
)

// Leg is one leg of an order in the shape the harness sends it.
type Leg struct {
	Symbol   string `json:"symbol"`
	Side     string `json:"side"`      // buy | sell
	RatioQty string `json:"ratio_qty"` // a string, as in their code
	// Ratio is the same number, parsed. It is parsed ONCE, at submission, and an
	// order with an unreadable ratio_qty does not get that far at all. Earlier
	// code parsed the string in every computation with a function that quietly
	// returned one for "2.0", " 2" and "0": the ratio of a spread changed by
	// itself, the risk stopped being the risk that was agreed, and the fee came
	// out half of what it should be.
	Ratio          int    `json:"ratio"`
	PositionIntent string `json:"position_intent"` // *_to_open | *_to_close
}

// Position is what a participant holds. The key is a contract symbol or a share
// ticker.
type Position struct {
	Symbol   string
	Qty      int     // signed: a minus means sold
	AvgPrice float64 // average entry price per contract (per share for shares)
	Class    string
	// Mark is the last price known. It is needed where collateral is counted
	// without going to the network: a short position in shares demands
	// collateral, and the requirement cannot be counted without a price. An empty
	// price falls back to the entry price - an understatement, and one that is
	// said out loud rather than hidden.
	Mark float64
}

// Order is an order that STANDS. That is the main difference from the earlier
// arena, where an order filled inside the call: a laddering routine walks and
// cancels orders, and an order that filled instantly leaves it nothing to walk.
type Order struct {
	ID       string
	ClientID string
	// TurnRef is the turn the order was born in. It is taken from the tail of
	// ClientID as the field turn=<ref>, which the agent receives from
	// record_intent and puts there itself.
	//
	// Without it the judge cannot check an intent against the deed: the intent
	// sits in their record with a turn reference, the order sits with us, and
	// there is no key common to the two sources. Under codex it exists through
	// tool_calls.turn_ref; through the mailbox the calls are not visible at all.
	// A key absent from the data collected does not appear after the fact - which
	// is why it was needed before Monday.
	TurnRef   string
	Status    string
	Qty       int // how many sets were asked for
	FilledQty int // how many sets were filled
	Limit     float64
	// Market is an order with no limit. The broker's schema declares type =
	// "market" as the default, and the Limit of such an order is not a price that
	// was asked for but a valuation at the moment of submission: collateral is
	// held against it.
	Market    bool
	FilledAvg float64 // average price of a set, signed: a minus is a credit
	Fees      float64
	// Stand marks a fill that happened in bench mode (-ignore-session), that is,
	// against a closed market or a stale quote. It is NOT a reason for refusal
	// and not a note: a note lives only while the order is open, and this fact
	// has to outlive the fill - otherwise, a week later, a fill against a frozen
	// book is indistinguishable from a real one.
	Stand bool
	TIF   string
	Legs  []Leg

	SubmittedAt time.Time
	FilledAt    time.Time
	CanceledAt  time.Time
	// ExpiresAt is the end of the session the order lives in. It is taken from
	// next_close on the broker's own clock: an order submitted on a Friday
	// evening lives until Monday's close, exactly as at Alpaca, rather than going
	// out a second later.
	ExpiresAt time.Time

	ReplacedBy string
	Replaces   string
	// Why is why the order has still not filled. An agent reads it and decides
	// whether to move the price; an order that says nothing is indistinguishable
	// from a lost one.
	Why string
	Seq int
}

func (o *Order) open() bool {
	return o.Status == statusNew || o.Status == statusPartial
}

func (o *Order) remaining() int {
	return o.Qty - o.FilledQty
}

// Event is what happened to the account. An order that did not fill is an event
// too: it shows that the agent asked for a price the market did not have.
type Event struct {
	OrderID string
	Kind    string // fill | expiry | exercise | assignment | rejected
	Symbol  string
	At      time.Time
	Sets    int
	Price   float64 // per set, signed: a minus means a credit was received
	Fees    float64
	Filled  bool
	Why     string
}

// Book is one participant's account.
type Book struct {
	mu sync.Mutex

	// Hash is what names a participant in the file. The token itself never
	// reaches the database: it is an access key, not a name.
	Hash string
	// Name is what people call the participant. A hash serves a machine and does
	// not serve the judge: records are told apart by the name of a database and
	// books by a hash, and without a name the only way to join them is the order
	// they were created in. Order is a coincidence, not a join.
	Name       string
	Cash       float64
	Start      float64
	LastEquity float64
	// ClosedOn is the New York date for which closing equity has already been
	// written. Without it last_equity would be rewritten on every tick after the
	// close and the day would show a flat result.
	ClosedOn  string
	Positions map[string]*Position
	Orders    map[string]*Order
	// order is the order of submission. Orders fill in it, because the size shown
	// on the book is one size for everyone: whoever queued first takes it.
	order  []string
	Events []Event

	seq         int
	savedEvents int
	store       *Store
}

func NewBook(hash string, start float64, store *Store) *Book {
	return &Book{
		Hash:       hash,
		Cash:       start,
		Start:      start,
		LastEquity: start,
		Positions:  map[string]*Position{},
		Orders:     map[string]*Order{},
		store:      store,
	}
}

// Submit puts an order into the book. It is NOT filled here: filling is the
// matcher's job, and the matcher looks at the market on its own tick.
//
// Collateral is checked at once and against the order's full size: a real broker
// refuses at submission rather than letting an order stand and discovering the
// shortfall at the fill. A refusal here is a refusal - the order is not created
// at all.
func (b *Book) Submit(o *Order) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// The turn is taken from the name the agent gave the order, HERE rather than
	// in the caller. It was in the caller once, in exactly one of the two paths:
	// Replace took the turn out and placeOrder did not, so every new order went
	// into the book with an empty turn while replacements carried one. Nothing
	// showed it until the turn started arriving at all - a field nobody filled
	// reads exactly like a field nobody sent.
	o.TurnRef = turnOf(o.ClientID)

	// We compute the book as it will be if EVERY standing order fills and this
	// one along with them, each at its own limit. At the limit rather than at the
	// market: the market at the fill may be better, and taking it now would mean
	// approving the order at a price that will not be there when it fills.
	// Together with the standing ones rather than alone: ten identical sold
	// spreads each pass the collateral check on their own and together ruin the
	// account - a broker holds collateral against an order from the moment it is
	// submitted, and so do we.
	projected, cashAfter := b.reserved(o, "")
	need, err := Requirement(projected)
	if err != nil {
		return fmt.Errorf("the collateral cannot be counted: %w", err)
	}
	if cashAfter < need-1e-9 {
		return fmt.Errorf("not enough collateral: %s", shortfall(projected, need, cashAfter))
	}

	b.seq++
	o.Seq = b.seq
	if o.ID == "" {
		// A UUID rather than "arena-7": the schema of the real cancel_order_by_id
		// declares order_id as a uuid, and a client that checks the schema would
		// throw a format of our own away.
		o.ID = uuid.NewString()
	}
	o.Status = statusNew
	b.Orders[o.ID] = o
	b.order = append(b.order, o.ID)

	return b.persist()
}

// Fill books the part of an order the market has reached.
//
// executable is the net price of a set against the opposite sides of the book: a
// buy pays the ask, a sell takes the bid. The sign is theirs: a minus is a
// credit.
func (b *Book) Fill(id string, sets int, executable float64, legPrice map[string]float64, now time.Time) (Event, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	o := b.Orders[id]
	if o == nil {
		return Event{}, fmt.Errorf("there is no order %s", id)
	}
	if !o.open() {
		return Event{}, fmt.Errorf("order %s is already %s", id, o.Status)
	}
	if sets < 1 || sets > o.remaining() {
		return Event{}, fmt.Errorf("%d sets against %d remaining", sets, o.remaining())
	}

	e := Event{OrderID: id, Kind: "fill", At: now, Sets: sets, Price: executable}

	// Collateral. We compute the book as it will be and require the collateral to
	// suffice. Without this check a participant sells a million dollars' worth of
	// spreads and "earns" a credit it has nothing to back.
	projected := b.project(o.Legs, sets)
	need, err := Requirement(projected)
	if err != nil {
		o.Why = fmt.Sprintf("the collateral cannot be counted: %v", err)

		return Event{}, err
	}
	fees := float64(contractsIn(o.Legs, sets)) * perContract
	cashAfter := b.Cash - executable*float64(sets)*multiplier - fees
	if cashAfter < need-1e-9 {
		o.Why = "not enough collateral: " + shortfall(projected, need, cashAfter)
		e.Why = o.Why
		b.Events = append(b.Events, e)

		return e, b.persist()
	}

	b.apply(o.Legs, sets, legPrice)

	// The sign: executable is negative on a credit, so money comes in.
	b.Cash = cashAfter
	o.Fees += fees
	// The average fill price is weighted by sets: an order topped up at a worse
	// price is obliged to show that price in its average, or the agent counts its
	// takings from the first part and never sees what the second cost.
	o.FilledAvg = (o.FilledAvg*float64(o.FilledQty) + executable*float64(sets)) / float64(o.FilledQty+sets)
	o.FilledQty += sets
	o.FilledAt = now
	o.Why = ""
	if o.FilledQty >= o.Qty {
		o.Status = statusFilled
	} else {
		o.Status = statusPartial
	}

	e.Fees = fees
	e.Filled = true
	b.Events = append(b.Events, e)

	return e, b.persist()
}

// MarkStand marks an order as filled in bench mode. Unlike Note it works after
// the fill as well: this is a fact about the fill, not a reason for its
// absence.
func (b *Book) MarkStand(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if o := b.Orders[id]; o != nil && !o.Stand {
		o.Stand = true
		b.persist() //nolint:errcheck // the next change rewrites it
	}
}

// Note writes the reason an order has NOT YET filled, and so stays silent on a
// closed one: a filled order has no reason for not filling. Anything that has to
// outlive the fill goes into a field of its own rather than here.
func (b *Book) Note(id, why string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if o := b.Orders[id]; o != nil && o.open() && o.Why != why {
		o.Why = why
		b.persist() //nolint:errcheck // a reason is not money: unwritten, it is rewritten next tick
	}
}

func (b *Book) Cancel(id string, now time.Time) (Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	o := b.Orders[id]
	if o == nil {
		return Order{}, fmt.Errorf("there is no order %s", id)
	}
	if !o.open() {
		return Order{}, fmt.Errorf("order %s is already %s: there is nothing to cancel", id, o.Status)
	}

	// A partly filled order cancels into a filled one: what is filled cannot be
	// cancelled, and Alpaca shows such an order as canceled with filled_qty above
	// zero. We keep that status and add the mark of cancellation.
	o.Status = statusCanceled
	o.CanceledAt = now
	o.Why = "cancelled by the participant"

	return *o, b.persist()
}

// Replace moves an order's price. At Alpaca this is NOT an edit in place: the
// old order becomes replaced and its remainder goes into a new one with a new
// identifier. An agent that does not know this will lose sight of the order -
// but lying here would teach it code that does not work at a real broker.
func (b *Book) Replace(id string, limit float64, clientID string, qty int, now time.Time) (Order, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	old := b.Orders[id]
	if old == nil {
		return Order{}, fmt.Errorf("there is no order %s", id)
	}
	if !old.open() {
		return Order{}, fmt.Errorf("order %s is already %s: there is nothing to move", id, old.Status)
	}

	remaining := old.remaining()
	if qty > 0 {
		if qty < old.FilledQty {
			return Order{}, fmt.Errorf("the new size %d is less than the %d already filled", qty, old.FilledQty)
		}
		remaining = qty - old.FilledQty
	}
	if remaining < 1 {
		return Order{}, fmt.Errorf("the move leaves not one set")
	}

	next := &Order{
		ClientID:    clientID,
		Status:      statusNew,
		Qty:         remaining,
		Limit:       limit,
		TIF:         old.TIF,
		Legs:        old.Legs,
		SubmittedAt: now,
		ExpiresAt:   old.ExpiresAt,
		Replaces:    old.ID,
		ID:          uuid.NewString(),
	}
	if next.ClientID == "" {
		next.ClientID = old.ClientID
	}
	// After the fallback, not before it: a replacement that was given no name of
	// its own inherits the old one, and the turn lives inside that name. Taken
	// too early it would be read off an empty string, and the moved order would
	// lose the turn the original carried.
	next.TurnRef = turnOf(next.ClientID)

	// Collateral is counted afresh: moving the price up is a new obligation, and
	// a broker refuses it when the collateral does not suffice. The old order is
	// dropped from the computation - the same operation releases its
	// collateral.
	projected, cashAfter := b.reserved(next, old.ID)
	need, err := Requirement(projected)
	if err != nil {
		return Order{}, fmt.Errorf("the collateral cannot be counted: %w", err)
	}
	if cashAfter < need-1e-9 {
		return Order{}, fmt.Errorf("not enough collateral for the moved order: %s", shortfall(projected, need, cashAfter))
	}

	old.Status = statusReplaced
	old.CanceledAt = now
	old.ReplacedBy = next.ID
	old.Why = "moved"

	b.seq++
	next.Seq = b.seq
	b.Orders[next.ID] = next
	b.order = append(b.order, next.ID)

	return *next, b.persist()
}

// ExpireDay puts out day orders that outlived their session. This is not tidying
// up: an order left standing since yesterday would fill in the morning at a price
// the agent never asked for.
func (b *Book) ExpireDay(now time.Time) []Order {
	b.mu.Lock()
	defer b.mu.Unlock()

	var gone []Order
	for _, id := range b.order {
		o := b.Orders[id]
		if !o.open() || o.TIF != "day" || o.ExpiresAt.IsZero() || now.Before(o.ExpiresAt) {
			continue
		}
		o.Status = statusExpired
		o.CanceledAt = now
		o.Why = "the session ended, the day order was withdrawn"
		gone = append(gone, *o)
	}
	if len(gone) > 0 {
		b.persist() //nolint:errcheck // the caller logs a write error
	}

	return gone
}

// apply moves the positions and does not touch the cash: the cash is counted by
// the caller from the price of a set rather than from the sum of the legs, and
// adding it twice is not allowed.
func (b *Book) apply(legs []Leg, sets int, legPrice map[string]float64) {
	for _, leg := range legs {
		qty := leg.Ratio * sets
		signed := qty
		if leg.Side == "sell" {
			signed = -qty
		}
		b.move(leg.Symbol, classOption, signed, legPrice[leg.Symbol])
	}
}

// move adds to a position and keeps its average entry price.
func (b *Book) move(symbol, class string, signed int, price float64) {
	if signed == 0 {
		return
	}

	p := b.Positions[symbol]
	if p == nil {
		p = &Position{Symbol: symbol, Class: class}
		b.Positions[symbol] = p
	}
	p.Mark = price

	// The average entry price is weighted by quantity rather than by the number
	// of orders: otherwise a second order for one contract would weigh as much as
	// a first for a hundred, and the P&L would drift silently.
	was := p.Qty
	switch {
	case was == 0:
		p.AvgPrice = price
	case sameSign(was, signed):
		p.AvgPrice = (p.AvgPrice*absf(was) + price*absf(signed)) / absf(was+signed)
	case absf(signed) > absf(was):
		// Flipped through zero: what was there is closed out entirely, and the
		// entry price now belongs to the new side.
		p.AvgPrice = price
	}

	p.Qty += signed
	if p.Qty == 0 {
		delete(b.Positions, symbol)
	}
}

// project is what the book becomes if an order fills. A copy: the real book must
// not be touched until it is decided that the order goes through.
func (b *Book) project(legs []Leg, sets int) []Position {
	after := make(map[string]Position, len(b.Positions)+len(legs))
	for s, p := range b.Positions {
		after[s] = *p
	}
	for _, leg := range legs {
		qty := leg.Ratio * sets
		if leg.Side == "sell" {
			qty = -qty
		}
		p := after[leg.Symbol]
		p.Symbol, p.Class = leg.Symbol, classOption
		p.Qty += qty
		after[leg.Symbol] = p
	}

	out := make([]Position, 0, len(after))
	for _, p := range after {
		if p.Qty == 0 {
			continue
		}
		out = append(out, p)
	}

	return out
}

// reserved is the book and the cash on the assumption that every standing order
// fills at its own limit, plus the proposed extra. skip names an order to leave
// out: when a price is moved, the old order goes and takes its collateral with
// it.
//
// This is deliberately stricter than the truth: some standing orders will never
// fill. But an instrument that errs towards caution does not let a participant
// draw profit, and one that errs the other way does.
func (b *Book) reserved(extra *Order, skip string) (positions []Position, cash float64) {
	after := make(map[string]Position, len(b.Positions)+4)
	for s, p := range b.Positions {
		after[s] = *p
	}
	cash = b.Cash

	add := func(o *Order, sets int) {
		if sets < 1 {
			return
		}
		for _, leg := range o.Legs {
			qty := leg.Ratio * sets
			if leg.Side == "sell" {
				qty = -qty
			}
			p := after[leg.Symbol]
			p.Symbol, p.Class = leg.Symbol, classOption
			p.Qty += qty
			after[leg.Symbol] = p
		}
		cash -= o.Limit*float64(sets)*multiplier + float64(contractsIn(o.Legs, sets))*perContract
	}

	for _, id := range b.order {
		o := b.Orders[id]
		if !o.open() || id == skip {
			continue
		}
		add(o, o.remaining())
	}
	if extra != nil {
		add(extra, extra.Qty)
	}

	out := make([]Position, 0, len(after))
	for _, p := range after {
		if p.Qty == 0 {
			continue
		}
		out = append(out, p)
	}

	return out, cash
}

func contractsIn(legs []Leg, sets int) int {
	n := 0
	for _, leg := range legs {
		n += leg.Ratio * sets
	}

	return n
}

// SetMarks remembers the last prices seen. Collateral needs them: a short
// position in shares demands collateral, and collateral cannot be counted
// without a price.
func (b *Book) SetMarks(marks map[string]float64) {
	b.mu.Lock()
	defer b.mu.Unlock()

	changed := false
	for symbol, price := range marks {
		if p := b.Positions[symbol]; p != nil && price > 0 && p.Mark != price {
			p.Mark = price
			changed = true
		}
	}
	if changed {
		b.persist() //nolint:errcheck // the price is rewritten next tick
	}
}

// Snapshot hands out a copy so a reader does not hold the book's lock.
func (b *Book) Snapshot() (cash, start, lastEquity float64, positions []Position) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, p := range b.Positions {
		positions = append(positions, *p)
	}

	return b.Cash, b.Start, b.LastEquity, positions
}

// AccruedFees is how much the book has already paid in fees.
//
// The account is obliged to say this itself. On 29 August the arena's second
// participant read accrued_fees, saw a zero and wrote in its report that "the
// arena charges no commission" - though 0.05 had been taken from it for that
// very trade. The fees were visible on the order and were not visible on the
// account, and positions are sized from the account.
func (b *Book) AccruedFees() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()

	total := 0.0
	for _, o := range b.Orders {
		total += o.Fees
	}

	return total
}

// OrdersSnapshot hands out the orders in the order they were submitted, oldest
// first.
func (b *Book) OrdersSnapshot() []Order {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]Order, 0, len(b.order))
	for _, id := range b.order {
		out = append(out, *b.Orders[id])
	}

	return out
}

// ByID hands out an order by its identifier.
func (b *Book) ByID(id string) (Order, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	o := b.Orders[id]
	if o == nil {
		return Order{}, false
	}

	return *o, true
}

// ByClientID looks an order up by the name the agent gave it. Idempotency stands
// on this: a retry after a timeout is obliged to find its own earlier order
// rather than create a second one.
func (b *Book) ByClientID(clientID string) (Order, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if clientID == "" {
		return Order{}, false
	}
	for _, id := range b.order {
		if o := b.Orders[id]; o.ClientID == clientID {
			return *o, true
		}
	}

	return Order{}, false
}

func (b *Book) OpenOrders() []Order {
	out := make([]Order, 0, 4)
	for _, o := range b.OrdersSnapshot() {
		if o.open() {
			out = append(out, o)
		}
	}

	return out
}

// MarkClose writes the equity at the day's close. The agent counts today's
// result from it, and rewriting it on every tick after the close means showing a
// flat day.
func (b *Book) MarkClose(day string, equity float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.ClosedOn == day {
		return false
	}
	b.ClosedOn = day
	b.LastEquity = equity
	b.persist() //nolint:errcheck // the caller logs it

	return true
}

// persist is called under the book's lock: memory and file are required to agree
// at every moment the lock is not held.
func (b *Book) persist() error {
	if b.store == nil {
		return nil
	}

	return b.store.Save(b)
}

// sameSign says whether we are adding to a position or reducing it. Reducing
// does not move the average entry price: the entry price belongs to what has
// already been bought.
func sameSign(a, b int) bool {
	return (a > 0 && b > 0) || (a < 0 && b < 0)
}

func absf(n int) float64 {
	if n < 0 {
		return float64(-n)
	}

	return float64(n)
}

// short is a short name for the log. It cuts by RUNES rather than by bytes: a
// participant's token is under no obligation to be Latin, and cutting UTF-8 in
// the middle of a character means writing broken bytes into the log.
func short(s string) string {
	runes := []rune(s)
	if len(runes) <= 6 {
		return string(runes)
	}

	return string(runes[:6])
}

// parseCount parses a positive integer STRICTLY. No sign, no point, no space, no
// zero: each of those is a different number from this one, and substituting a
// one for them means filling an order other than the one that was sent. The
// neighbouring qty refuses the same input honestly, and the disagreement between
// the two has already cost one silent error in the size of a leg.
func parseCount(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("%q: digits only, no sign and no point", s)
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("%q: %w", s, err)
	}
	if n < 1 {
		return 0, fmt.Errorf("%q: must be greater than zero", s)
	}

	return n, nil
}

// shortfall is a collateral refusal in words that show what to do.
//
// The two cases differ in substance and must not be confused. An infinite
// requirement is about the SHAPE of the book: an uncovered short cannot be
// backed by any amount of money, and "put more in" is the wrong advice there. A
// finite one is about SIZE: the same order in fewer sets will go through.
func shortfall(projected []Position, need, cashAfter float64) string {
	if math.IsInf(need, 1) {
		if why := Unbounded(projected); why != "" {
			return why
		}

		// The requirement is infinite and nothing was named as the cause - that
		// should not happen. We say so plainly: the agent needs a limit, and we
		// need a sign that the explanation has fallen behind the arithmetic.
		return "the book's loss has no ceiling and the arena could not name what causes it - that is a defect of the arena, please report it"
	}

	return fmt.Sprintf("%.2f is required, the cash after the fill is %.2f, %.2f short - "+
		"the same structure in fewer sets will go through",
		need, cashAfter, need-cashAfter)
}

// turnOf takes the turn reference out of the name the agent gave the order.
//
// The name is fields separated by semicolons: `worst=-0.11;turn=tu-7;QQQ703`. We
// read exactly the turn= field and not "something that looks like a turn":
// parsing by shape is a guess, and guessing is the thing we spent the day
// agreeing not to do. No field means empty, and the judge honestly sees an
// unstitched order rather than one stitched at random.
func turnOf(clientID string) string {
	for _, field := range strings.Split(clientID, ";") {
		key, ref, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		// Whitespace around the separator is tolerated - that is not a guess but
		// a normalisation. The field's name is matched exactly: turnip= is not
		// the turn= field.
		if strings.TrimSpace(key) == "turn" {
			return strings.TrimSpace(ref)
		}
	}

	return ""
}
