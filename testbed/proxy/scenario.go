// A staged market: the prices a scenario says, instead of the prices the market
// happens to have.
//
// This exists for the questions the real market will not answer on request.
// "The price reached the sold strike - does the defence fire or stay quiet?"
// "Equity fell three percent - does the safety catch refuse the next opening?"
// "The contract expired and one leg is left - what then?" Each of those is
// answered YES or NO, with no profit in it at all, and each is the class of bug
// that costs a day. Waiting for the market to stage them is not a plan.
//
// What is substituted and what is NOT. Only the three reads that carry a PRICE:
// the clock, the underlying's last trade, and the option snapshot. Everything
// else - the chain, the contracts, the news - goes upstream to the real broker
// unchanged, because a scenario has no business inventing which contracts exist.
// The instrument stays what it was: reads from the real Alpaca, writes to the
// arena's own book, and no order ever leaves for the broker.
//
// A fill under a scenario is marked in the book exactly as a bench fill is, and
// for the same reason: those numbers are not a measurement, and a week later
// nobody will remember which run was which.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Scenario is a staged market, read from a file.
type Scenario struct {
	// Name is what the run is called, and it travels into the book beside every
	// order the scenario fills, so a reader afterwards knows which staging
	// produced the number.
	Name string `json:"name"`
	// Mode is how the scenario meets the market. Empty is the staged market:
	// prices are invented and only what the scenario names has one. "overlay" is
	// the alternative reality: every read goes to the real broker and the
	// underlying is moved along the steps' curve, with each contract repriced
	// from that move. See overlay.go.
	Mode string `json:"mode"`
	// Underlying is the symbol whose price the steps move.
	Underlying string `json:"underlying"`
	// Start is the scenario's own wall clock at step zero. It may be any date:
	// the point of a staged market is that it need not be today.
	Start time.Time `json:"start"`
	// Anchor set to "now" makes the staged clock read WALL TIME: the session is
	// staged open, and nothing else about the clock is moved.
	//
	// It exists because a staged date breaks an invariant production holds - that
	// the broker's timestamps and the harness's own clock are the same clock.
	// Anything measuring an AGE against a broker timestamp then sees the offset
	// instead of the age. Measured 31 August: the ladder read two orders stamped
	// 14:37 by a staged clock while it was 22:12 outside, called them seven and a
	// half hours old, and cancelled both on patience within a second of their
	// being placed. Nothing was wrong with the ladder.
	//
	// So: a scenario that stages a TIME OF DAY (an open, a close, an expiry that
	// day) names a start. A scenario that stages a BOOK and does not care what
	// hour it is anchors to now, and every clock in the stand agrees again.
	Anchor string `json:"anchor"`
	// Open says whether the session is open for the whole run. A scenario that
	// needs the session to close mid-run says so on the step instead.
	Open bool `json:"open"`
	// NextClose is what the clock answers for the end of the session. Left out,
	// it is the start plus six and a half hours.
	NextClose time.Time `json:"next_close"`
	// Speed compresses time: 60 means one real second carries a scenario minute.
	// Without it a five-minute step takes five real minutes, and a whole scenario
	// outlives the turn watching it.
	Speed float64 `json:"speed"`
	// Steps are cumulative from Start and must be given in order.
	Steps []Step `json:"steps"`
	// Faults make named tools refuse for a stretch of the run. A market that
	// misbehaves is only half of what breaks an agent: the other half is a tool
	// that stops answering while the market keeps moving. Measured on the team's
	// own stand on 31 August - a grant check refused every price read for
	// eighteen minutes, nothing crashed, and two entry windows passed unnoticed.
	Faults []Fault `json:"faults"`
}

