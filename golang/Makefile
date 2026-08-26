.PHONY: build test test-db test-broker lint fmt

APPS := app

build: ## Собрать все приложения в bin/
	@for app in $(APPS); do go build -o bin/$$app ./apps/$$app; done

# -count=1 always: several tests read files Go's cache does not track - the agent
# declarations above all - and a cached pass on a file that has since broken is a
# guard that lies. It cost a crash-looping agent on 26 August.
# Тесты под тегами сборки в `./...` не попадают, поэтому переименование ломает
# их МОЛЧА: 26 августа `FromVolatility` стал `FromBorrowedVolatility`, весь
# обычный прогон остался зелёным, а тир записи не собирался. Поэтому здесь же
# проверяется, что КАЖДЫЙ тир хотя бы компилируется - без базы и без брокера.
test:
	go test -count=1 ./...
	go vet -tags db ./...
	go vet -tags broker ./...

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
