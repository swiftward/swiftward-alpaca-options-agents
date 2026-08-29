"""Black-Scholes: the price, the volatility that produces it, and delta.

Time to expiry is calendar time up to 20:00 UTC on the day of expiry - the same
way `leftUntil` does it in the product (golang/internal/screener/survival.go),
so that these numbers and its numbers measure the same thing.
"""

import math

SQRT2 = math.sqrt(2.0)


def norm_cdf(x: float) -> float:
    return 0.5 * math.erfc(-x / SQRT2)


def norm_pdf(x: float) -> float:
    return math.exp(-0.5 * x * x) / math.sqrt(2.0 * math.pi)


def d1(spot, strike, vol, years, rate, div):
    return (math.log(spot / strike) + (rate - div + 0.5 * vol * vol) * years) / (vol * math.sqrt(years))


def price(kind, spot, strike, vol, years, rate, div):
    if years <= 0 or vol <= 0:
        intrinsic = spot - strike if kind == "call" else strike - spot
        return max(0.0, intrinsic)
    one = d1(spot, strike, vol, years, rate, div)
    two = one - vol * math.sqrt(years)
    disc_s = spot * math.exp(-div * years)
    disc_k = strike * math.exp(-rate * years)
    if kind == "call":
        return disc_s * norm_cdf(one) - disc_k * norm_cdf(two)
    return disc_k * norm_cdf(-two) - disc_s * norm_cdf(-one)


def delta(kind, spot, strike, vol, years, rate, div):
    if years <= 0 or vol <= 0:
        if kind == "call":
            return 1.0 if spot > strike else 0.0
        return -1.0 if spot < strike else 0.0
    one = d1(spot, strike, vol, years, rate, div)
    if kind == "call":
        return math.exp(-div * years) * norm_cdf(one)
    return -math.exp(-div * years) * norm_cdf(-one)


def implied(kind, market, spot, strike, years, rate, div, low=0.005, high=6.0):
    """The volatility at which Black-Scholes returns market.

    Bisection rather than Newton: far from the money the derivative with respect
    to volatility is near zero and Newton runs away. Bisection always converges,
    and sixty steps are enough. None where there is no answer: the price is below
    intrinsic value or above the bound.
    """
    if years <= 0 or market <= 0:
        return None
    lowest = price(kind, spot, strike, low, years, rate, div)
    highest = price(kind, spot, strike, high, years, rate, div)
    if market <= lowest or market >= highest:
        return None
    for _ in range(60):
        mid = 0.5 * (low + high)
        if price(kind, spot, strike, mid, years, rate, div) < market:
            low = mid
        else:
            high = mid
    return 0.5 * (low + high)


def years_left(expiration_date, at_utc):
    """Years until 20:00 UTC on the day of expiry. expiration_date is a date, at_utc a UTC datetime."""
    import datetime as dt
    expires = dt.datetime.combine(expiration_date, dt.time(20, 0), tzinfo=dt.timezone.utc)
    return (expires - at_utc).total_seconds() / (365.0 * 24 * 3600)
