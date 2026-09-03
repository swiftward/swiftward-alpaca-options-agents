// Package api serves the read side: the JSON the demo page reads and the built
// page itself. It decides nothing and orders nothing; the broker it reads from
// answers only questions about money already made.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/account"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/envelope"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/screener"
)

// Broker is the money side of the page: what the account is worth now, what is
// held, and what was sent to the broker today. Nil means this deployment reads
// no broker, and the routes answer that plainly rather than with zeros.
type Broker interface {
	Account(ctx context.Context) (marketdata.Account, error)
	Positions(ctx context.Context) ([]marketdata.Position, error)
	Orders(ctx context.Context, limit int) ([]marketdata.Order, error)
}

// History is the line the account drew. Nil means no history is kept here.
type History interface {
	Since(ctx context.Context, since time.Time) ([]account.Snapshot, error)
}

// Read is everything the page can ask for.
type Read struct {
	// Record is what the agent did and why.
	Record record.Keeper
	// Broker answers the money questions, live.
	Broker Broker
	// History is the recorded equity line.
	History History
	// OrdersShown bounds the order list the page carries.
	OrdersShown int
	// HistoryDays is how far back the equity line is drawn.
	HistoryDays int
	// EnvelopePath and EnvelopeIdentity are the limits AS THE AGENT READS THEM.
	// The page answers them from the same file and the same call the agent's own
	// question goes through, so what a reader sees is the thing itself and not a
	// retelling that can drift from it.
	//
	// This is worth a route of its own. Limits reaching the agent by discovery
	// rather than by being written into its prompt is the distinctive claim of
	// this project, and a claim is worth more shown than described.
	EnvelopePath     string
	EnvelopeIdentity string
	// Sweep is the screener's last pass: how much was priced and how much of it
	// survived. Small, and it does what no other number does - proves the page is
	// live rather than a screenshot.
	Sweep Sweep
	// WebDir holds the built page. Empty serves the JSON alone, and the log says
	// so, because a page served from nowhere looks like a broken deployment.
	WebDir string
	// Key is what a reader must carry. See key.go for why it exists and how it
	// arrives. Empty is refused where the handler is built: a read side on a port
	// open to the internet, serving the account with no key at all, is the state
	// this exists to end - so it is not a state this can be started in.
	Key string
	// Public serves the page to anyone who reaches it, with no key at all. The
	// platform requires an address a judge opens by hand, and a judge carries no
	// key. It is a separate value rather than an empty Key so that a page can
	// never become open by a setting somebody forgot: absence keeps the key
	// required, and only this says otherwise.
	Public bool
	Log    *zap.Logger
}

// money is what the page shows above everything else.
// Sweep is the screener's last pass, read the same way the session reads it.
type Sweep interface {
	Candidates(ctx context.Context, most int) ([]screener.Candidate, time.Time, error)
}

type sweep struct {
	Candidates []screener.Candidate `json:"candidates"`
	// TakenAt is when the pass behind this list ran. Rows outlive their sweep, so
	// a list an hour old reads exactly like one a minute old unless it says which.
	TakenAt time.Time `json:"taken_at"`
}

type money struct {
	Account   marketdata.Account    `json:"account"`
	Positions []marketdata.Position `json:"positions"`
	Orders    []marketdata.Order    `json:"orders"`
}

