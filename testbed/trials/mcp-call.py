#!/usr/bin/env python3
"""Call one tool on an MCP server and print what came back.

    arena/trials/mcp-call.py <url> <tool> '<json args>'

Written for reading the market while building a measurement, and deliberately
thin: it does no interpreting at all, so what is printed is what the server
said. Run it with the venv that holds the mcp package:

    python3 testbed/trials/mcp-call.py http://127.0.0.1:8000/mcp \
        get_stock_bars '{"symbols":"SPY","timeframe":"1Day","limit":10}'
"""
import asyncio
import json
import os
import sys

from mcp import ClientSession
from mcp.client.streamable_http import streamablehttp_client


async def main() -> None:
    url, tool = sys.argv[1], sys.argv[2]
    args = json.loads(sys.argv[3]) if len(sys.argv) > 3 else {}

    # A header, when the server wants one. The arena tells participants apart by
    # it, so comparing the arena's answer with the broker's needs the same call
    # shape twice - once with, once without.
    headers = {}
    if os.environ.get("MCP_HEADER"):
        name, _, value = os.environ["MCP_HEADER"].partition(":")
        headers[name.strip()] = value.strip()

    async with streamablehttp_client(url, headers=headers or None) as (read, write, _):
        async with ClientSession(read, write) as session:
            await session.initialize()
            if tool == "-list":
                for t in (await session.list_tools()).tools:
                    print(t.name)
                return
            res = await session.call_tool(tool, args)
            # The structured half when there is one - that is what CODE reads -
            # and the text otherwise.
            if res.structuredContent is not None:
                print(json.dumps(res.structuredContent, ensure_ascii=False))
            else:
                for block in res.content:
                    print(getattr(block, "text", block))


asyncio.run(main())
