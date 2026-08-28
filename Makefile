# Root Makefile. Every target here delegates to golang/, typescript/ or docker compose.

.DEFAULT_GOAL := help
SHELL := /bin/bash

.PHONY: help up down build test test-db test-broker lint migrate fmt

help: ## List the targets
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

up: ## Start the local stack from this checkout
	docker compose --env-file .env up -d --build

down: ## Stop the stack, keeping the volumes
	docker compose --env-file .env down

prod-up: ## Start the stack from the published images, agent included
	# --profile session нужен: у агента стоит profiles: [session], чтобы голый
	# `up` не запускал торговлю случайно. Защита верная, но звать его надо явно -
	# без этого поднимается всё, КРОМЕ главного, и выглядит это как успех.
	docker compose --env-file .env -f compose.prod.yaml --profile session up -d

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

lint: ## Check the style
	$(MAKE) -C golang lint
	$(MAKE) -C typescript lint

fmt: ## Format
	$(MAKE) -C golang fmt

migrate: ## Apply the migrations (the stack does this itself on start)
	docker compose --env-file .env run --rm migrate
