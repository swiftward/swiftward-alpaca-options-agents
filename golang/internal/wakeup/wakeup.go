// Package wakeup keeps the wake-ups a session asks for: "wake me at 15:45", or
// "wake me when SPY trades under 760".
//
// They live on disk, so a restart does not forget them. A session that asked to
// be woken and never was would be worse than one that never asked: it would have
// planned around a promise nobody kept.
package wakeup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind is what the harness watches for.
type Kind string

const (
	// KindAt fires once, at a time.
	KindAt Kind = "at"
	// KindPrice fires once, when a symbol trades through a level.
	KindPrice Kind = "price"
)

// Direction is which side of the level fires.
type Direction string

const (
	Above Direction = "above"
	Below Direction = "below"
)

// Wakeup is one standing request from the session.
type Wakeup struct {
	ID string `json:"id"`
	// Cause is why the session wants to be woken. The harness repeats it back, so
	// the woken session knows what it meant.
	Cause     Cause     `json:"cause"`
	Kind      Kind      `json:"kind"`
	At        time.Time `json:"at,omitempty"`
	Symbol    string    `json:"symbol,omitempty"`
	Direction Direction `json:"direction,omitempty"`
	Level     float64   `json:"level,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// MarshalJSON leaves out a time that was never set.
//
// `omitempty` cannot do it: a struct is never "empty" to encoding/json, so a
// price wake-up - which has no time at all, it waits for a number - was carried
// as `"at":"0001-01-01T00:00:00Z"`, both to the session and into the file. A
// reader sorting standing wake-ups by that field puts every price one in the
// first year of our era, and one reading it as "when this fires" is simply told
// something false. Found 28 August by a session that set a price wake-up and
// read its own list back.
func (w Wakeup) MarshalJSON() ([]byte, error) {
	// plain has no method set of its own, so marshalling it does not recur.
	type plain Wakeup
	if !w.At.IsZero() {
		return json.Marshal(plain(w))
	}

	// The shallower field wins in encoding/json, and an empty string with
	// omitempty is left out - which is what "no time here" should look like.
	return json.Marshal(struct {
		plain
		At string `json:"at,omitempty"`
	}{plain: plain(w)})
}

// Cause is the sentence the session wrote. It is a named type so that nothing
// can pass an empty string by accident.
type Cause string

// Store holds the wake-ups and writes them through to a file on every change.
type Store struct {
	path string

	mu   sync.Mutex
	list []Wakeup
	next int
}

// Open reads the wake-ups kept at path. A missing file is an empty store, not an
// error: the first run has nothing to remember.
func Open(path string) (*Store, error) {
	s := &Store{path: path}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read wake-ups: %w", err)
	}
	if err := json.Unmarshal(raw, &s.list); err != nil {
		return nil, fmt.Errorf("read wake-ups from %s: %w", path, err)
	}
	for _, w := range s.list {
		if n := idNumber(w.ID); n > s.next {
			s.next = n
		}
	}

	return s, nil
}

// AddAt records a wake-up at a time.
func (s *Store) AddAt(cause Cause, at time.Time, now time.Time) (Wakeup, error) {
	if strings.TrimSpace(string(cause)) == "" {
		return Wakeup{}, fmt.Errorf("a wake-up needs a cause: the session it wakes has to know what it meant")
	}
	if !at.After(now) {
		return Wakeup{}, fmt.Errorf("%s is not in the future", at.Format(time.RFC3339))
	}

	return s.add(Wakeup{Cause: cause, Kind: KindAt, At: at, CreatedAt: now})
}

// AddPrice records a wake-up on a price crossing.
func (s *Store) AddPrice(cause Cause, symbol string, direction Direction, level float64, now time.Time) (Wakeup, error) {
	if strings.TrimSpace(string(cause)) == "" {
		return Wakeup{}, fmt.Errorf("a wake-up needs a cause: the session it wakes has to know what it meant")
	}
	if strings.TrimSpace(symbol) == "" {
		return Wakeup{}, fmt.Errorf("a price wake-up needs a symbol")
	}
	if direction != Above && direction != Below {
		return Wakeup{}, fmt.Errorf("direction %q is neither above nor below", direction)
	}
	if level <= 0 {
		return Wakeup{}, fmt.Errorf("level %v is not a price", level)
	}

	return s.add(Wakeup{Cause: cause, Kind: KindPrice, Symbol: strings.ToUpper(symbol), Direction: direction, Level: level, CreatedAt: now})
}

func (s *Store) add(w Wakeup) (Wakeup, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.next++
	w.ID = fmt.Sprintf("w%d", s.next)
	s.list = append(s.list, w)

	if err := s.write(); err != nil {
		s.list = s.list[:len(s.list)-1]
		return Wakeup{}, err
	}

	return w, nil
}

// List returns what is still standing, oldest first.
func (s *Store) List() []Wakeup {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := append([]Wakeup(nil), s.list...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })

	return out
}

// Cancel removes one. Removing something that is not there is an error, because
// a session that thinks it cancelled a wake-up will plan as if it did.
func (s *Store) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, w := range s.list {
		if w.ID == id {
			s.list = append(s.list[:i], s.list[i+1:]...)
			return s.write()
		}
	}

	return fmt.Errorf("no wake-up %s", id)
}

// Due returns the wake-ups that have come true, and removes them: a wake-up
// fires once. price is the last trade of each symbol being watched; a symbol
// absent from it is simply not checked this round.
func (s *Store) Due(now time.Time, price map[string]float64) []Wakeup {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []Wakeup
	kept := s.list[:0]
	for _, w := range s.list {
		if w.fires(now, price) {
			due = append(due, w)
			continue
		}
		kept = append(kept, w)
	}
	s.list = kept

	if len(due) > 0 {
		if err := s.write(); err != nil {
			// The wake-ups already fired; losing the file would repeat them on the
			// next start, which is worse than reporting it here.
			return due
		}
	}

	return due
}

// Watching lists the symbols any standing wake-up needs a price for.
func (s *Store) Watching() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := map[string]bool{}
	var symbols []string
	for _, w := range s.list {
		if w.Kind == KindPrice && !seen[w.Symbol] {
			seen[w.Symbol] = true
			symbols = append(symbols, w.Symbol)
		}
	}
	sort.Strings(symbols)

	return symbols
}

func (w Wakeup) fires(now time.Time, price map[string]float64) bool {
	switch w.Kind {
	case KindAt:
		return !now.Before(w.At)
	case KindPrice:
		last, ok := price[w.Symbol]
		if !ok {
			return false
		}
		if w.Direction == Above {
			return last >= w.Level
		}
		return last <= w.Level
	}

	return false
}

// Prompt is what the woken session is told.
func (w Wakeup) Prompt() string {
	switch w.Kind {
	case KindPrice:
		return fmt.Sprintf("Woken by a condition you set: %s trades %s %.2f.\n%s", w.Symbol, w.Direction, w.Level, w.Cause)
	default:
		return fmt.Sprintf("Woken by the time you set: %s.\n%s", w.At.Format(time.RFC3339), w.Cause)
	}
}

func (s *Store) write() error {
	raw, err := json.MarshalIndent(s.list, "", "  ")
	if err != nil {
		return fmt.Errorf("write wake-ups: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("write wake-ups: %w", err)
	}

	// Written beside the file and renamed, so a crash mid-write leaves the old
	// list rather than half of the new one.
	temporary := s.path + ".writing"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return fmt.Errorf("write wake-ups: %w", err)
	}

	return os.Rename(temporary, s.path)
}

func idNumber(id string) int {
	var n int
	if _, err := fmt.Sscanf(id, "w%d", &n); err != nil {
		return 0
	}

	return n
}
