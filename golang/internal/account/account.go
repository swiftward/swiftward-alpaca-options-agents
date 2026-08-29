// Package account keeps what the account was worth, moment by moment.
//
// The broker answers what it is worth now. A week of trading is judged on the
// line those answers draw, so the line is kept here - and, like the volatility
// history, it is worth what its length is, so it starts on the first day.
package account

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/disciplinedware/swiftward-alpaca-options-agents/internal/marketdata"
)

// Snapshot is the account at a moment.
type Snapshot struct {
	RecordedAt          time.Time `json:"recorded_at"`
	Equity              float64   `json:"equity"`
	EquityYesterday     float64   `json:"equity_yesterday"`
	Cash                float64   `json:"cash"`
	BuyingPower         float64   `json:"buying_power"`
	OptionsBuyingPower  float64   `json:"options_buying_power"`
	PositionMarketValue float64   `json:"position_market_value"`
}

// Broker is the reading half of the broker: the recorder never orders anything.
type Broker interface {
	Account(ctx context.Context) (marketdata.Account, error)
}

// Store keeps the line.
type Store interface {
	Append(ctx context.Context, snapshot Snapshot) error
	Since(ctx context.Context, since time.Time) ([]Snapshot, error)
}

// Recorder writes one snapshot every Every, around the clock. Unlike the
// volatility history it does not wait for the market to open: money moves when
// positions are marked, and a flat line overnight is itself the truth.
type Recorder struct {
	Broker Broker
	Store  Store
	Every  time.Duration
	Now    func() time.Time
	Log    *zap.Logger
}

func (r *Recorder) Run(ctx context.Context) error {
	switch {
	case r.Broker == nil || r.Store == nil:
		return fmt.Errorf("the account recorder needs a broker to read and somewhere to write")
	case r.Every <= 0:
		return fmt.Errorf("the account recorder needs how often to read: set ACCOUNT_EVERY")
	case r.Now == nil:
		return fmt.Errorf("the account recorder has no clock")
	}

	ticker := time.NewTicker(r.Every)
	defer ticker.Stop()

	r.readOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			r.readOnce(ctx)
		}
	}
}

func (r *Recorder) readOnce(ctx context.Context) {
	read, err := r.Broker.Account(ctx)
	if err != nil {
		r.Log.Error("could not read the account", zap.Error(err))
		return
	}

	err = r.Store.Append(ctx, Snapshot{
		RecordedAt:          r.Now(),
		Equity:              read.Equity,
		EquityYesterday:     read.EquityYesterday,
		Cash:                read.Cash,
		BuyingPower:         read.BuyingPower,
		OptionsBuyingPower:  read.OptionsBuyingPower,
		PositionMarketValue: read.PositionMarketValue,
	})
	if err != nil {
		r.Log.Error("could not record the account", zap.Error(err))
	}
}
