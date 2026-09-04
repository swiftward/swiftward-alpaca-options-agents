# Deploying the stack on a server

The stack lives permanently: it holds the clock, wakes a session on schedule,
walks an order to the book and writes down everything that happened. So it needs
an ordinary machine with Docker, not an environment where a process starts on a
request and dies after the answer.

## What you need

- **A machine** with Docker and Docker Compose, **x86_64**: the published images
  are built for it. Two cores and 4 GB of memory are enough - the stack at rest
  takes about 240 MiB and grows while a session is thinking.
- **A domain** pointing at the machine's address. The DNS record must propagate
  **before** the first start: the certificate is obtained through a challenge on
  port 80, and without a working name it is simply not issued.
- **The broker's keys** and a login for the agent - both below.

Closer to the exchange is better. A pass goes through hundreds of underlyings and
is bounded by requests per minute, and the defence decides on price every thirty
minutes: an extra hundred milliseconds per request stretches a pass enough that
the list goes stale before it is refreshed.

## Install

```
git clone https://github.com/swiftward/swiftward-alpaca-options-agents.git
cd swiftward-alpaca-options-agents
cp .env.example .env
```

`.env` is then filled in on the machine. What is required:

| What | Why |
|---|---|
| `AGENT_1_ALPACA_KEY_ID`, `AGENT_1_ALPACA_SECRET_KEY` | the account being traded |
| `DASHBOARD_HOST`, `ACME_EMAIL` | the page's domain and the email for the certificate |
| `TELEGRAM_*` | the room where the session speaks and listens |
| `AGENT_MODEL` | which model the session thinks with |
| `IMAGE_TAG` | which images to pull, see below |
| `SCREENER_UNDERLYINGS` and the other `SCREENER_*` | what the screener looks through; **an empty list switches off both it and the session's candidates tool, silently** |

Everything else has sensible values in `.env.example` and is changed as needed.

## Log the agent in

The agent does not start without a login: it does not guess who it works as. The
login is done on the machine itself.

```
ssh -L 1455:localhost:1455 you@server
codex login
```

The port forward is needed because the login returns to a browser and the server
has none: the command prints an address you open at home. If a browser will not
do at all, the same `codex login` accepts a key or a token on standard input.
Check with `codex login status`.

The login refreshes itself as it goes, so the directory holding it lives in a
volume and survives a restart.

## Log in to the image registry

The images are private packages, and that is a choice rather than an oversight.
Public packages would spare the server a secret, but the agent's image contains
the `agent/` directory - the declaration, the playbooks, `AGENTS.md`, that is,
the whole trading logic in plain text. The repository opens on submission day;
there is no reason to hand the logic a week earlier to people who are still
trading. (The organisation does not permit public packages either: that is its
policy, and only the owner can lift it.)

So the server needs a READ-ONLY token. A classic one, with the single scope
`read:packages`:

```
https://github.com/settings/tokens/new?scopes=read:packages&description=alpaca-stand-ghcr
```

It is put in place once, appearing neither in command history nor in a chat:

```
read -rs "T?token: "
printf '%s' "$T" | ssh -i ~/.ssh/alpaca-swiftward root@<address> \
  "docker login ghcr.io -u <your-login> --password-stdin"
unset T
```

Two small things in that line are not accidental. The prompt is given as
`"T?token: "` rather than through `-p`: in zsh `-p` means "read from the
coprocess" and gives `no coprocess`. And the token goes through standard INPUT
rather than in the command's arguments - otherwise it is visible in the process
list on both machines while the command runs.

After that `pull` works as it does with public packages. The secret lives in
`~/.docker/config.json` on the server and nowhere else - it must NOT go into
`.env`, or it will travel into backups and other people's eyes.

## What must not be empty in .env

A copy of `.env.example` will not do: some values there are deliberately empty,
and a service with an empty value does not run at half strength - it **does not
start at all**. Verified by bringing the stack up on 28 August: both failures
were found exactly this way.

| variable | without it | the failure |
|---|---|---|
| `ENVELOPE_CALLERS` | the envelope | `the envelope role would recognise nobody` |
| `THREAD_RESUME_LIMIT` | the harness | a thread can only be remembered with a limit on resuming |
| `AGENT_1_GATEWAY_TOKEN` | the envelope and the agent | the session cannot prove who it is |
| `ENVELOPE_IDENTITY` | the ladder | it has nothing to enforce with: it reads that identity's limit |
| `RECORD_DATABASES` | the migrations | nowhere to apply them |
| `AGENT_1_ALPACA_KEY_ID` and the secret | the broker's server | restarts in a loop |
| `AGENT_1_BROKER_MCP_URL` | the agent | there is no broker at all |
| `AGENT_1_PAGE_MCP_TOKEN` | the page | it shows no money and answers 503 |
| `SCREENER_KEEP` | the harness | `the screener needs how long to keep what it finds` |
| `CODEX_AUTH_DIR` and the login | the agent | `no login at /mnt/codex/auth.json` |
| `PAGE_KEY`, unless `PAGE_PUBLIC` is set | the page | `PAGE_KEY is empty: this serves the account's positions and the agent's own words` |

### The page: a key, or deliberately public

