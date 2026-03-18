# =============================================================================
# ranf Makefile
# =============================================================================

BINARY      := ranf
MODULE      := github.com/risqinf/ranf
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS     := -s -w -X main.version=$(VERSION)
BUILD_FLAGS := -ldflags="$(LDFLAGS)" -trimpath

# Output directory for cross-compiled binaries
DIST_DIR := dist

# All supported platforms
PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

.PHONY: all build clean test fmt vet lint run repl check install release help

## all: Build the binary for the current platform
all: build

## build: Compile ranf for the current OS/ARCH
build:
	@echo "→ Building $(BINARY) $(VERSION)"
	@go build $(BUILD_FLAGS) -o $(BINARY) ./cmd/ranf
	@echo "✓ $(BINARY) ready"

## run: Run a .ranf file — usage: make run FILE=examples/01_hello.ranf
run: build
	@./$(BINARY) run $(FILE)

## repl: Start the interactive REPL
repl: build
	@./$(BINARY) repl

## test: Run the full test suite
test:
	@echo "→ Running tests"
	@go test -v -race -count=1 ./...

## test-short: Run tests without the race detector (faster)
test-short:
	@go test -short ./...

## fmt: Format all Go source files
fmt:
	@echo "→ Formatting"
	@gofmt -s -w .
	@echo "✓ Done"

## vet: Run go vet
vet:
	@echo "→ Vetting"
	@go vet ./...
	@echo "✓ No issues"

## lint: Run golangci-lint (install separately)
lint:
	@golangci-lint run ./...

## check-examples: Run 'ranf check' on every example file
check-examples: build
	@echo "→ Checking all examples"
	@for f in examples/*.ranf; do \
		./$(BINARY) check $$f && echo "  ✓ $$f" || exit 1; \
	done

## run-examples: Run every example file
run-examples: build
	@echo "→ Running all examples"
	@for f in examples/*.ranf; do \
		echo ""; \
		echo "=== $$f ==="; \
		./$(BINARY) run $$f; \
	done

## install: Install ranf to GOPATH/bin
install:
	@echo "→ Installing $(BINARY)"
	@go install $(BUILD_FLAGS) ./cmd/ranf
	@echo "✓ Installed to $$(go env GOPATH)/bin/$(BINARY)"

## clean: Remove built artifacts
clean:
	@echo "→ Cleaning"
	@rm -f $(BINARY)
	@rm -rf $(DIST_DIR)
	@echo "✓ Clean"

## release-all: Cross-compile for all platforms into dist/
release-all:
	@echo "→ Cross-compiling for all platforms (version: $(VERSION))"
	@mkdir -p $(DIST_DIR)
	$(foreach platform,$(PLATFORMS), \
		$(eval GOOS   := $(word 1,$(subst /, ,$(platform)))) \
		$(eval GOARCH := $(word 2,$(subst /, ,$(platform)))) \
		$(eval EXT    := $(if $(filter windows,$(GOOS)),.exe,)) \
		$(eval OUT    := $(DIST_DIR)/$(BINARY)-$(GOOS)-$(GOARCH)$(EXT)) \
		@echo "  $(GOOS)/$(GOARCH) → $(OUT)" && \
		GOOS=$(GOOS) GOARCH=$(GOARCH) CGO_ENABLED=0 \
			go build $(BUILD_FLAGS) -o $(OUT) ./cmd/ranf; \
	)
	@echo ""
	@echo "✓ All binaries in $(DIST_DIR)/"
	@ls -lh $(DIST_DIR)/

## tidy: Tidy and verify go.mod
tidy:
	@go mod tidy
	@go mod verify

## help: Print this help message
help:
	@echo "Usage: make <target>"
	@echo ""
	@grep -E '^##' Makefile | sed 's/## /  /'
