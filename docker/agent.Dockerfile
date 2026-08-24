# The container a trading session runs in. It holds the agent and the harness
# that starts it - the harness spawns the agent as a child process, reads its
# stream and posts it, so the two live together.
#
# It holds no broker credential, no database and no docker socket. Its only ways
# out are the services beside it and the egress proxy.
FROM node:22-alpine AS web
WORKDIR /src
COPY typescript/web/package*.json ./
RUN npm ci
COPY typescript/web/ ./
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY golang/go.mod golang/go.sum ./
RUN go mod download
COPY golang/ ./
RUN CGO_ENABLED=0 go build -o /out/app ./apps/app

FROM node:22-alpine

ARG CODEX_VERSION=0.149.0
RUN npm install -g @openai/codex@${CODEX_VERSION} \
    && command -v codex >/dev/null

COPY --from=build /out/app /usr/local/bin/app
COPY --from=web /src/dist /srv/web
COPY docker/agent-entrypoint.sh /usr/local/bin/entrypoint
COPY agent/ /agent/
COPY playbooks/ /playbooks/
RUN chmod +x /usr/local/bin/entrypoint

# The session runs as the same user id that owns the login on the host: that file
# is readable by its owner alone, and a mismatch here shows up as a session that
# cannot start. AGENT_UID is a build argument for hosts where it is not 1000.
ARG AGENT_UID=1000
ENV HOME=/home/agent
ENV CODEX_HOME=/home/agent/.codex
RUN mkdir -p ${CODEX_HOME} /work \
    && chown -R ${AGENT_UID}:${AGENT_UID} ${HOME} /work
USER ${AGENT_UID}

WORKDIR /work

ENTRYPOINT ["/usr/local/bin/entrypoint"]
