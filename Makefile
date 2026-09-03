# Root Makefile. Every target here delegates to golang/, typescript/ or docker compose.

.DEFAULT_GOAL := help
SHELL := /bin/bash

.PHONY: help local-up local-down prod-up prod-down build test test-db test-broker rehearse lint english migrate fmt account-claims

help: ## List the targets
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

# --profile session is on both `up` targets and has to be. The agent carries
# profiles: [session], so a compose command without it brings up everything EXCEPT
# the thing that trades - and that looks exactly like success. It cost us a
# rebuild on 30 August: the stack came up, the agents kept running the previous
# binary, and the only symptom was that they were writing to a column the
# migration had just dropped. Never run a bare `docker compose up` here.
local-up: ## Start the whole stack from this checkout, agents included
	docker compose --env-file .env --profile session up -d --build

local-down: ## Stop the local stack, keeping the volumes
	docker compose --env-file .env --profile session down

prod-up: ## Start the whole stack from the published images, agents included
	docker compose --env-file .env -f compose.prod.yaml --profile session up -d

prod-down: ## Stop the published stack, keeping the volumes
	docker compose --env-file .env -f compose.prod.yaml --profile session down

build: ## Build everything
	$(MAKE) -C golang build
	$(MAKE) -C typescript build

test: ## Run the tests
	$(MAKE) -C golang test
	$(MAKE) -C typescript test

test-db: ## Run the record's tests against a real Postgres
	docker compose --env-file .env run --rm tests

test-broker: ## Run the tests against the broker's server on the development account
	docker compose --env-file .env run --rm tests go test -tags broker -count=1 ./internal/marketdata/...

check: ## Every gate a push must pass: style, language, tests, the race detector, both builds
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) -C golang test-race
	$(MAKE) build
	# The test stand is a module of its own, outside go.work on purpose, so
	# nothing above reaches it. Without these two lines the proxy that serves
	# every trial could be broken while this gate stayed green.
	$(MAKE) -C testbed lint
	$(MAKE) -C testbed test

claims: ## Recompute every number this project publishes, from data in the repository, with no credentials
	cd research && uv run claims.py

account-claims: ## What the account did, checked against what the docs say: make account-claims PAGE=https://... [KEY=...]
	@[ -n "$(PAGE)" ] || { echo "PAGE is required, for example: make account-claims PAGE=https://alpaca.swiftward.dev" >&2; exit 2; }
	python3 tools/account-claims.py --page "$(PAGE)" --key "$(KEY)"

day: ## The three numbers a trading day is judged by, per account, from the record
	@for db in $$(grep -E '^RECORD_DATABASES=' .env | cut -d= -f2-); do \
		echo "===== $$db"; \
		docker compose --env-file .env exec -T postgres \
			psql -U "$$(grep -E '^POSTGRES_USER=' .env | cut -d= -f2-)" -d "$$db" \
			-f - < postgres/the-day.sql; \
	done

rehearse: ## Send the reads a trading day sends, from every agent at once, and print what was refused
	# Run it BEFORE the day, including with the market closed: reads work at the
	# weekend and the answer is the same one Monday would give. On 31 August the
	# market opened and the screener could not read a single price - a rate limit
	# inside our own platform, left on its default, that no check had ever asked
	# for a burst. Two entry windows went before the cause was found.
	docker compose --env-file .env --profile tools run --rm rehearse

reconcile: ## Every order the broker holds, against what the record says: RECONCILE_SINCE=12h make reconcile
	docker compose --env-file .env --profile tools run --rm reconcile

lint: ## Check the style
	$(MAKE) -C golang lint
	$(MAKE) english

english: ## Refuse any text in this repository that is not English
	@if git grep -nIP '\p{Cyrillic}'; then \
		echo "This repository is written in English - see AGENTS.md. Translate the lines above."; \
		exit 1; \
	fi

fmt: ## Format
	$(MAKE) -C golang fmt

migrate: ## Apply the migrations (the stack does this itself on start)
	docker compose --env-file .env run --rm migrate
