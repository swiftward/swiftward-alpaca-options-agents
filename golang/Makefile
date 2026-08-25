.PHONY: build test test-db test-broker lint fmt

APPS := app

build: ## Собрать все приложения в bin/
	@for app in $(APPS); do go build -o bin/$$app ./apps/$$app; done

test:
	go test ./...

# Запускается из корневого `make test-db`, рядом с Postgres. Без DATABASE_URL
# тест падает, а не пропускается: тир, который молча зеленеет, ничего не проверяет.
test-db:
	go test -tags db -count=1 ./internal/record/... ./internal/volatility/...

# Проверка против настоящего сервера брокера на счёте для разработки.
test-broker:
	go test -tags broker -count=1 ./internal/marketdata/...

lint:
	go vet ./...

fmt:
	go fmt ./...
