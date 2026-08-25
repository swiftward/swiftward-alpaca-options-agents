package marketdata

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decode reads an answer captured from the broker into the shape the readers
// work from. Every literal below is a real answer, trimmed - the risk this
// package carries is the shape changing, and only a real answer tests it.
func decode[T any](t *testing.T, raw string) T {
	t.Helper()

	var answer T
	require.NoError(t, json.Unmarshal([]byte(raw), &answer))

	return answer
}

// The shape asserted here is the one the broker actually returned on
// 24 August 2026; a wake-up depends on reading it correctly.
func TestPricesFromTheBrokersAnswer(t *testing.T) {
	answer := decode[tradesAnswer](t, `{
	  "_alpaca_mcp_security": {"trust": "untrusted_tool_output"},
	  "data": {"trades": {
	    "SPY": {"c": [" ", "T"], "i": 52983929263718, "p": 763.65, "s": 40, "x": "V", "z": "B"},
	    "QQQ": {"p": 0.0}
	  }}
	}`)

	prices := answer.prices()

	assert.Equal(t, 763.65, prices["SPY"])
	_, present := prices["QQQ"]
	assert.False(t, present, "a price of zero is not a reading, and a wake-up must not fire on it")
}

func TestAnEmptyAnswerIsNoPrices(t *testing.T) {
	assert.Empty(t, decode[tradesAnswer](t, `{"data": {}}`).prices())
}

func TestContractsFromTheBrokersAnswer(t *testing.T) {
	answer := decode[contractsAnswer](t, `{
	  "data": {"option_contracts": [
	    {"expiration_date": "2026-08-25", "strike_price": "762", "style": "american",
	     "symbol": "SPY260825C00762000", "type": "call", "underlying_symbol": "SPY"},
	    {"expiration_date": "2026-09-01", "strike_price": "763.5", "style": "american",
	     "symbol": "SPY260901P00763500", "type": "put", "underlying_symbol": "SPY"}
	  ]}
	}`)

	contracts, err := answer.contracts()
	require.NoError(t, err)

	require.Len(t, contracts, 2)
	assert.Equal(t, "SPY260825C00762000", contracts[0].Symbol)
	assert.Equal(t, 762.0, contracts[0].Strike)
	assert.Equal(t, "call", contracts[0].Type)
	assert.Equal(t, 2026, contracts[0].Expiration.Year())
	assert.Equal(t, 763.5, contracts[1].Strike)
}

// A contract whose strike cannot be read is refused rather than passed on as
// zero: a strike of zero would be recorded as a price the market never named.
func TestAnUnreadableStrikeIsRefused(t *testing.T) {
	answer := decode[contractsAnswer](t, `{
	  "data": {"option_contracts": [
	    {"expiration_date": "2026-08-25", "strike_price": "", "symbol": "SPY260825C00762000", "type": "call"}
	  ]}
	}`)

	_, err := answer.contracts()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "SPY260825C00762000")
}

func TestQuotesCarryVolatilityAndDeltaWhenTheBrokerComputesThem(t *testing.T) {
	answer := decode[snapshotsAnswer](t, `{
	  "data": {"snapshots": {
	    "SPY260825P00760000": {
	      "greeks": {"delta": -0.2158, "gamma": 0.0645, "theta": -0.6476, "vega": 0.117},
	      "impliedVolatility": 0.1135,
	      "latestQuote": {"ap": 0.57, "as": 131, "bp": 0.55, "bs": 95, "t": "2026-08-24T19:59:59Z"}
	    },
	    "SPXW260825P07650000": {
	      "latestQuote": {"ap": 14.15, "as": 5, "bp": 13.92, "bs": 4, "t": "2026-08-24T19:59:59Z"}
	    }
	  }}
	}`)

	quotes := answer.quotes()

	spy := quotes["SPY260825P00760000"]
	require.NotNil(t, spy.ImpliedVolatility)
	assert.InDelta(t, 0.1135, *spy.ImpliedVolatility, 1e-9)
	require.NotNil(t, spy.Delta)
	assert.InDelta(t, -0.2158, *spy.Delta, 1e-9)
	assert.Equal(t, 0.55, spy.Bid)

	// Index options carry no greeks on this account. Absent must stay absent:
	// zero volatility is a number the market never charged.
	index := quotes["SPXW260825P07650000"]
	assert.Nil(t, index.ImpliedVolatility)
	assert.Nil(t, index.Delta)
	assert.Equal(t, 13.92, index.Bid)
}

