.PHONY: help setup build test test-race cover lint vuln vet fuzz fuzz-long install snapshot clean

BINARY := mcp-slack-blockkit
PKG := github.com/hishamkaram/mcp-slack-blockkit

help: ## Show this help.
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2}'

setup: ## One-time setup after clone: install Lefthook git hooks.
	@command -v lefthook >/dev/null 2>&1 || { \
		echo "ERROR: lefthook not found on PATH."; \
		echo "  Install: brew install lefthook   (macOS/Linux)"; \
		echo "           or download a release from https://github.com/evilmartians/lefthook/releases"; \
		echo "  Note: 'go install github.com/evilmartians/lefthook/v2@latest' currently"; \
		echo "        requires Go 1.26 to build. Project uses Go 1.25 — Homebrew is the path of least resistance."; \
		exit 1; \
	}
	lefthook install
	@echo "Hooks installed. Try: git commit --allow-empty -m 'chore: test commit-msg hook'"

build: ## Build the binary into ./bin/.
	@mkdir -p bin
	go build -o bin/$(BINARY) ./cmd/$(BINARY)

test: ## Run the test suite.
	go test ./...

test-race: ## Run tests with the race detector and coverage.
	go test -race -coverprofile=cover.out ./...

cover: test-race ## Run tests and open the HTML coverage report.
	go tool cover -html=cover.out

vet: ## Run go vet.
	go vet ./...

lint: ## Run golangci-lint (requires v2.12+ installed locally).
	golangci-lint run

vuln: ## Run govulncheck across all packages.
	govulncheck ./...

fuzz: ## Smoke fuzz the splitter for 30s (matches CI smoke).
	cd internal/splitter && go test -run=^$$ -fuzz=FuzzSplitText -fuzztime=30s

fuzz-long: ## Long fuzz the splitter for 10 minutes (matches nightly CI).
	cd internal/splitter && go test -run=^$$ -fuzz=FuzzSplitText -fuzztime=10m

install: ## Install the binary into $$GOBIN (or $$GOPATH/bin).
	go install ./cmd/$(BINARY)

snapshot: ## Build a multi-arch snapshot via GoReleaser (no publish).
	goreleaser build --snapshot --clean

clean: ## Remove build artifacts.
	rm -rf bin dist cover.out
