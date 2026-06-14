BINARY := patchpilot
CMD := ./cmd/patchpilot
DIST := dist
RELEASE_DIR := $(DIST)/release
VERSION ?= dev
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/TheOriUHD/patchpilot/internal/version.Version=$(VERSION) \
	-X github.com/TheOriUHD/patchpilot/internal/version.Commit=$(COMMIT) \
	-X github.com/TheOriUHD/patchpilot/internal/version.BuildDate=$(BUILD_DATE)

LXC_USER ?= root
REMOTE_TMP ?= /tmp/patchpilot-install

.PHONY: build build-linux build-arm64 test vet lint check clean install release run-daemon run-tui

build:
	mkdir -p $(DIST)
	go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY) $(CMD)

build-linux:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux $(CMD)

build-arm64:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BINARY)-linux-arm64 $(CMD)

test:
	go test -count=1 ./...

vet:
	go vet ./...

lint:
	golangci-lint run

check: vet lint test

clean:
	rm -rf $(DIST)/

install: build-linux
	@if [ -z "$(LXC_HOST)" ]; then echo "LXC_HOST is required, for example: make install LXC_HOST=debian12.local LXC_USER=root" >&2; exit 1; fi
	ssh $(LXC_USER)@$(LXC_HOST) 'rm -rf $(REMOTE_TMP) && mkdir -p $(REMOTE_TMP)'
	scp $(DIST)/$(BINARY)-linux deploy/install.sh deploy/patchpilot.service $(LXC_USER)@$(LXC_HOST):$(REMOTE_TMP)/
	ssh $(LXC_USER)@$(LXC_HOST) 'cd $(REMOTE_TMP) && mv $(BINARY)-linux $(BINARY) && if [ "$$(id -u)" -eq 0 ]; then ./install.sh; else sudo ./install.sh; fi'

release: clean
	mkdir -p $(RELEASE_DIR)
	set -eu; \
	for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${target%/*}; \
		arch=$${target#*/}; \
		platform=$$os-$$arch; \
		pkg=patchpilot-$(VERSION)-$$platform; \
		pkgdir=$(RELEASE_DIR)/$$pkg; \
		mkdir -p $$pkgdir; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o $$pkgdir/$(BINARY) $(CMD); \
		cp README.md LICENSE deploy/install.sh $$pkgdir/; \
		if [ "$$os" = "linux" ]; then cp deploy/patchpilot.service $$pkgdir/; fi; \
		(cd $$pkgdir && shasum -a 256 $(BINARY) > $(BINARY).sha256); \
		tar -C $(RELEASE_DIR) -czf $(RELEASE_DIR)/$$pkg.tar.gz $$pkg; \
		rm -rf $$pkgdir; \
	done
	cd $(RELEASE_DIR) && shasum -a 256 *.tar.gz > SHA256SUMS

run-daemon:
	mkdir -p $$HOME/.patchpilot
	PATCHPILOT_CONFIG=$$HOME/.patchpilot/config.yaml \
	PATCHPILOT_DATA_DIR=$$HOME/.patchpilot \
	PATCHPILOT_DB_PATH=$$HOME/.patchpilot/patchpilot.db \
	PATCHPILOT_SOCKET_PATH=$$HOME/.patchpilot/patchpilot.sock \
	PATCHPILOT_LOG_PATH=$$HOME/.patchpilot/daemon.log \
	PATCHPILOT_SECRET_KEY_PATH=$$HOME/.patchpilot/secret.key \
	go run $(CMD) daemon

run-tui:
	mkdir -p $$HOME/.patchpilot
	PATCHPILOT_SOCKET=$$HOME/.patchpilot/patchpilot.sock \
	PATCHPILOT_CONFIG=$$HOME/.patchpilot/config.yaml \
	PATCHPILOT_DATA_DIR=$$HOME/.patchpilot \
	PATCHPILOT_DB_PATH=$$HOME/.patchpilot/patchpilot.db \
	PATCHPILOT_SOCKET_PATH=$$HOME/.patchpilot/patchpilot.sock \
	PATCHPILOT_LOG_PATH=$$HOME/.patchpilot/daemon.log \
	PATCHPILOT_SECRET_KEY_PATH=$$HOME/.patchpilot/secret.key \
	go run $(CMD) tui
