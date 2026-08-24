.PHONY: build test lint fmt

APPS := app backtest

build: ## Собрать все приложения в bin/
	@for app in $(APPS); do go build -o bin/$$app ./apps/$$app; done

test:
	go test ./...

lint:
	go vet ./...

fmt:
	go fmt ./...