// Fault is one stretch of the run in which named tools refuse.
//
// The refusal is a plain tool error carrying the message the scenario gives, so
// it reads to the agent exactly like a real one: a limit, a policy, a feed it is
// not entitled to. What is being asked is not whether the agent can parse it but
// whether it NOTICES that it is no longer reading, and says so, rather than
// reporting calm from data it never received.
type Fault struct {
	// After and Until are offsets from Start, as "5m" or "90s". Until is
	// exclusive.
	After string `json:"after"`
	Until string `json:"until"`
	// Tools are the names that refuse. A name the arena does not serve is
	// refused at load: a fault on a tool nobody calls is a fault that never
	// fires, and it would sit in the file looking like a test.
	Tools []string `json:"tools"`
	// Message is what the agent is told. It has to sound like the thing it
	// imitates, or the trial measures how odd our wording is.
	Message string `json:"message"`

	from, until time.Duration
}

// Step is what the staged market looks like from one moment until the next step.
type Step struct {
	// After is the offset from Start, as "5m" or "90s".
	After string `json:"after"`
	// Price is what the underlying trades at from here on. Staged mode only.
	Price float64 `json:"underlying_price"`
	// Delta is how far the underlying stands from the price the real market has
	// at this moment. Overlay mode only, and a POINTER because zero is a
	// statement - "here the two realities are the same" - and "no displacement
	// given" is not that statement.
	Delta *float64 `json:"underlying_delta"`
	// Open, when given, overrides the scenario's own session state from here on.
	Open *bool `json:"open,omitempty"`
	// Quotes are the option contracts this step names. A contract absent from a
	// step keeps whatever the previous step said: a scenario states what CHANGES,
	// and repeating an unchanged book on every step is how a step gets edited in
	// one place and forgotten in the other.
	Quotes map[string]StagedQuote `json:"quotes"`

	offset time.Duration
}

// StagedQuote is one contract's book and greeks at a step.
type StagedQuote struct {
	Bid     float64 `json:"bid"`
	Ask     float64 `json:"ask"`
	BidSize int     `json:"bid_size"`
	AskSize int     `json:"ask_size"`
	Delta   float64 `json:"delta"`
	IV      float64 `json:"iv"`
}