The page serves the account - its positions, its equity, every order and the
agent's own words - so who may read it is a decision the deployment makes and not
a default. Two values express it and exactly one of them is set.

`PAGE_KEY` is a long random string; a reader arrives once at `?key=<it>`, the key
becomes a cookie and leaves the address. This is what a page reachable by anyone
who finds the port needs.

`PAGE_PUBLIC=true` serves the page to everyone, with `PAGE_KEY` left empty. The
platform requires an address a judge opens by hand and a judge carries no key, so
the public address is set this way and no other. Both together refuse to start:
they say two different things about who may read the account, and there is no
reading of them safe to guess.

The codex login is done once, on a machine with no browser, and only like this:

```
docker run --rm -it --entrypoint codex \
  -v /opt/alpaca-stand/codex-auth:/home/agent/.codex \
  ghcr.io/swiftward/swiftward-alpaca-options-agents/agent:<version> login --device-auth
```

Three details, each of which cost an attempt. `--device-auth`, because the
ordinary login waits for a callback on `localhost` and does not survive a port
forward. The directory belongs to `1000:1000` - the image runs as that user, and
from root you get `Permission denied`. And it sits beside the stack rather than
under `/root`: the container has no route there at all.

`AGENT_1_GATEWAY_TOKEN` and `ENVELOPE_CALLERS` are OUR own strings, not the broker's and
not another gateway's. One key per agent on purpose: limits are applied to
whoever asked, and two agents sharing a key would be one agent to the rules.

```
T=$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9' | head -c 32)
printf 'AGENT_1_GATEWAY_TOKEN=%s\nENVELOPE_CALLERS=%s=alpaca-agent-1\n' "$T" "$T" >> .env
```

Be careful writing into `.env` over ssh: `\$T` inside a heredoc lands in the file
LITERALLY, and compose then looks for a variable `T` that does not exist. We
walked into this on 28 August.

## Bring it up

```
mkdir -p /opt/alpaca-stand/transcripts
make prod-up
```

The directory is created in advance on purpose. Leave it out and Docker makes it,
owned by root, and the agent, which does not run as root, cannot write there. No
trace of the work appears and there is no error either: the agent simply writes
nothing, silently.

The stack pulls the published images by `IMAGE_TAG` and comes up. One container
faces outwards - the proxy; it also obtains the certificate. The rest are
reachable only from inside.

**Pin the images to a commit, not to `latest`.** Every build is published twice:
under `latest` and under the commit's tag. A deployment standing on a commit tag
answers the question "what exactly is running now", and a rollback comes down to
changing one line.

## If the browser complains about the certificate

Traefik requests a certificate ONCE at start and, having been refused, does not
retry soon. So the order matters: the DNS record first, then the start. If the
domain was set up afterwards, restart the proxy, or it will keep serving its own
self-signed `TRAEFIK DEFAULT CERT` - the site works, the browser just does not
trust it.

```
docker compose --env-file .env -f compose.prod.yaml restart traefik
```

Check it in the log rather than by the look of the page:

```
docker compose -f compose.prod.yaml logs traefik | grep -viE 'GET |POST ' | grep -iE 'certificate|obtain|error'
```

A refusal looks like this and names its reason plainly: `NXDOMAIN looking up A
for <domain> - check that a DNS record exists`. Success is two lines in a row:
`Validations succeeded; requesting certificates` and `Server responded with a
certificate`.

What this is NOT: an empty `ACME_EMAIL` inside the container. The email goes into
the start command rather than the environment, so `echo $ACME_EMAIL` there is
always empty - look in `docker inspect`, under `Config.Cmd`.

## Where to see what the agent did

Two records, and they complement each other rather than duplicate.

`transcripts/` on the machine is the full trace of every thread as codex writes
it: what was fed to the session, which tools were called and with what answer,
what the agent said in reply, and its reasoning. The format is JSONL, one line
per event, laid out by date.

Postgres holds the same thing broken into tables: `turns` (when, what woke it,
how it ended), `tool_calls` (the tool, the arguments, the answer), `intents`
(what was decided and why), `candidates` (what the screener found on its last
pass), `account_snapshots` and `volatility_samples`. The texts of the task and
the answer are NOT here - they are in `transcripts/`.

## Make sure it came up

Seeing that a container is running is not enough: a process can stall at start
and look alive. Look for **two** lines in the agent's log:

```
conversation ready
started
```

The first says the conversation with the agent is open, the second that the
schedule is running. A line about the service listening on a port means neither.

Then the living sign: account snapshots appear in the database, and at the
appointed time a turn appears in the record.

## Updating

```
git pull
docker compose --env-file .env -f compose.prod.yaml pull
make prod-up
```

**Not during trading hours.** A restart cuts a running turn, and a turn is a
decision already taken: a session can record an intent and not get as far as
sending the order. Updating the deployment is a person's decision and is done
with the market closed.

## If the agent refused to start over the playbook directory

It says so plainly and names the reason. The playbook directory in the work
volume belongs to the process that laid it out, and it will not delete what is
not its own. This happens after updating from a version where the entrypoint
placed the playbooks. The command is in `agent/README.md`, and so is why it is a
one-off.
