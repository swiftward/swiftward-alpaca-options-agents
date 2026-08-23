# Root Makefile. Every target here delegates to golang/, typescript/ or postgres/.

.DEFAULT_GOAL := help
SHELL := /bin/bash

.PHONY: help up down build test lint migrate seed fmt

help: ## Показать список целей
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'

up: ## Поднять локальный стек из этой копии
	docker compose --env-file .env up -d --build

down: ## Остановить стек, тома сохранить
	docker compose --env-file .env down

prod-up: ## Поднять стек на опубликованных образах
	docker compose --env-file .env -f compose.prod.yaml up -d

build: ## Собрать всё
	$(MAKE) -C golang build
	$(MAKE) -C typescript build

test: ## Прогнать тесты
	$(MAKE) -C golang test
	$(MAKE) -C typescript test

lint: ## Проверить стиль
	$(MAKE) -C golang lint
	$(MAKE) -C typescript lint

fmt: ## Отформатировать
	$(MAKE) -C golang fmt

migrate: ## Накатить миграции
	$(MAKE) -C postgres migrate