// LoadScenario reads a scenario and checks it before anything runs on it.
//
// Every check here is a scenario that would otherwise stage the wrong thing
// quietly: steps out of order replay in the wrong sequence, a missing start date
// puts the run at year zero, and a contract with no ask can never be bought, so
// the case the author meant to test never happens and the run still goes green.
func LoadScenario(path string) (*Scenario, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read the scenario %s: %w", path, err)
	}

	var s Scenario
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&s); err != nil {
		return nil, fmt.Errorf("the scenario %s: %w", path, err)
	}

	if strings.TrimSpace(s.Name) == "" {
		return nil, fmt.Errorf("the scenario %s has no name: the name travels into the book beside every order it fills", path)
	}
	if strings.TrimSpace(s.Underlying) == "" {
		return nil, fmt.Errorf("the scenario %q names no underlying", s.Name)
	}
	overlay := s.Mode == "overlay"
	switch s.Mode {
	case "", "overlay":
	default:
		return nil, fmt.Errorf("the scenario %q has mode=%q: the modes are \"\" (a staged market) and \"overlay\"", s.Name, s.Mode)
	}
	if overlay {
		// An overlay does not stage a clock, and saying otherwise in the file is
		// the beginning of a run in which half the answers come from one world.
		switch {
		case s.Anchor != "":
			return nil, fmt.Errorf("the scenario %q is an overlay and sets anchor=%q: an overlay reads the REAL clock, there is nothing to anchor", s.Name, s.Anchor)
		case !s.Start.IsZero():
			return nil, fmt.Errorf("the scenario %q is an overlay and sets a start: an overlay happens now, in the real session", s.Name)
		case !s.NextClose.IsZero():
			return nil, fmt.Errorf("the scenario %q is an overlay and sets next_close: the session it runs in is the real one", s.Name)
		case s.Open:
			return nil, fmt.Errorf("the scenario %q is an overlay and sets open: whether the market is open is the market's answer, not ours", s.Name)
		}
	}
	switch s.Anchor {
	case "":
		if overlay {
			break
		}
		if s.Start.IsZero() {
			return nil, fmt.Errorf("the scenario %q has no start: a staged market still happens at a time, "+
				"or set anchor to \"now\" to stage the book and leave the clock alone", s.Name)
		}
	case "now":
		if !s.Start.IsZero() {
			return nil, fmt.Errorf("the scenario %q sets both anchor=now and a start: "+
				"one of the two is a lie about which clock the stand is on", s.Name)
		}
		if s.Speed != 0 && s.Speed != 1 {
			return nil, fmt.Errorf("the scenario %q anchors to now at speed %g: "+
				"anchoring means the staged clock IS the wall clock, and a speed moves them apart again", s.Name, s.Speed)
		}
	default:
		return nil, fmt.Errorf("the scenario %q has anchor=%q: the only anchor is \"now\"", s.Name, s.Anchor)
	}
	if len(s.Steps) == 0 {
		return nil, fmt.Errorf("the scenario %q has no steps: there is nothing to stage", s.Name)
	}
	if s.Speed <= 0 {
		s.Speed = 1
	}
	if s.NextClose.IsZero() && s.Anchor == "" && !overlay {
		s.NextClose = s.Start.Add(390 * time.Minute)
	}

	last := time.Duration(-1)
	for i := range s.Steps {
		step := &s.Steps[i]
		if step.After == "" {
			step.After = "0s"
		}
		d, err := time.ParseDuration(step.After)
		if err != nil {
			return nil, fmt.Errorf("the scenario %q, step %d: after=%q is not a duration", s.Name, i, step.After)
		}
		if d < 0 {
			return nil, fmt.Errorf("the scenario %q, step %d: after=%q runs backwards", s.Name, i, step.After)
		}
		if d <= last && i > 0 {
			return nil, fmt.Errorf("the scenario %q, step %d: after=%q is not later than the step before it - steps are given in order", s.Name, i, step.After)
		}
		last = d
		step.offset = d
		switch {
		case overlay && step.Delta == nil:
			return nil, fmt.Errorf("the scenario %q, step %d: an overlay moves the price BY something - give underlying_delta, in dollars, and 0 for \"the same as the real market here\"", s.Name, i)
		case overlay && step.Price != 0:
			return nil, fmt.Errorf("the scenario %q, step %d: an overlay names no underlying_price - the price is the market's, and the scenario only displaces it", s.Name, i)
		case overlay && len(step.Quotes) > 0:
			return nil, fmt.Errorf("the scenario %q, step %d: an overlay quotes no contracts of its own - every contract is repriced from the real one", s.Name, i)
		case !overlay && step.Delta != nil:
			return nil, fmt.Errorf("the scenario %q, step %d: underlying_delta belongs to an overlay; a staged market states the price outright", s.Name, i)
		case !overlay && step.Price <= 0:
			return nil, fmt.Errorf("the scenario %q, step %d: underlying_price is %v", s.Name, i, step.Price)
		}
		for symbol, q := range step.Quotes {
			if _, err := parseOCC(symbol); err != nil {
				return nil, fmt.Errorf("the scenario %q, step %d: %w", s.Name, i, err)
			}
			if q.Bid < 0 || q.Ask < 0 {
				return nil, fmt.Errorf("the scenario %q, step %d, %s: a negative price", s.Name, i, symbol)
			}
		}
	}

	for i := range s.Faults {
		f := &s.Faults[i]
		if f.from, err = time.ParseDuration(orZero(f.After)); err != nil {
			return nil, fmt.Errorf("the scenario %q, fault %d: after=%q is not a duration", s.Name, i, f.After)
		}
		if f.until, err = time.ParseDuration(orZero(f.Until)); err != nil {
			return nil, fmt.Errorf("the scenario %q, fault %d: until=%q is not a duration", s.Name, i, f.Until)
		}
		if f.until <= f.from {
			return nil, fmt.Errorf("the scenario %q, fault %d: until=%q is not later than after=%q, so it never fires",
				s.Name, i, f.Until, f.After)
		}
		if strings.TrimSpace(f.Message) == "" {
			return nil, fmt.Errorf("the scenario %q, fault %d: no message - the agent would be refused without being told why", s.Name, i)
		}
		if len(f.Tools) == 0 {
			return nil, fmt.Errorf("the scenario %q, fault %d: names no tools", s.Name, i)
		}
		for _, name := range f.Tools {
			if !servable[name] {
				return nil, fmt.Errorf("the scenario %q, fault %d: the arena does not serve %q, so this fault would never fire",
					s.Name, i, name)
			}
		}
	}

	return &s, nil
}

