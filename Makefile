GOLANGCI_VERSION := v2.14.0
BIN              := $(CURDIR)/bin
LINT             := $(BIN)/golangci-lint
ARCH             ?= amd64
VERSION          ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS          := -s -w -X main.version=$(VERSION)
# TAGS is a list of Go build tags. release-linux sets webui.
TAGS             ?=

.PHONY: all ci vet lint test build build-linux web web-test release-linux deploy check-deploy clean

all: ci

ci: vet lint test check-deploy build-linux

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
	go build -trimpath -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o dist/todo-bot ./cmd/todo-bot

build-linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=$(ARCH) go build -trimpath -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o dist/todo-bot-linux-$(ARCH) ./cmd/todo-bot

# The web UI. These targets need Node (web/.nvmrc). make ci does not.
# "web" builds the front end into internal/web/dist.
web:
	cd web && npm ci --ignore-scripts && npm run build

# Typecheck, unit tests and the size budget (docs/WEB-DESIGN.md section 8.5).
web-test: web
	cd web && npm run typecheck && npm test && npm run size

# The front end, then a Linux binary that embeds it (the webui build tag).
release-linux: web
	$(MAKE) build-linux TAGS=webui ARCH=$(ARCH) VERSION=$(VERSION)

# CMD is one of bootstrap|update|rollback|status|logs.
deploy:
	./scripts/deploy.sh $(or $(CMD),update)

check-deploy:
	./scripts/check-deploy.sh

clean:
	rm -rf dist internal/web/dist
