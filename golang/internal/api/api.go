// Package api serves the read side: the JSON the demo page reads and the built
// page itself. It decides nothing and orders nothing; the broker it reads from
// answers only questions about money already made.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/account"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/record"
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
	// WebDir holds the built page. Empty serves the JSON alone, and the log says
	// so, because a page served from nowhere looks like a broken deployment.
	WebDir string
	Log    *zap.Logger
}

// money is what the page shows above everything else.
type money struct {
	Account   marketdata.Account    `json:"account"`
	Positions []marketdata.Position `json:"positions"`
	Orders    []marketdata.Order    `json:"orders"`
}

// Handler builds the read-side routes.
func (r Read) Handler() (http.Handler, error) {
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
		return mux, nil
	}
	if _, err := os.Stat(r.WebDir); err != nil {
		return nil, err
	}
	// Данные под /api, страница под корнем. Разделение не техническое - обе
	// половины отдаёт этот же процесс, - а про то, чтобы имя файла на странице
	// однажды не закрыло собой маршрут с тем же именем. /healthz остаётся на
	// корне: это проверка живости, её спрашивают снаружи, и она не данные.
	mux.Handle("GET /", http.FileServer(http.Dir(r.WebDir)))
	r.Log.Info("serving the built page", zap.String("web_dir", r.WebDir))

	return mux, nil
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

// missing answers a route this deployment cannot serve. It is a plain refusal
// rather than an empty body: empty would read as an account with no money.
func (r Read) missing(w http.ResponseWriter, says string) {
	http.Error(w, says, http.StatusNotImplemented)
}
