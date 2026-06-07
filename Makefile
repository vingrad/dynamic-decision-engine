# dynamic-decision-engine — developer tasks
.DEFAULT_GOAL := help

BINARY      := dde
PKG         := ./...
BIN_DIR     := bin
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the dde binary into ./bin
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) ./cmd/dde

.PHONY: run
run: ## Run the API server
	go run ./cmd/dde serve

.PHONY: serve
serve: run ## Alias for run

.PHONY: migrate
migrate: ## Apply database migrations (requires DATABASE_URL)
	go run ./cmd/dde migrate

.PHONY: test
test: ## Run unit tests with the race detector
	go test $(PKG) -race -count=1

.PHONY: test-integration
test-integration: ## Run tests including Postgres (requires DATABASE_URL)
	go test $(PKG) -race -count=1

.PHONY: cover
cover: ## Run tests and write a coverage profile
	go test $(PKG) -race -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

.PHONY: fmt
fmt: ## Format Go code
	gofmt -w .

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: lint
lint: ## Run golangci-lint (must be installed)
	golangci-lint run

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	go mod tidy

.PHONY: docker-build
docker-build: ## Build the API Docker image
	docker build -t dde:$(VERSION) .

.PHONY: docker-up
docker-up: ## Start the full local stack
	docker compose up --build

.PHONY: docker-down
docker-down: ## Stop the local stack and remove volumes
	docker compose down -v

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out
