.PHONY: build test lint clean install dashboard-ui

BINARY := mantismo
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS := -ldflags "-X main.version=$(VERSION)"

build:
	CGO_ENABLED=0 go build $(LDFLAGS) -o bin/$(BINARY) ./cmd/mantismo/

test:
	CGO_ENABLED=0 go test -v -count=1 ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/

install: build
	cp bin/$(BINARY) $(GOPATH)/bin/

dashboard-ui:
	cd internal/dashboard/ui && npm install && npm run build
