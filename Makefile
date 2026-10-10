GOLANGCI_VERSION := v2.14.0
BIN              := $(CURDIR)/bin
LINT             := $(BIN)/golangci-lint
ARCH             ?= amd64
VERSION          ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS          := -s -w -X main.version=$(VERSION)

.PHONY: all ci vet lint test build build-linux deploy clean

all: ci

ci: vet lint test build-linux

vet:
	go vet ./...

# The linter is built with the project's own toolchain so it always
# understands the go.mod Go version.
$(LINT):
	GOBIN=$(BIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)

lint: $(LINT)
	$(LINT) run ./...

test:
	go test -race -count=1 ./...

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o dist/todo-bot ./cmd/todo-bot

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -ldflags "$(LDFLAGS)" -o dist/todo-bot-linux-$(ARCH) ./cmd/todo-bot

# deploy is completed in #9 (scripts/deploy.sh).
deploy:
	./scripts/deploy.sh $(or $(CMD),update)

clean:
	rm -rf dist
