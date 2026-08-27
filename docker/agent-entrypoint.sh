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
    # The model, reached through the policy gateway. The session keeps using its
    # OWN subscription login: the gateway forwards the Authorization header
    # upstream unchanged and stores nothing. `requires_openai_auth` is what makes
    # the CLI send that login to an address that is not OpenAI's.
    if [ -n "${MODEL_GATEWAY_URL}" ]; then
        echo ""
        echo "model_provider = \"swiftward\""
        echo ""
        echo "[model_providers.swiftward]"
        echo "name = \"Swiftward\""
        echo "base_url = \"${MODEL_GATEWAY_URL}\""
        echo "wire_api = \"responses\""
        echo "requires_openai_auth = true"
        # Who is calling, in the gateway's OWN header: `Authorization` already
        # carries the subscription login and is forwarded upstream untouched.
        # The value is the same credential this agent carries to the broker -
        # one machine, one key - so the gateway counts the spend against an
        # identity it verified rather than a label in the URL.
        # Two credentials, two slots: the agent's own key says WHO is calling,
        # the person's key says who it acts FOR. The user key is optional - an
        # agent nobody stands behind simply sends the first.
        # `X-Swiftward-Authorization` is the GATEWAY's own slot and is fixed by
        # it; the user header is ours, so it comes from the same value the
        # endpoint declaration uses rather than being spelled again here.
        if [ -n "${BROKER_MCP_TOKEN}" ] && [ -n "${USER_TOKEN}" ] && [ -n "${USER_HEADER_NAME}" ]; then
            echo "env_http_headers = { \"X-Swiftward-Authorization\" = \"BROKER_MCP_TOKEN\", \"${USER_HEADER_NAME}\" = \"USER_TOKEN\" }"
        elif [ -n "${BROKER_MCP_TOKEN}" ]; then
            echo "env_http_headers = { \"X-Swiftward-Authorization\" = \"BROKER_MCP_TOKEN\" }"
        fi
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
    # The broker's tools. This is either the broker's own server, which asks for
    # nothing, or a policy gateway in front of it, which asks who is calling -
    # the session cannot tell the two apart and needs no change when they swap.
    # A token is written only when there is one, because an empty credential is
    # a claim to have one.
    if [ -n "${BROKER_MCP_URL}" ]; then
        echo ""
        echo "[mcp_servers.broker]"
        echo "url = \"${BROKER_MCP_URL}\""
        if [ -n "${BROKER_MCP_TOKEN}" ]; then
            echo "bearer_token_env_var = \"BROKER_MCP_TOKEN\""
        fi
        # The person this agent acts for. `bearer_token_env_var` above is the
        # machine; this is who it works on behalf of, and the gateway records
        # both. Where a gateway DECLARES a user source, sending it is not
        # optional: a call arriving without one resolves no grants and is offered
        # no tools at all.
        # The literal form, not `env_http_headers`. Both are accepted here, and
        # the environment one did not reach the gateway from this block: the
        # session's calls arrived with no user while the harness's, sent by our
        # own client, carried it. Measured 27 August by looking at a tool only a
        # session calls.
        if [ -n "${USER_TOKEN}" ] && [ -n "${USER_HEADER_NAME}" ]; then
            echo "http_headers = { \"${USER_HEADER_NAME}\" = \"${USER_TOKEN}\" }"
        fi
    fi
} > "${CODEX_HOME}/config.toml"

# The session reads its instructions from the directory it works in, and keeps
# its notes beside them. Instructions are replaced on every start - they belong
# to this image; notes are created once and never overwritten - they belong to
# the session.
mkdir -p /work/notes
cp /agent/AGENTS.md /work/AGENTS.md

# Skills are instructions too, and the agent looks for them in `.agents/skills`
# inside the directory it works in. They are NOT laid out here, and the reason is
# that the choice is no longer a copy: which skills an agent gets is written in
# its declaration, this is a shell script, and a shell script that guesses at
# YAML is a worse place for that decision than the process that already reads it.
#
# The app below does it, reading them from SKILLS_DIR and copying the ones the
# declaration names. To edit a skill and have a running session read the new
# text, mount a checkout at SKILLS_DIR - not over /work/.agents/skills, which the
# app rebuilds and will refuse to start on top of.
#
# It also refuses to start on a /work/.agents/skills it did not write, and the
# copy THIS script used to make is one of those. On a work volume that predates
# the change, remove that directory once; the app builds it on the next start.

exec /usr/local/bin/app "$@"
