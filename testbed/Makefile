.DEFAULT_GOAL := help

# GOWORK=off on every line, and that is deliberate. The repository's go.work
# names ./golang alone, so a module outside it is not merely un-built: `go test`
# refuses with "directory prefix . does not contain modules listed in go.work",
# which reads like a broken module rather than one kept out of the workspace on
# purpose. Adding this module to go.work would put a stand for questioning the
# agent into the same build as the agent, and the one thing this folder must
# never do is enter the trading path.

help: ## Show these commands
	@grep -hE '^[a-z-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

test: ## Run the proxy's tests
	cd proxy && GOWORK=off go test -count=1 ./...

lint: ## Vet and check the layout
	cd proxy && GOWORK=off go vet ./...
	@cd proxy && unformatted=$$(GOWORK=off gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "unformatted: $$unformatted"; exit 1; fi

build: ## Build the proxy into bin/
	cd proxy && GOWORK=off go build -o ../bin/testbed-proxy .

.PHONY: help test lint build
