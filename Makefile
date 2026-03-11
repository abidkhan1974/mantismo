.PHONY: build test test-e2e lint clean install dashboard-ui

BINARY  := mantismo
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"

# Locate Go — prefer the user's installation, fall back to whatever is in PATH.
GO      := $(shell command -v go 2>/dev/null || echo $(HOME)/go-install/go/bin/go)
LINT    := $(shell command -v golangci-lint 2>/dev/null || echo $(HOME)/go-install/go/bin/golangci-lint)

build:
	CGO_ENABLED=0 $(GO) build $(LDFLAGS) -o bin/$(BINARY) ./cmd/mantismo/

test:
	CGO_ENABLED=0 $(GO) test -v -count=1 -timeout 120s $(shell $(GO) list ./... | grep -v /e2e)

test-e2e:
	CGO_ENABLED=0 $(GO) test -v -count=1 -timeout 120s ./e2e/...

lint:
	$(LINT) run ./...

clean:
	rm -rf bin/

install: build
	sudo install -m 755 bin/$(BINARY) /usr/local/bin/$(BINARY)

dashboard-ui:
	cd internal/dashboard/ui && npm install && node build.js