func orZero(d string) string {
	if strings.TrimSpace(d) == "" {
		return "0s"
	}

	return d
}

// refuses says whether a tool is faulted at this moment, and with what words.
func (s *stage) refuses(name string) (string, bool) {
	if s == nil {
		return "", false
	}
	s.mu.Lock()
	sc, began := s.scenario, s.began
	s.mu.Unlock()
	if sc == nil {
		return "", false
	}

	elapsed := time.Duration(float64(time.Since(began)) * sc.Speed)
	for i := range sc.Faults {
		f := &sc.Faults[i]
		if elapsed < f.from || elapsed >= f.until {
			continue
		}
		for _, tool := range f.Tools {
			if tool == name {
				return f.Message, true
			}
		}
	}

	return "", false
}

// staged is the market as the scenario has it at one moment.
type staged struct {
	Now    time.Time
	Open   bool
	Price  float64
	Quotes map[string]Quote
}

// at resolves the scenario at an elapsed real time.
//
// Quotes accumulate: a contract named in an early step and not since keeps that
// book. That is what lets a scenario say only what moves.
func (s *Scenario) at(elapsed time.Duration) staged {
	if elapsed < 0 {
		elapsed = 0
	}
	scenarioElapsed := time.Duration(float64(elapsed) * s.Speed)

	out := staged{
		Now:    s.Start.Add(scenarioElapsed),
		Open:   s.Open,
		Quotes: map[string]Quote{},
	}
	for i := range s.Steps {
		step := &s.Steps[i]
		if step.offset > scenarioElapsed {
			break
		}
		out.Price = step.Price
		if step.Open != nil {
			out.Open = *step.Open
		}
		for symbol, q := range step.Quotes {
			out.Quotes[symbol] = Quote{
				Symbol: symbol, Bid: q.Bid, Ask: q.Ask,
				BidSize: q.BidSize, AskSize: q.AskSize,
				// The staged quote is stamped with the staged clock, not with the
				// machine's: the freshness check must see a fresh quote, or every
				// scenario dated anywhere but today is refused as stale.
				At: out.Now,
			}
		}
	}

	return out
}

// greeksOf answers the delta and the volatility a step named for a contract, for
// the snapshot the agent reads. Absent, they are left out of the answer rather
// than sent as zero: a delta of zero is a statement, and "we did not stage one"
// is not that statement.
func (s *Scenario) greeksOf(elapsed time.Duration, symbol string) (delta, iv float64, ok bool) {
	scenarioElapsed := time.Duration(float64(elapsed) * s.Speed)
	for i := range s.Steps {
		step := &s.Steps[i]
		if step.offset > scenarioElapsed {
			break
		}
		if q, named := step.Quotes[symbol]; named {
			delta, iv, ok = q.Delta, q.IV, true
		}
	}

	return delta, iv, ok
}

// stage holds a running scenario and the moment it began.
type stage struct {
	mu       sync.Mutex
	scenario *Scenario
	began    time.Time
}

// staging says a scenario is on AND it invents the market outright. Every place
// that answers a read from the scenario asks this and not on(): under an overlay
// the same read goes upstream and is corrected afterwards.
func (s *stage) staging() bool {
	return s.on() && s.mode() != "overlay"
}

// overlaying says the run is an alternative reality: real reads, one number moved.
func (s *stage) overlaying() bool {
	return s.on() && s.mode() == "overlay"
}

