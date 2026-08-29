"""Reading Alpaca directly, for measurement rather than for trading.

Not through the MCP server and not through the gateway, and that is deliberate.
MCP is the AGENT's path: it trims answers to what a session needs and asks one
question at a time. Here the opposite is wanted - pull eighteen months of bars
page by page and put them on disk. Read-only: nothing in this file can send an
order.

Keys come from the repository's own .env, the same file the stack runs on, so
there is no second copy of a secret to keep in step.
"""

import os
import time
from pathlib import Path

import requests

HERE = Path(__file__).resolve().parent
ENV = HERE.parent / ".env"

DATA = "https://data.alpaca.markets"
TRADE = "https://paper-api.alpaca.markets"


def _env(name: str) -> str:
    """The value of one setting, from the environment or from .env beside it."""
    if os.environ.get(name):
        return os.environ[name]
    if ENV.exists():
        for line in ENV.read_text().splitlines():
            if line.startswith(name + "="):
                value = line.split("=", 1)[1].strip()
                if value:
                    return value
    raise SystemExit(f"{name} is empty or missing: set it in the environment or in {ENV}")


def headers() -> dict[str, str]:
    return {
        "APCA-API-KEY-ID": _env("ALPACA_API_KEY_ID"),
        "APCA-API-SECRET-KEY": _env("ALPACA_API_SECRET_KEY"),
    }


def get(url: str, params: dict | None = None, tries: int = 4) -> dict:
    """One request, with retries. The retries are not laziness: name resolution
    on a development machine fails in bursts, and one failure should not bring
    down a year's download."""
    last = None
    for attempt in range(tries):
        try:
            answer = requests.get(url, headers=headers(), params=params, timeout=30)
            if answer.status_code == 429:
                time.sleep(2 * (attempt + 1))
                continue
            answer.raise_for_status()

            return answer.json()
        except requests.RequestException as failure:
            last = failure
            time.sleep(1 + attempt)

    raise SystemExit(f"{url}: {last}")


def pages(url: str, params: dict, key: str, most: int = 200) -> list:
    """Every page of a paged answer. `key` names the field holding the list."""
    out, token, page = [], None, 0
    while page < most:
        ask = dict(params)
        if token:
            ask["page_token"] = token
        answer = get(url, ask)
        chunk = answer.get(key) or []
        out.extend(chunk if isinstance(chunk, list) else [chunk])
        token = answer.get("next_page_token")
        page += 1
        if not token:
            break

    return out
