# Makefile
SHELL := /usr/bin/env bash
GO := go
PKG := chatbot-factory-pp-cli/internal/cli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X $(PKG).version=$(VERSION)
BIN := chatbot-factory-pp-cli
OUT := dist

.PHONY: build install test lint golden golden-update release clean

build:
	$(GO) build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN) ./cmd/chatbot-factory-pp-cli

install:
	$(GO) install -trimpath -ldflags="$(LDFLAGS)" ./cmd/chatbot-factory-pp-cli

test:
	$(GO) test ./...

lint:
	golangci-lint run ./...

golden:
	$(GO) test ./internal/cli/... -run TestGolden -v

golden-update:
	$(GO) test ./internal/cli/... -run TestGolden -update-golden

release: clean
	@mkdir -p $(OUT)
	@for OS in linux darwin windows; do \
	  for ARCH in amd64 arm64; do \
	    EXT=""; [[ $$OS == windows ]] && EXT=".exe"; \
	    echo "Building $$OS/$$ARCH..."; \
	    GOOS=$$OS GOARCH=$$ARCH $(GO) build -trimpath -ldflags="$(LDFLAGS)" \
	      -o $(OUT)/$(BIN)-$$OS-$$ARCH$$EXT ./cmd/chatbot-factory-pp-cli; \
	  done; \
	done
	cd $(OUT) && sha256sum * > checksums.txt

clean:
	rm -rf $(OUT) $(BIN) $(BIN).exe
