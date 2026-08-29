package screener

import (
	"math"
	"time"
)

// closeAt is when an option stops trading on the day it expires: 16:00 in New
// York, which is 20:00 UTC while the United States keeps summer time.
//
// It matters only on expiry day, and there it decides everything: at two hours
// left a strike half a percent away is far, and at ten minutes left it is the
// same strike and a different bet.
var closeAt = 20 * time.Hour

// Survival is the chance the sold strike is NOT crossed by expiry, read from the
// market's own implied volatility.
//
// The broker computes no delta on the day a contract expires, and delta is what
// the edge measure weighs the credit against. Rather than leave the expiry-day
// book unmeasured - it is the book that pays most on the day - the same quantity
// is taken from the price of volatility, which the broker does give.
//
// It is the lognormal answer with no drift: over hours, drift is far below the
// noise it would sit inside, and pretending to know its sign would be the only
// invented number here. Like delta this is a risk-neutral probability rather than
// a real one, so it understates the same way and for the same reason.
//
// Reports false where there is nothing to compute from: no volatility, no time
// left, or a strike already crossed.
func Survival(price, strike, impliedVolatility float64, left time.Duration) (float64, bool) {
	if price <= 0 || strike <= 0 || impliedVolatility <= 0 || left <= 0 {
		return 0, false
	}

	// Volatility is quoted per year of calendar time, so the clock that measures
	// what is left has to be the same one.
	years := left.Hours() / 24 / 365
	spread := impliedVolatility * math.Sqrt(years)
	if spread <= 0 {
		return 0, false
	}

	// How many of those the strike sits away from the price. The sign is dropped:
	// a call sold above and a put sold below are the same question mirrored.
	distance := math.Abs(math.Log(price/strike)) / spread

	// The chance of ending on the far side of it, which is what the seller loses.
	crossed := 0.5 * math.Erfc(distance/math.Sqrt2)

	return 1 - crossed, true
}

// leftUntil is how long a contract has to live, counted to the close of the day
// it expires.
func leftUntil(expiration, now time.Time) time.Duration {
	expiresAt := expiration.UTC().Truncate(24 * time.Hour).Add(closeAt)

	return expiresAt.Sub(now)
}
