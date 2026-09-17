GO           ?= go
BINARIES     := api worker migrate
COMPOSE      := docker compose -f deploy/docker-compose.yml
DATABASE_URL ?= postgres://marketplace:marketplace@localhost:5432/marketplace?sslmode=disable
VERSION      ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

export DATABASE_URL

.PHONY: all build test test-integration test-race-inventory cover cover-gate lint vuln fmt tidy \
	migrate-up migrate-down migrate-status run-api run-worker up down logs docker

all: lint test build

build:
	@mkdir -p bin
	@for b in $(BINARIES); do \
		CGO_ENABLED=0 $(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/$$b ./cmd/$$b || exit 1; \
	done

test:
	$(GO) test -race -count=1 ./...

test-integration:
	$(GO) test -race -count=1 -tags=integration ./test/integration/...

cover:
	$(GO) test -count=1 -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage-unit.out ./internal/...
	$(GO) test -count=1 -covermode=atomic -coverpkg=./internal/... -coverprofile=coverage-integration.out -tags=integration ./test/integration/...

cover-gate: cover
	./scripts/coverage-gate.sh coverage-unit.out coverage-integration.out

lint:
	golangci-lint run ./...

vuln:
	govulncheck ./...

fmt:
	gofmt -s -w cmd internal test migrations

tidy:
	$(GO) mod tidy

migrate-up:
	$(GO) run ./cmd/migrate up

migrate-down:
	$(GO) run ./cmd/migrate down

migrate-status:
	$(GO) run ./cmd/migrate status

run-api:
	APP_ENV=dev LOG_LEVEL=debug IDENTITY_DEV_REVEAL_CODES=true METRICS_ADDR=:9090 $(GO) run ./cmd/api

run-worker:
	APP_ENV=dev LOG_LEVEL=debug METRICS_ADDR=:9091 $(GO) run ./cmd/worker

up:
	$(COMPOSE) up -d postgres redis seaweedfs

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

docker:
	@for b in $(BINARIES); do \
		docker build --build-arg SERVICE=$$b --build-arg VERSION=$(VERSION) -t marketplace-$$b:$(VERSION) . || exit 1; \
	done
