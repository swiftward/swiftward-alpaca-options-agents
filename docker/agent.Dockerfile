# The container a trading session runs in. It holds the agent and the harness
# that starts it - the harness spawns the agent as a child process, reads its
# stream and posts it, so the two live together.
#
# It holds no broker credential, no database and no docker socket. Its only ways
# out are the services beside it and the egress proxy.
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
COPY agent/ /agent/
COPY playbooks/ /playbooks/

RUN adduser -D -u 1001 agent && mkdir -p /work && chown agent:agent /work
USER agent

# Codex keeps its login and its session files here; compose mounts a writable
# volume over it, because the CLI refreshes the token as it runs.
ENV CODEX_HOME=/home/agent/.codex
WORKDIR /work

ENTRYPOINT ["/usr/local/bin/app"]
