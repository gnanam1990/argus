BINARY  := argus
PKG     := github.com/gnanam1990/argus
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

.PHONY: all build build-robotgo test lint cover tidy fmt clean

all: lint test build

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/argus

# macOS/Windows native backend (needs a C toolchain). On macOS, grant the
# resulting binary Screen Recording + Accessibility permissions.
build-robotgo:
	CGO_ENABLED=1 go build -tags robotgo -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/argus

test:
	go test -race ./...

lint:
	go vet ./...
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed; skipping (CI runs it)"

cover:
	go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

tidy:
	go mod tidy

fmt:
	gofmt -w .

clean:
	rm -rf bin dist coverage.out
