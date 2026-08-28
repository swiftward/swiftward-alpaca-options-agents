package marketdata

import (
	"fmt"
	"sort"
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
				// How much the book is showing at each side. It does not cap a
				// fill - 50 contracts went through against 25 shown on 25 August -
				// but a side showing nothing at all is a price nobody is standing
				// behind.
				BidSize int `json:"bs"`
				AskSize int `json:"as"`
			} `json:"latestQuote"`
		} `json:"snapshots"`
		// NextPage is set when the strikes asked for did not fit in one answer.
		NextPage string `json:"next_page_token"`
	} `json:"data"`
}

func (a snapshotsAnswer) quotes() map[string]Quote {
	quotes := make(map[string]Quote, len(a.Data.Snapshots))
	for symbol, snapshot := range a.Data.Snapshots {
		quote := Quote{
			Symbol:            symbol,
			Bid:               snapshot.LatestQuote.Bid,
			Ask:               snapshot.LatestQuote.Ask,
			BidSize:           snapshot.LatestQuote.BidSize,
			AskSize:           snapshot.LatestQuote.AskSize,
			ImpliedVolatility: snapshot.ImpliedVolatility,
		}
		if snapshot.Greeks != nil {
			quote.Delta = snapshot.Greeks.Delta
		}
		quotes[symbol] = quote
	}

	return quotes
}

type accountAnswer struct {
	Data struct {
		Number              string `json:"account_number"`
		Status              string `json:"status"`
		Equity              string `json:"equity"`
		LastEquity          string `json:"last_equity"`
		Cash                string `json:"cash"`
		BuyingPower         string `json:"buying_power"`
		OptionsBuyingPower  string `json:"options_buying_power"`
		PositionMarketValue string `json:"position_market_value"`
		OptionsTradingLevel int    `json:"options_trading_level"`
	} `json:"data"`
}

func (a accountAnswer) account() (Account, error) {
	read := numbers{of: "the account"}
	account := Account{
		Number:              a.Data.Number,
		Status:              a.Data.Status,
		OptionsTradingLevel: a.Data.OptionsTradingLevel,
		Equity:              read.field("equity", a.Data.Equity),
		EquityYesterday:     read.field("last_equity", a.Data.LastEquity),
		Cash:                read.field("cash", a.Data.Cash),
		BuyingPower:         read.field("buying_power", a.Data.BuyingPower),
		OptionsBuyingPower:  read.field("options_buying_power", a.Data.OptionsBuyingPower),
		PositionMarketValue: read.field("position_market_value", a.Data.PositionMarketValue),
	}
	if read.err != nil {
		return Account{}, read.err
	}

	return account, nil
}

type positionsAnswer struct {
	Data struct {
		Positions []struct {
			Symbol              string `json:"symbol"`
			AssetClass          string `json:"asset_class"`
			Side                string `json:"side"`
			Quantity            string `json:"qty"`
			AverageEntryPrice   string `json:"avg_entry_price"`
			CurrentPrice        string `json:"current_price"`
			MarketValue         string `json:"market_value"`
			CostBasis           string `json:"cost_basis"`
			UnrealizedPL        string `json:"unrealized_pl"`
			UnrealizedPLPercent string `json:"unrealized_plpc"`
		} `json:"result"`
	} `json:"data"`
}

func (a positionsAnswer) positions() ([]Position, error) {
	positions := make([]Position, 0, len(a.Data.Positions))
	for _, held := range a.Data.Positions {
		read := numbers{of: held.Symbol}
		position := Position{
			Symbol:               held.Symbol,
			AssetClass:           held.AssetClass,
			Side:                 held.Side,
			Quantity:             read.field("qty", held.Quantity),
			AverageEntryPrice:    read.field("avg_entry_price", held.AverageEntryPrice),
			CurrentPrice:         read.field("current_price", held.CurrentPrice),
			MarketValue:          read.field("market_value", held.MarketValue),
			CostBasis:            read.field("cost_basis", held.CostBasis),
			UnrealizedPL:         read.field("unrealized_pl", held.UnrealizedPL),
			UnrealizedPLFraction: read.field("unrealized_plpc", held.UnrealizedPLPercent),
		}
		if read.err != nil {
			return nil, read.err
		}
		positions = append(positions, position)
	}

	return positions, nil
}

// numbers reads the numbers the broker sends as strings and remembers the first
// field it could not read, so a row is refused whole rather than half-read with
// a zero standing in for a price.
type numbers struct {
	of  string
	err error
}

func (n *numbers) field(name, raw string) float64 {
	if raw == "" {
		// The broker writes "0" where it means zero and omits what does not apply.
		return 0
	}

	value, err := strconv.ParseFloat(raw, 64)
	if err != nil && n.err == nil {
		n.err = fmt.Errorf("read %s of %s: %w", name, n.of, err)
	}

	return value
}

type ordersAnswer struct {
	Data struct {
		Orders []brokerOrder `json:"result"`
	} `json:"data"`
}

