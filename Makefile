BIN     := lv
PKG     := ./cmd/lv
OUT     := bin/$(BIN)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Tags are v0.2.0 and the flake's version is 0.2.0; lv prints the latter
# however it was built.
LDFLAGS := -s -w -X main.version=$(patsubst v%,%,$(VERSION))

# Every target runs inside the nix dev shell. Re-exec into it when we are not
# already there, so make behaves identically with or without direnv loaded.
ifeq ($(IN_NIX_SHELL),)
  NIX := nix develop --command
else
  NIX :=
endif
GO := $(NIX) go

GOFILES := $(shell find . -name '*.go' -not -path './bin/*')

.PHONY: all build install test race bench vet fmt lint check clean release scale cross release-check

all: build

build:
	@mkdir -p bin
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(OUT) $(PKG)

install:
	$(GO) install -trimpath -ldflags '$(LDFLAGS)' $(PKG)

# Reproducible build of the released artifact.
release:
	$(NIX) true
	nix build .#lv

test:
	$(GO) test ./...

# Race detection needs a 48-bit VMA; it does not run on every machine.
race:
	$(GO) test -race ./...

bench:
	$(GO) test -run '^$$' -bench . -benchmem ./...

# Correctness and resident-memory check against a file larger than RAM:
#   make scale LV_SCALE_FILE=/path/to/huge.log
scale:
	LV_SCALE_FILE=$(LV_SCALE_FILE) $(GO) test -run TestScale -v -timeout 60m ./internal/view/

vet:
	$(GO) vet ./...

# Every platform lv claims to run on. A package that only compiles on the host
# otherwise goes unnoticed until someone tries to build it elsewhere.
CROSS := linux/amd64 linux/arm64 darwin/arm64 freebsd/amd64 openbsd/amd64 windows/amd64

cross:
	@for t in $(CROSS); do \
		echo "build $$t"; \
		GOOS=$${t%/*} GOARCH=$${t#*/} $(GO) build ./... || exit 1; \
		GOOS=$${t%/*} GOARCH=$${t#*/} $(GO) vet ./... || exit 1; \
	done

# The release tooling: its scripts and their end-to-end test.
release-check:
	$(NIX) shellcheck scripts/*.sh
	$(NIX) bash scripts/release_test.sh

fmt:
	$(NIX) gofmt -w $(GOFILES)

lint:
	$(NIX) golangci-lint run

check: fmt vet cross test release-check
	@test -z "$$($(NIX) gofmt -l $(GOFILES))" || { echo "gofmt needed:"; $(NIX) gofmt -l $(GOFILES); exit 1; }

clean:
	rm -rf bin result