func (s *stage) mode() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scenario == nil {
		return ""
	}

	return s.scenario.Mode
}

// shiftNow is how far the overlaid underlying stands from the real one, at this
// moment of the run.
func (s *stage) shiftNow() float64 {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	sc, began := s.scenario, s.began
	s.mu.Unlock()
	if sc == nil {
		return 0
	}

	return sc.shiftAt(time.Since(began))
}

func (s *stage) on() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.scenario != nil
}

// newStage puts a loaded scenario on the clock. An anchored scenario learns its
// start HERE and not at load: the start is the moment the proxy came up, and
// only this side of the program knows it.
func newStage(sc *Scenario, began time.Time) *stage {
	if sc != nil && sc.Anchor == "now" {
		sc.Start = began.UTC()
		if sc.NextClose.IsZero() {
			sc.NextClose = sc.Start.Add(390 * time.Minute)
		}
	}

	return &stage{scenario: sc, began: began}
}

func (s *stage) now() staged {
	s.mu.Lock()
	sc, began := s.scenario, s.began
	s.mu.Unlock()

	return sc.at(time.Since(began))
}

func (s *stage) name() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scenario == nil {
		return ""
	}

	return s.scenario.Name
}

// snapshotJSON builds the answer to get_option_snapshot in the broker's own
// shape, taken from a live answer on 29 August rather than invented: an agent
// reads greeks and impliedVolatility beside latestQuote, and a shape of our own
// would break the code it reads them with.
func (s *stage) snapshotJSON(symbols []string) map[string]any {
	s.mu.Lock()
	sc, began := s.scenario, s.began
	s.mu.Unlock()

	elapsed := time.Since(began)
	now := sc.at(elapsed)

	snapshots := map[string]any{}
	for _, symbol := range symbols {
		q, ok := now.Quotes[symbol]
		if !ok {
			continue
		}
		row := map[string]any{
			"latestQuote": map[string]any{
				"ap": q.Ask, "as": q.AskSize, "ax": "N",
				"bp": q.Bid, "bs": q.BidSize, "bx": "N",
				"c": "A", "t": q.At.UTC().Format(time.RFC3339Nano),
			},
		}
		if delta, iv, named := sc.greeksOf(elapsed, symbol); named {
			row["greeks"] = map[string]any{"delta": delta}
			row["impliedVolatility"] = iv
		}
		snapshots[symbol] = row
	}

	return map[string]any{"snapshots": snapshots, "next_page_token": nil}
}

func (s *stage) tradeJSON(symbols []string) map[string]any {
	now := s.now()
	trades := map[string]any{}
	for _, symbol := range symbols {
		if !strings.EqualFold(symbol, s.underlying()) {
			continue
		}
		trades[strings.ToUpper(symbol)] = map[string]any{
			"p": now.Price, "s": 1, "t": now.Now.UTC().Format(time.RFC3339Nano), "x": "V", "c": []string{"@"},
		}
	}

	return map[string]any{"trades": trades}
}

func (s *stage) clockJSON() map[string]any {
	s.mu.Lock()
	sc := s.scenario
	s.mu.Unlock()
	now := s.now()

	return map[string]any{
		"is_open":    now.Open,
		"timestamp":  now.Now.UTC().Format(time.RFC3339Nano),
		"next_open":  sc.Start.UTC().Format(time.RFC3339Nano),
		"next_close": sc.NextClose.UTC().Format(time.RFC3339Nano),
	}
}

// closeAt is when the staged session ends. A boundary, so it does not move.
func (s *stage) closeAt() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scenario == nil {
		return time.Time{}
	}

	return s.scenario.NextClose
}

func (s *stage) underlying() string {
	// Nil is the ordinary case - no scenario at all - and this is asked on every
	// read of a last trade, overlay or not.
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.scenario == nil {
		return ""
	}

	return s.scenario.Underlying
}

// symbolsOf splits the comma-separated argument the broker takes.
func symbolsOf(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)

	return out
}
