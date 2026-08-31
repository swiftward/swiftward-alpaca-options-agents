# One binary, two shapes. `page` serves the read side and carries nothing else;
# `agent` is the container a trading session runs in, holding the harness and the
# agent it starts as a child process.
#
# Neither holds a broker credential, a database or a docker socket.
# Both of these stages run on the machine doing the building, not on the machine
# the image is for. What they produce does not depend on the architecture: the web
# stage emits JavaScript, and Go cross-compiles from one line of environment. Left
# on the target platform they would run under emulation, which on a developer's
# laptop turns a two-minute build into a twenty-minute one - and a deployment that
# takes twenty minutes is one nobody makes in a hurry.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src
COPY typescript/web/package*.json ./
RUN npm ci
COPY typescript/web/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY golang/go.mod golang/go.sum ./
RUN go mod download
COPY golang/ ./
# CGO is already off, so naming the target is the whole of cross-compiling. The
# defaults are the building machine's own, which is what makes this line necessary
# rather than decorative.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -o /out/app ./apps/app

FROM alpine:3.22 AS page
COPY --from=build /out/app /usr/local/bin/app
COPY --from=web /src/dist /srv/web
# The rules travel here too: the page shows the limits as the agent reads them,
# and takes them from the same file through the same call. Showing a retelling is
# not allowed - one day it differs from what is traded, and the reader is shown a
# story about the system rather than the system. In development a working copy is
# mounted over this, as it is for the envelope.
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

# ripgrep, because the session reaches for it out of habit. Measured 27 August:
# every turn began with `rg --files /work/notes`, was refused, and asked again
# through `find` - an extra round trip to the model on every session. Installing
# the tool is cheaper than breaking thirteen sessions a day of the habit.
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
