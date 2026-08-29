package declaration

import (
	"bytes"
	"fmt"
	"os"
	"sync"
)

// Watcher holds the declaration in force and hands back a newer one when the
// file behind it changes.
//
// It exists because the file is edited while the week runs. Until it did, the
// declaration was read once at start, so tightening a window or adding a session
// meant restarting the process - in the middle of a trading day, with orders the
// ladder was still walking. A schedule only a restart can change is a schedule
// nobody edits.
type Watcher struct {
	path string
	// verify is asked about a declaration before it is put in force. It is how
	// something outside this package - the skills an agent is given, and the
	// parameters they need - gets a say in whether the new file is usable. A
	// declaration that fails it is refused and the old one keeps working.
	verify func(*Declaration) error

	mu      sync.RWMutex
	current *Declaration
	raw     []byte
}

// Watch reads the declaration once and keeps the path for later. A file that
// cannot be read or cannot be verified is a failure to start, exactly as it was
// before this existed: an agent whose schedule is unreadable wakes nobody all
// day and looks like one that simply had nothing to do.
func Watch(path string, verify func(*Declaration) error) (*Watcher, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read declaration: %w", err)
	}
	declared, err := parse(raw, path)
	if err != nil {
		return nil, err
	}
	if verify != nil {
		if err := verify(declared); err != nil {
			return nil, fmt.Errorf("declaration %s: %w", path, err)
		}
	}

	return &Watcher{path: path, verify: verify, current: declared, raw: raw}, nil
}

// Current is the declaration in force right now.
func (w *Watcher) Current() *Declaration {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.current
}

// Reread reads the file again and returns the declaration in force after it.
//
// Unchanged bytes give back the very same declaration, pointer and all, so a
// caller can tell "the schedule changed" from "the file was read again" by
// comparing what it got against what it had.
//
// A file that has become unreadable - half-saved, or edited into something that
// does not check out - returns the declaration already in force together with
// the error. The schedule keeps working; whoever is watching the log is told
// that what is on disk is not what is running.
func (w *Watcher) Reread() (*Declaration, error) {
	w.mu.RLock()
	current, raw := w.current, w.raw
	w.mu.RUnlock()

	fresh, err := os.ReadFile(w.path)
	if err != nil {
		return current, fmt.Errorf("read declaration %s: %w", w.path, err)
	}
	if bytes.Equal(fresh, raw) {
		return current, nil
	}

	declared, err := parse(fresh, w.path)
	if err != nil {
		return current, err
	}
	if w.verify != nil {
		if err := w.verify(declared); err != nil {
			return current, fmt.Errorf("declaration %s: %w", w.path, err)
		}
	}

	w.mu.Lock()
	w.current, w.raw = declared, fresh
	w.mu.Unlock()

	return declared, nil
}

// Schedule is what wakes this agent, read from whichever declaration is in force
// when the session asks. The session's own tool holds this rather than a
// declaration, so an answer given after an edit is the edited schedule.
func (w *Watcher) Schedule() []Scheduled { return w.Current().Schedule() }
