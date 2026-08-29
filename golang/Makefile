.PHONY: build test test-db test-broker lint fmt

APPS := app

build: ## Build every application into bin/
	@for app in $(APPS); do go build -o bin/$$app ./apps/$$app; done

# -count=1 always: several tests read files Go's cache does not track - the agent
# declarations above all - and a cached pass on a file that has since broken is a
# guard that lies. It cost a crash-looping agent on 26 August.
# Tests behind a build tag are not in `./...`, so a rename breaks them SILENTLY:
# on 26 August `FromVolatility` became `FromBorrowedVolatility`, the ordinary run
# stayed green, and the record's tier did not compile. So this target also checks
# that EVERY tier at least builds - with no database and no broker.
test:
	go test -count=1 ./...
	go vet -tags db ./...
	go vet -tags broker ./...

# Run from the root `make test-db`, beside a Postgres. Without DATABASE_URL the
# test fails rather than skips: a tier that quietly goes green checks nothing.
test-db:
	go test -tags db -count=1 ./internal/record/... ./internal/volatility/...

# Checked against the broker's real server on the development account.
test-broker:
	go test -tags broker -count=1 ./internal/marketdata/...

lint:
	go vet ./...

fmt:
	go fmt ./...
