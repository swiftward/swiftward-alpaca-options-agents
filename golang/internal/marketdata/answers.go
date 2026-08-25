package marketdata

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// The shapes the broker answers in, and the one place that turns them into
// values this program uses. They are separate from the calls so that a change in
// the broker's shape is caught by a test holding a real answer, not only by a
// live call.

type tradesAnswer struct {
	Data struct {
		Trades map[string]struct {
			Price float64 `json:"p"`
		} `json:"trades"`
	} `json:"data"`
}

// prices drops a symbol the broker gave no price for. Zero is a price, and a
// wake-up must not fire on a missing reading.
func (a tradesAnswer) prices() map[string]float64 {
	prices := make(map[string]float64, len(a.Data.Trades))
	for symbol, trade := range a.Data.Trades {
		if trade.Price > 0 {
			prices[strings.ToUpper(symbol)] = trade.Price
		}
	}

	return prices
}

type clockAnswer struct {
	Data struct {
		IsOpen bool `json:"is_open"`
	} `json:"data"`
}

type contractsAnswer struct {
	Data struct {
		Contracts []struct {
			Symbol     string `json:"symbol"`
			Expiration string `json:"expiration_date"`
			Strike     string `json:"strike_price"`
			Type       string `json:"type"`
		} `json:"option_contracts"`
	} `json:"data"`
}

// contracts refuses an unreadable expiration or strike rather than passing a
// zero along: a contract nobody can price is not a contract.
func (a contractsAnswer) contracts() ([]Contract, error) {
	contracts := make([]Contract, 0, len(a.Data.Contracts))
	for _, listed := range a.Data.Contracts {
		expiration, err := time.Parse(time.DateOnly, listed.Expiration)
		if err != nil {
			return nil, fmt.Errorf("read the expiration of %s: %w", listed.Symbol, err)
		}
		strike, err := strconv.ParseFloat(listed.Strike, 64)
		if err != nil {
			return nil, fmt.Errorf("read the strike of %s: %w", listed.Symbol, err)
		}
		contracts = append(contracts, Contract{
			Symbol:     listed.Symbol,
			Expiration: expiration,
			Strike:     strike,
			Type:       listed.Type,
		})
	}

	return contracts, nil
}

type snapshotsAnswer struct {
	Data struct {
		Snapshots map[string]struct {
			ImpliedVolatility *float64 `json:"impliedVolatility"`
			Greeks            *struct {
				Delta *float64 `json:"delta"`
			} `json:"greeks"`
			LatestQuote struct {
				Bid float64 `json:"bp"`
				Ask float64 `json:"ap"`
			} `json:"latestQuote"`
		} `json:"snapshots"`
	} `json:"data"`
}

func (a snapshotsAnswer) quotes() map[string]Quote {
	quotes := make(map[string]Quote, len(a.Data.Snapshots))
	for symbol, snapshot := range a.Data.Snapshots {
		quote := Quote{
			Symbol:            symbol,
			Bid:               snapshot.LatestQuote.Bid,
			Ask:               snapshot.LatestQuote.Ask,
			ImpliedVolatility: snapshot.ImpliedVolatility,
		}
		if snapshot.Greeks != nil {
			quote.Delta = snapshot.Greeks.Delta
		}
		quotes[symbol] = quote
	}

	return quotes
}
