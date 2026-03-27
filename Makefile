# ==============================================================================
# gemara-content-service Makefile
# ==============================================================================
#
# Usage:
#   make all         - Runs tests and then builds the binary
#   make test        - Runs tests with coverage
#   make build       - Builds the binary and places it in the ./bin directory
#   make clean       - Removes generated binaries and build artifacts
#   make help        - Displays this help message
# ==============================================================================

BIN_DIR := bin
BINARY := compass

all: test build

# ------------------------------------------------------------------------------
# Test
# ------------------------------------------------------------------------------
test: ## Runs unit tests with coverage
	go test -v -coverprofile=coverage.out -covermode=atomic ./...
	@echo "Coverage summary:"
	@go tool cover -func=coverage.out | tail -n1
.PHONY: test

test-race: ## Runs tests with race detection
	go test -v -race ./...
.PHONY: test-race

coverage-report: test ## Generate HTML coverage report and show summary
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"
.PHONY: coverage-report

# ------------------------------------------------------------------------------
# Build
# ------------------------------------------------------------------------------
build: ## Builds the binary and places it in the $(BIN_DIR) directory
	@mkdir -p $(BIN_DIR)
	go build -v -o $(BIN_DIR)/$(BINARY) ./cmd/compass/
	@echo "--- Binary built: $(BIN_DIR)/$(BINARY) ---"
.PHONY: build

clean: ## Removes all generated binaries and build artifacts
	@echo "--- Cleaning up build artifacts ---"
	@rm -rf $(BIN_DIR) coverage.out coverage.html
	@echo "--- Cleanup complete ---"
.PHONY: clean

# ------------------------------------------------------------------------------
# Dependencies
# ------------------------------------------------------------------------------
deps: ## Tidy, verify, and download dependencies
	go mod tidy
	go mod verify
	go mod download
.PHONY: deps

# ------------------------------------------------------------------------------
# Code Generation
# ------------------------------------------------------------------------------
api-codegen: ## Runs go generate for OpenAPI code generation
	go generate ./...
.PHONY: api-codegen

# ------------------------------------------------------------------------------
# Linting
# ------------------------------------------------------------------------------
golangci-lint: ## Runs golangci-lint
	golangci-lint run ./...
.PHONY: golangci-lint

# ------------------------------------------------------------------------------
# CRAP Load Monitoring
# ------------------------------------------------------------------------------

GAZE_VERSION ?= latest
GAZE_BASELINE := .gaze/baseline.json
GAZE_COVERPROFILE := coverage.out
GAZE_NEW_FUNC_THRESHOLD ?= 30

ensure-gaze: ## Install gaze if not present
	@command -v gaze >/dev/null 2>&1 || \
		(echo "Installing gaze..." && go install github.com/unbound-force/gaze/cmd/gaze@$(GAZE_VERSION))
.PHONY: ensure-gaze

crapload: ensure-gaze test ## Run CRAP and GazeCRAP analysis (human-readable)
	gaze crap --format=text --coverprofile=$(GAZE_COVERPROFILE) ./...
.PHONY: crapload

crapload-baseline: ensure-gaze test ## Generate baseline thresholds in .gaze/baseline.json
	@mkdir -p .gaze
	@REPO_ROOT=$$(pwd); \
	gaze crap --format=json --coverprofile=$(GAZE_COVERPROFILE) ./... | \
		jq --arg root "$$REPO_ROOT/" '(.scores[],.summary.worst_crap[]?,.summary.worst_gaze_crap[]?) |= (.file |= ltrimstr($$root))' > $(GAZE_BASELINE)
	@echo "Baseline written to $(GAZE_BASELINE)"
.PHONY: crapload-baseline

crapload-check: ensure-gaze test ## Check for CRAP regressions against baseline
	@if [ ! -f $(GAZE_BASELINE) ]; then \
		echo "ERROR: Baseline file $(GAZE_BASELINE) not found. Run 'make crapload-baseline' first."; \
		exit 1; \
	fi
	@REPO_ROOT=$$(pwd); \
	gaze crap --format=json --coverprofile=$(GAZE_COVERPROFILE) ./... | \
		jq --arg root "$$REPO_ROOT/" '(.scores[],.summary.worst_crap[]?,.summary.worst_gaze_crap[]?) |= (.file |= ltrimstr($$root))' > /tmp/crapload-current.json
	@echo "Comparing against baseline..."
	@jq -r '.scores[] | "\(.file):\(.function) \(.crap) \(.gaze_crap // 0)"' $(GAZE_BASELINE) | sort > /tmp/crapload-baseline.txt
	@jq -r '.scores[] | "\(.file):\(.function) \(.crap) \(.gaze_crap // 0)"' /tmp/crapload-current.json | sort > /tmp/crapload-current.txt
	@REGRESSIONS=0; \
	while IFS=' ' read -r func crap gaze_crap; do \
		baseline_crap=$$(grep -F "$$func " /tmp/crapload-baseline.txt | head -1 | awk '{print $$2}'); \
		baseline_gaze=$$(grep -F "$$func " /tmp/crapload-baseline.txt | head -1 | awk '{print $$3}'); \
		if [ -z "$$baseline_crap" ]; then \
			if [ "$$(echo "$$crap > $(GAZE_NEW_FUNC_THRESHOLD)" | bc -l)" = "1" ]; then \
				echo "NEW FUNCTION VIOLATION: $$func CRAP=$$crap (threshold=$(GAZE_NEW_FUNC_THRESHOLD))"; \
				REGRESSIONS=$$((REGRESSIONS + 1)); \
			fi; \
		else \
			if [ "$$(echo "$$crap > $$baseline_crap" | bc -l)" = "1" ]; then \
				echo "REGRESSION: $$func CRAP $$baseline_crap -> $$crap"; \
				REGRESSIONS=$$((REGRESSIONS + 1)); \
			fi; \
			if [ "$$(echo "$$gaze_crap > $$baseline_gaze" | bc -l)" = "1" ]; then \
				echo "REGRESSION: $$func GazeCRAP $$baseline_gaze -> $$gaze_crap"; \
				REGRESSIONS=$$((REGRESSIONS + 1)); \
			fi; \
		fi; \
	done < /tmp/crapload-current.txt; \
	if [ $$REGRESSIONS -gt 0 ]; then \
		echo "FAIL: $$REGRESSIONS regression(s) detected"; \
		exit 1; \
	else \
		echo "PASS: No regressions detected"; \
	fi
.PHONY: crapload-check

# ------------------------------------------------------------------------------
# Help
# ------------------------------------------------------------------------------
help: ## Display this help screen
	@grep -E '^[a-z.A-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
.PHONY: help