type brokerOrder struct {
	ID             string        `json:"id"`
	ClientID       string        `json:"client_order_id"`
	Symbol         string        `json:"symbol"`
	Side           string        `json:"side"`
	Type           string        `json:"order_type"`
	Class          string        `json:"order_class"`
	Status         string        `json:"status"`
	Quantity       string        `json:"qty"`
	Ratio          string        `json:"ratio_qty"`
	Notional       string        `json:"notional"`
	FilledQuantity string        `json:"filled_qty"`
	LimitPrice     string        `json:"limit_price"`
	FilledPrice    string        `json:"filled_avg_price"`
	PositionIntent string        `json:"position_intent"`
	SubmittedAt    *time.Time    `json:"submitted_at"`
	FilledAt       *time.Time    `json:"filled_at"`
	CanceledAt     *time.Time    `json:"canceled_at"`
	Legs           []brokerOrder `json:"legs"`
}

func (a ordersAnswer) orders() ([]Order, error) {
	orders := make([]Order, 0, len(a.Data.Orders))
	for _, placed := range a.Data.Orders {
		order, err := placed.order()
		if err != nil {
			return nil, err
		}
		orders = append(orders, order)
	}

	return orders, nil
}

func (o brokerOrder) order() (Order, error) {
	read := numbers{of: "order " + o.ID}
	order := Order{
		ID:             o.ID,
		ClientID:       o.ClientID,
		Symbol:         o.Symbol,
		Side:           o.Side,
		Type:           o.Type,
		Class:          o.Class,
		Status:         o.Status,
		PositionIntent: o.PositionIntent,
		Quantity:       read.field("qty", o.Quantity),
		Ratio:          read.field("ratio_qty", o.Ratio),
		Notional:       read.field("notional", o.Notional),
		FilledQuantity: read.field("filled_qty", o.FilledQuantity),
		LimitPrice:     read.field("limit_price", o.LimitPrice),
		FilledPrice:    read.field("filled_avg_price", o.FilledPrice),
		SubmittedAt:    o.SubmittedAt,
		FilledAt:       o.FilledAt,
		CanceledAt:     o.CanceledAt,
	}
	if read.err != nil {
		return Order{}, read.err
	}

	for _, leg := range o.Legs {
		read, err := leg.order()
		if err != nil {
			return Order{}, err
		}
		order.Legs = append(order.Legs, read)
	}

	return order, nil
}

// chainAnswer is a whole underlying's options priced in one call. The snapshots
// are shaped exactly as get_option_snapshot's are, so the quote is read by the
// same code; what the chain adds is that the contract itself arrives with it,
// named by its symbol.
type chainAnswer struct {
	snapshotsAnswer
}

// chain reads the contracts out of the symbols the answer is keyed by, and the
// quotes out of the snapshots under them.
//
// The symbol is the source for strike, expiration and type here, because the
// chain gives no separate contract record. A symbol that does not parse is
// skipped rather than guessed: the two places this is read - what a fill is
// called and what the record keeps - are both worse with a wrong strike.
func (a chainAnswer) chain() ([]Contract, map[string]Quote, error) {
	// A page token means the strikes asked for did not fit, so what came back is
	// part of the book and nothing in it says which part. Refusing is the only
	// honest answer: a truncated chain reads exactly like a complete one, and the
	// screener would report "nothing here" for a name whose best structure simply
	// did not make the page.
	if a.Data.NextPage != "" {
		return nil, nil, fmt.Errorf("the chain did not fit in one answer: ask for fewer strikes or fewer expirations")
	}

	quotes := a.quotes()
	contracts := make([]Contract, 0, len(quotes))
	for symbol := range quotes {
		contract, parsed := ContractFrom(symbol)
		if !parsed {
			continue
		}
		contracts = append(contracts, contract)
	}
	sort.Slice(contracts, func(i, j int) bool {
		if !contracts[i].Expiration.Equal(contracts[j].Expiration) {
			return contracts[i].Expiration.Before(contracts[j].Expiration)
		}

		return contracts[i].Strike < contracts[j].Strike
	})

	return contracts, quotes, nil
}

// barsAnswer is the broker's daily history for one or more symbols. Only the
// close is read: the replay this feeds asks where the price ENDED a window, and
// a high that was touched for a second is not where anything settled.
type barsAnswer struct {
	Data struct {
		Bars map[string][]struct {
			Close float64 `json:"c"`
			At    string  `json:"t"`
		} `json:"bars"`
	} `json:"data"`
}

// closes returns the closes for symbol in the order the broker sent them, which
// the caller asks to be ascending. A bar with no close is dropped rather than
// read as zero: a zero in a price series turns into a -100% return and poisons
// every window that touches it.
func (a barsAnswer) closes(symbol string) []float64 {
	bars := a.Data.Bars[strings.ToUpper(symbol)]
	out := make([]float64, 0, len(bars))
	for _, bar := range bars {
		if bar.Close > 0 {
			out = append(out, bar.Close)
		}
	}

	return out
}
