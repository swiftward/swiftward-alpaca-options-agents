# Root Makefile. Every target here delegates to golang/, typescript/ or docker compose.

.DEFAULT_GOAL := help
SHELL := /bin/bash

.PHONY: help local-up local-down prod-up prod-down build test test-db test-broker lint english migrate fmt

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

claims: ## Recompute every number this project publishes, from data in the repository, with no credentials
	cd research && uv run claims.py

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
