#!/bin/sh
# Prepares the session's home, then hands over to the harness.
#
# Two things have to happen before the agent can work, and both must happen on
# every start: the login has to be copied out of the read-only mount into a
# writable home (the CLI refreshes its token as it runs), and the servers the
# session may call have to be written from the environment, so changing an
# address in compose is enough.
set -e

CODEX_HOME="${CODEX_HOME:-/home/agent/.codex}"
mkdir -p "${CODEX_HOME}"

if [ -f /mnt/codex/auth.json ]; then
    cp /mnt/codex/auth.json "${CODEX_HOME}/auth.json"
    chmod 600 "${CODEX_HOME}/auth.json"
    echo "[entrypoint] login copied from the host"
else
    echo "[entrypoint] no login at /mnt/codex/auth.json: the agent cannot start a session" >&2
    exit 1
fi

{
    echo "# Written on every start from the environment. Edit compose, not this file."
    if [ -n "${AGENT_REASONING_EFFORT}" ]; then
        # How hard the model thinks before it answers. A trade is worth more
        # thinking than a chat reply, and the setting belongs to the deployment.
        echo "model_reasoning_effort = \"${AGENT_REASONING_EFFORT}\""
    fi
    if [ -n "${SESSION_MCP_URL}" ]; then
        echo ""
        echo "[mcp_servers.session]"
        echo "url = \"${SESSION_MCP_URL}\""
    fi
    if [ -n "${GATEWAY_URL}" ]; then
        echo ""
        echo "[mcp_servers.gateway]"
        echo "url = \"${GATEWAY_URL}\""
        echo "bearer_token_env_var = \"GATEWAY_TOKEN\""
    fi
    # Development only: the broker's own server, reached without the gateway in
    # front of it. The judged account is never wired this way - every order it
    # sees goes through the gateway, which is what declares the limits.
    if [ -n "${BROKER_MCP_URL}" ]; then
        echo ""
        echo "[mcp_servers.broker]"
        echo "url = \"${BROKER_MCP_URL}\""
    fi
} > "${CODEX_HOME}/config.toml"

# The session reads its instructions from the directory it works in, and keeps
# its notes beside them. Instructions are replaced on every start - they belong
# to this image; notes are created once and never overwritten - they belong to
# the session.
mkdir -p /work/notes
cp /agent/AGENTS.md /work/AGENTS.md

exec /usr/local/bin/app "$@"
