# The container the trading session runs in. It holds the agent and nothing else:
# no broker credential, no database, no docker socket. Its only ways out are the
# gateway on the internal network and the egress proxy.
FROM node:22-alpine

ARG CODEX_VERSION=0.149.0
RUN npm install -g @openai/codex@${CODEX_VERSION} \
    && command -v codex >/dev/null

RUN adduser -D -u 1001 agent
USER agent

# Codex keeps its login and its session files here; compose mounts a writable
# volume over it, because the CLI refreshes the token as it runs.
ENV CODEX_HOME=/home/agent/.codex
WORKDIR /work

ENTRYPOINT ["codex"]
