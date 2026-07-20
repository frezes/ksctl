.PHONY: build test fmt-check mod-check verify clean

GO ?= go
VERSION ?= dev
LDFLAGS := -s -w -X github.com/kubesphere/ksctl/pkg/cmd.version=$(VERSION)

build:
	@mkdir -p bin
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/ksctl ./cmd/ksctl
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o bin/kubectl-ks ./cmd/kubectl-ks

test:
	$(GO) test ./... -count=1

fmt-check:
	@test -z "$$(gofmt -l $$(git ls-files '*.go'))" || \
		(gofmt -l $$(git ls-files '*.go'); exit 1)

mod-check:
	$(GO) mod tidy -diff
	$(GO) mod verify

verify: fmt-check mod-check
	$(GO) vet ./...
	$(GO) test ./... -count=1
	$(GO) test -race ./... -count=1
	$(MAKE) build

clean:
	rm -f bin/ksctl bin/kubectl-ks