// Handler builds the read-side routes.
func (r Read) Handler() (http.Handler, error) {
	key := strings.TrimSpace(r.Key)
	switch {
	case r.Public && key != "":
		return nil, errors.New("PAGE_PUBLIC is set and so is PAGE_KEY: one of them is a mistake, and guessing which would either publish the account or lock out a judge")
	case !r.Public && key == "":
		return nil, errors.New("PAGE_KEY is empty: this serves the account's positions and the agent's own words, and it will not do that to whoever reaches the port. Set PAGE_PUBLIC where that is the intention")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("GET /api/state", func(w http.ResponseWriter, req *http.Request) {
		current, err := r.Record.Read(req.Context())
		if err != nil {
			r.fail(w, "the record is unavailable", err)
			return
		}
		r.answer(w, current)
	})

	mux.HandleFunc("GET /api/money", func(w http.ResponseWriter, req *http.Request) {
		if r.Broker == nil {
			r.missing(w, "no broker is configured for the read side")
			return
		}

		read, err := r.money(req.Context())
		if err != nil {
			r.fail(w, "the broker is unavailable", err)
			return
		}
		r.answer(w, read)
	})

	mux.HandleFunc("GET /api/limits", func(w http.ResponseWriter, req *http.Request) {
		if r.EnvelopePath == "" || r.EnvelopeIdentity == "" {
			r.missing(w, "no envelope is served here")
			return
		}

		set, err := envelope.Load(r.EnvelopePath)
		if err != nil {
			r.fail(w, "the envelope is unreadable", err)
			return
		}
		// The tool an order travels through: its limits are the ones that decide
		// what may be opened, and they are what a reader came to see.
		limits, err := set.For(r.EnvelopeIdentity, "place_option_order")
		if err != nil {
			r.fail(w, "the envelope has nothing for this identity", err)
			return
		}
		r.answer(w, limits)
	})

	mux.HandleFunc("GET /api/sweep", func(w http.ResponseWriter, req *http.Request) {
		if r.Sweep == nil {
			r.missing(w, "no screener runs here")
			return
		}

		found, takenAt, err := r.Sweep.Candidates(req.Context(), r.OrdersShown)
		if err != nil {
			r.fail(w, "the screener's findings are unavailable", err)
			return
		}
		// An empty LIST, not null. A reader handed null cannot tell "no pass has run
		// yet" from "this field is broken", and the page dies on it outright: on
		// 28 August the live /live returned a white screen exactly this way -
		// `candidates: null` and a `.length` taken on it.
		if found == nil {
			found = []screener.Candidate{}
		}
		r.answer(w, sweep{Candidates: found, TakenAt: takenAt})
	})

	mux.HandleFunc("GET /api/equity", func(w http.ResponseWriter, req *http.Request) {
		if r.History == nil {
			r.missing(w, "no account history is kept here")
			return
		}

		line, err := r.History.Since(req.Context(), time.Now().AddDate(0, 0, -r.HistoryDays))
		if err != nil {
			r.fail(w, "the account history is unavailable", err)
			return
		}
		r.answer(w, line)
	})

	if r.WebDir == "" {
		r.Log.Info("no WEB_DIR set: serving JSON only")
		return r.gate(mux), nil
	}
	if _, err := os.Stat(r.WebDir); err != nil {
		return nil, err
	}
	// Data under /api, the page under the root. The split is not technical - the
	// same process serves both halves - it is so that a file name on the page never
	// covers a route with the same name. /healthz stays at the root: it is a
	// liveness check, it is asked from outside, and it is not data.
	mux.Handle("GET /", spa(r.WebDir))
	r.Log.Info("serving the built page", zap.String("web_dir", r.WebDir))

	return r.gate(mux), nil
}

// money asks the broker its three questions. One failure fails the answer: a
// page showing an account with no positions beside it would read as an agent
// holding nothing.
func (r Read) money(ctx context.Context) (money, error) {
	held, err := r.Broker.Account(ctx)
	if err != nil {
		return money{}, err
	}
	positions, err := r.Broker.Positions(ctx)
	if err != nil {
		return money{}, err
	}
	orders, err := r.Broker.Orders(ctx, r.OrdersShown)
	if err != nil {
		return money{}, err
	}

	return money{Account: held, Positions: positions, Orders: orders}, nil
}

func (r Read) answer(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		r.Log.Error("could not write the answer", zap.Error(err))
	}
}

func (r Read) fail(w http.ResponseWriter, says string, err error) {
	r.Log.Error(says, zap.Error(err))
	http.Error(w, says, http.StatusServiceUnavailable)
}

// spa serves the file when it exists and the page when it does not.
//
// Routes live in the browser: /live is not a file, it is a state of the page. A
// file server answers such a path with 404, and a visitor who opened a link or
// refreshed the tab sees an error instead of what they opened. So anything not
// found gets index.html, and the page resolves the route from there.
//
// Data never reaches here: it is under /api, and this handler stands at the root,
// which longer paths do not reach.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		name := filepath.Join(dir, filepath.Clean(req.URL.Path))
		if info, err := os.Stat(name); err == nil && !info.IsDir() {
			files.ServeHTTP(w, req)

			return
		}

		http.ServeFile(w, req, filepath.Join(dir, "index.html"))
	})
}

// missing answers a route this deployment cannot serve. It is a plain refusal
// rather than an empty body: empty would read as an account with no money.
func (r Read) missing(w http.ResponseWriter, says string) {
	http.Error(w, says, http.StatusNotImplemented)
}
