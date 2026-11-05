BINARY := anycompare
PKG := ./cmd/anycompare
OUT := bin/$(BINARY)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

GOFLAGS ?=
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build build-all build-native install test vet lint clean run-demo help

all: build

build:
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(OUT) $(PKG)

# Cross-compile static Linux binaries for both supported architectures.
build-all:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath $(GOFLAGS) -ldflags "$(LDFLAGS)" -o dist/$(BINARY)-linux-arm64 $(PKG)
	file dist/$(BINARY)-linux-amd64 dist/$(BINARY)-linux-arm64 || true

# Build for the host machine (convenience alias for build).
build-native: build

install: build
	install -d $(DESTDIR)$(BINDIR)
	install -m 755 $(OUT) $(DESTDIR)$(BINDIR)/$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

lint:
	gofmt -l cmd internal

clean:
	rm -f $(BINARY)
	rm -rf bin
	rm -rf dist

run-demo:
	go run $(PKG) testdata/left testdata/right

help:
	@echo "Targets:"
	@echo "  make            build $(OUT) for the host (same as make build)"
	@echo "  make build      build $(OUT) (static, CGO disabled)"
	@echo "  make build-all  cross-compile dist/$(BINARY)-linux-amd64 and -arm64"
	@echo "  make install    install $(OUT) to \$$(BINDIR) (default $(BINDIR))"
	@echo "  make test       go test ./..."
	@echo "  make vet        go vet ./..."
	@echo "  make clean      remove $(OUT) and dist/"
	@echo ""
	@echo "Variables: VERSION=$(VERSION)  (override: make VERSION=v1.0.0 build)"
	@echo "           BINDIR=$(BINDIR)  (override: make install PREFIX=~/.local)"
