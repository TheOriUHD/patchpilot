BINARY := patchpilot
CMD := ./cmd/patchpilot
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/TheOriUHD/patchpilot/internal/version.Version=$(VERSION) \
	-X github.com/TheOriUHD/patchpilot/internal/version.Commit=$(COMMIT) \
	-X github.com/TheOriUHD/patchpilot/internal/version.BuildDate=$(BUILD_DATE)

.PHONY: build build-linux test vet lint run-daemon run-tui clean release

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(CMD)

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY)-linux-amd64 $(CMD)

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run

run-daemon:
	go run $(CMD) daemon

run-tui:
	go run $(CMD) tui

clean:
	rm -rf bin dist

release: build-linux
	mkdir -p dist
	tar -C bin -czf dist/$(BINARY)-linux-amd64.tar.gz $(BINARY)-linux-amd64
