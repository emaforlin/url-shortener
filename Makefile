# Development tasks. Run `make` or `make help` for the list.
#
# Every target here is also what CI should run, so a green `make check` locally
# means the same thing as a green pipeline.

BINARY       := api
CMD_PATH     := ./cmd/api
LAMBDA_BINARY  := bootstrap
LAMBDA_CMD_PATH := ./cmd/lambda
LAMBDA_OUTPUT := ./dist
LAMBDA_ZIP    := $(LAMBDA_OUTPUT)/lambda.zip
BIN_DIR      := bin
GO           ?= go

# Stamped into the binary as main.version. Override it for a reproducible build:
#   make build VERSION=1.4.2
VERSION      ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS      := -s -w -X main.version=$(VERSION)
DOCKER_IMAGE ?= $(BINARY)

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@echo "Targets:"
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Compile the binary into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(CMD_PATH)

.PHONY: build-lambda
build-lambda: ## Package the Lambda function as dist/lambda.zip (linux/arm64)
	@mkdir -p $(LAMBDA_OUTPUT) $(BIN_DIR)
	@rm -f $(LAMBDA_ZIP) $(BIN_DIR)/$(LAMBDA_BINARY)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 $(GO) build -trimpath -ldflags="$(LDFLAGS)" -tags lambda.norpc -o $(BIN_DIR)/$(LAMBDA_BINARY) $(LAMBDA_CMD_PATH)
	zip -X -q -j $(LAMBDA_ZIP) $(BIN_DIR)/$(LAMBDA_BINARY)

.PHONY: build-all
build-all: build build-lambda ## Compile the binary for all targets
	@echo "All binaries built."

.PHONY: run
run: ## Run the service from source
	$(GO) run -ldflags="$(LDFLAGS)" $(CMD_PATH)

.PHONY: test
test: ## Run the test suite
	$(GO) test ./...

.PHONY: test-race
test-race: ## Run the test suite under the race detector
	$(GO) test -race ./...

.PHONY: cover
cover: ## Run tests and open the coverage report
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1
	$(GO) tool cover -html=coverage.out -o coverage.html
	@echo "report: coverage.html"

.PHONY: fmt
fmt: ## Format every file in place
	gofmt -w .

.PHONY: fmt-check
fmt-check: ## Fail if any file is not gofmt-clean
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: lint
lint: ## Run golangci-lint, falling back to go vet when it is not installed
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, running go vet instead."; \
		echo "install: https://golangci-lint.run/welcome/install/"; \
		$(GO) vet ./...; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

.PHONY: check
check: fmt-check vet lint test-race ## Everything CI should run
	@echo "all checks passed"

.PHONY: docker-build
docker-build: ## Build the container image
	docker build --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE):$(VERSION) -t $(DOCKER_IMAGE):latest .

.PHONY: docker-run
docker-run: ## Run the container image on port 8080
	docker run --rm -p 8080:8080 --env-file .env.example $(DOCKER_IMAGE):latest

.PHONY: clean
clean: ## Remove build and coverage artifacts
	rm -rf $(BIN_DIR) $(LAMBDA_OUTPUT) coverage.out coverage.html