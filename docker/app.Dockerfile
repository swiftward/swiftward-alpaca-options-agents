# One binary, two shapes. `page` serves the read side and carries nothing else;
# `agent` is the container a trading session runs in, holding the harness and the
# agent it starts as a child process.
#
# Neither holds a broker credential, a database or a docker socket.
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

FROM alpine:3.22 AS page
COPY --from=build /out/app /usr/local/bin/app
COPY --from=web /src/dist /srv/web
# Правила едут и сюда: страница показывает пределы, каким их читает агент, и
# берёт их из того же файла тем же вызовом. Показывать пересказ нельзя - он
# однажды разойдётся с тем, по чему торгуют, и читателю мы покажем не систему, а
# рассказ о ней. В разработке поверх монтируется рабочая копия, как и у конверта.
COPY policy/envelope.yaml /policy/envelope.yaml
USER 65534
ENTRYPOINT ["/usr/local/bin/app"]

# The stand-in for the policy gateway. It carries the ruleset as shipped, and the
# stack mounts the working copy over it: lowering a ceiling has to be one edit
# that a running session sees, and a limit baked into an image is not that.
FROM alpine:3.22 AS envelope
COPY --from=build /out/app /usr/local/bin/app
COPY policy/envelope.yaml /policy/envelope.yaml
USER 65534
ENTRYPOINT ["/usr/local/bin/app"]

FROM node:22-alpine AS agent

ARG CODEX_VERSION=0.149.0
RUN npm install -g @openai/codex@${CODEX_VERSION} \
    && command -v codex >/dev/null

# ripgrep, потому что сессия зовёт его по привычке. Измерено 27 августа: каждый
# ход начинался с `rg --files /work/notes`, получал отказ и переспрашивал через
# `find` - лишний круг к модели на каждой сессии. Дешевле положить инструмент,
# чем отучать от него тринадцать сессий в день.
RUN apk add --no-cache ripgrep && command -v rg >/dev/null

COPY --from=build /out/app /usr/local/bin/app
COPY docker/agent-entrypoint.sh /usr/local/bin/entrypoint
COPY agent/ /agent/
RUN chmod +x /usr/local/bin/entrypoint

# Where this image carries the skills it can offer a session. Which of them an
# agent actually gets is its declaration's to say, not this file's - two agents
# run from this one image and need not read the same instructions.
ENV SKILLS_DIR=/agent/skills

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