func TestTheAccountReadsAsMoney(t *testing.T) {
	answer := decode[accountAnswer](t, `{
	  "data": {"account_number": "PA3KVT8TYI6V", "status": "ACTIVE", "buying_power": "399997.76",
	    "cash": "99999.44", "equity": "99999.44", "last_equity": "100000",
	    "options_buying_power": "99999.44", "position_market_value": "0", "currency": "USD"}
	}`)

	account, err := answer.account()
	require.NoError(t, err)

	assert.Equal(t, "PA3KVT8TYI6V", account.Number)
	assert.InDelta(t, 99999.44, account.Equity, 1e-9)
	assert.InDelta(t, 100000, account.EquityYesterday, 1e-9)
	assert.InDelta(t, 399997.76, account.BuyingPower, 1e-9)
}

func TestAPositionReadsAsHeld(t *testing.T) {
	answer := decode[positionsAnswer](t, `{
	  "data": {"result": [{"asset_class": "crypto", "avg_entry_price": "78815.73",
	    "cost_basis": "39.143281", "current_price": "78831.6", "market_value": "39.151162",
	    "qty": "0.000496643", "qty_available": "0.000496643", "side": "long", "symbol": "BTCUSD",
	    "unrealized_pl": "0.007881", "unrealized_plpc": "0.0002"}]}
	}`)

	positions, err := answer.positions()
	require.NoError(t, err)

	require.Len(t, positions, 1)
	assert.Equal(t, "BTCUSD", positions[0].Symbol)
	assert.InDelta(t, 0.000496643, positions[0].Quantity, 1e-12)
	assert.InDelta(t, 0.007881, positions[0].UnrealizedPL, 1e-9)
	assert.InDelta(t, 0.0002, positions[0].UnrealizedPLFraction, 1e-9)
}

// A spread is one order with legs, because that is how it was sent. Reading it
// as two orders would tell a reader the agent risked half a structure.
func TestASpreadReadsAsOneOrderWithLegs(t *testing.T) {
	answer := decode[ordersAnswer](t, `{
	  "data": {"result": [{
	    "id": "4530b033-9c62-416f-8783-0daadf68b1a5", "order_class": "mleg", "order_type": "limit",
	    "status": "canceled", "qty": "1", "filled_qty": "0", "limit_price": "-0.4",
	    "filled_avg_price": null, "side": "sell", "symbol": "",
	    "submitted_at": "2026-08-24T22:28:48.884147Z", "filled_at": null,
	    "canceled_at": "2026-08-24T22:29:03.155008Z",
	    "legs": [
	      {"id": "ef26e044", "symbol": "SPY260825P00760000", "side": "sell", "qty": "1",
	       "filled_qty": "0", "order_type": "limit", "order_class": "mleg", "status": "canceled",
	       "position_intent": "sell_to_open", "submitted_at": "2026-08-24T22:28:48.884147Z"},
	      {"id": "9a1c7d21", "symbol": "SPY260825P00755000", "side": "buy", "qty": "1",
	       "filled_qty": "0", "order_type": "limit", "order_class": "mleg", "status": "canceled",
	       "position_intent": "buy_to_open", "submitted_at": "2026-08-24T22:28:48.884147Z"}
	    ]}]}
	}`)

	orders, err := answer.orders()
	require.NoError(t, err)

	require.Len(t, orders, 1)
	assert.Equal(t, "mleg", orders[0].Class)
	assert.InDelta(t, -0.4, orders[0].LimitPrice, 1e-9, "a negative limit price is a credit")
	require.NotNil(t, orders[0].CanceledAt)
	require.Len(t, orders[0].Legs, 2)
	assert.Equal(t, "SPY260825P00760000", orders[0].Legs[0].Symbol)
	assert.Equal(t, "sell_to_open", orders[0].Legs[0].PositionIntent)
}

// A price the broker could not send is absent, not zero - but a field it sends
// as an empty string is zero, and reading it must not fail the whole row.
func TestAnUnreadablePriceRefusesTheRow(t *testing.T) {
	answer := decode[positionsAnswer](t, `{
	  "data": {"result": [{"symbol": "BTCUSD", "qty": "not a number"}]}
	}`)

	_, err := answer.positions()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "BTCUSD")
}
