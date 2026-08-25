package marketdata

import (
	"strconv"
	"strings"
	"time"
)

// ContractFrom reads what a contract symbol says about itself.
//
// The broker names an option the way the industry does: the underlying, then the
// expiration as YYMMDD, then C or P, then the strike in thousandths, eight
// digits. QQQ260827P00706000 is a QQQ put at 706 expiring on 27 August 2026.
//
// Nothing is guessed. A symbol that does not parse is refused, because the two
// places this is read - what a fill is called in the room and what the record
// keeps - are both worse with a wrong strike than with none.
func ContractFrom(symbol string) (Contract, bool) {
	symbol = strings.TrimSpace(symbol)
	// Eight digits of strike, one letter, six of date: fifteen after the root, and
	// a root is at least one character.
	if len(symbol) < 16 {
		return Contract{}, false
	}

	tail := symbol[len(symbol)-15:]
	underlying := symbol[:len(symbol)-15]
	if underlying == "" {
		return Contract{}, false
	}

	expiration, err := time.Parse("060102", tail[:6])
	if err != nil {
		return Contract{}, false
	}

	var kind string
	switch tail[6] {
	case 'C', 'c':
		kind = "call"
	case 'P', 'p':
		kind = "put"
	default:
		return Contract{}, false
	}

	thousandths, err := strconv.ParseInt(tail[7:], 10, 64)
	if err != nil || thousandths <= 0 {
		return Contract{}, false
	}

	return Contract{
		Symbol:     symbol,
		Expiration: expiration,
		Strike:     float64(thousandths) / 1000,
		Type:       kind,
	}, true
}
