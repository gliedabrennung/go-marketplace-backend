# Go Marketplace Backend

The backend of a marketplace in Go — a modular monolith designed with DDD, Outbox, ACL, Clean Architecture and aiming for production-grade quality: transactional consistency, idempotency, a checkout saga with compensations, and full test coverage.

## Architecture

**Modular monolith.** A single deployable unit (`cmd/api` + `cmd/worker`), but the code is split into independent bounded contexts, each with its own database schema and explicit outward-facing ports (`internal/<context>/api`). Contexts communicate only through ports and domain events — not by directly importing another context's `domain`/`application`/`infrastructure` packages (this rule is enforced in `.golangci.yml` via `depguard`, with a separate deny list for each context).

```
                            ┌──────────────┐
                            │   identity   │  Generic, Open Host Service
                            └──────┬───────┘
                                   │ UserID (Published Language)
        ┌──────────────────────────┼──────────────────────────┐
        │                          │                          │
   ┌────▼─────┐   Customer/   ┌────▼─────┐   Customer/   ┌────▼──────┐
   │  seller  │◄──Supplier────│ catalog  │◄──Supplier────│   cart    │
   └────┬─────┘               └────┬─────┘               └────┬──────┘
        │                     ┌────▼─────┐                    │
        │                     │ pricing  │──── Conformist ───►│
        │                     └──────────┘                    │
        │                     ┌──────────┐                    │
        │                     │inventory │◄───────────────────┤
        │                     └────┬─────┘                    │
        │                          │   ┌──────────────────────▼──────┐
        │                          └──►│  ordering (Saga Orchestr.)  │ Core
        │                              └──┬────────┬──────────────────┘
        │                          ┌──────▼──┐ ┌───▼────┐
        │                          │ payment │ │shipping│
        │                          └────┬────┘ └───┬────┘
        │                               │ ACL      │ ACL (port, stage 5)
        │                          ┌────▼────┐ ┌───▼───────────┐
        │                          │  PSP    │ │ Delivery API  │ (external)
        │                          └─────────┘ └───────────────┘
   ┌────▼────────┐
   │ settlement  │◄── ordering.order_completed.v1
   └─────────────┘
```

Inside, each context is organized into Clean Architecture layers:

```
internal/<context>/
├── domain/           aggregates, value objects, domain events, repository ports
├── application/      commands and queries (CQRS), ports to other contexts
├── infrastructure/   postgres, memory (for tests), httpapi, external adapters
├── api/              the context's public contract: event DTOs and interfaces for other contexts
├── module.go         dependency wiring, HTTP route registration
└── worker.go         event subscriptions and background jobs
```

## Tech Stack

| Component | Technology | Why |
|-----------|-----------|-----|
| Language | Go 1.26 | |
| HTTP | standard `net/http` (`http.ServeMux` with `METHOD /path/{param}` patterns) | No third-party routers — routing by method and path parameters has been in the standard library since Go 1.22 |
| Database | PostgreSQL 16 | Transactions, JSONB, maturity |
| DB driver | `pgx/v5` | Control over SQL; the domain doesn't leak into tables |
| Migrations | `goose` | Versioned SQL migrations |
| Cache / sessions / rate limiting | Redis 7 | |
| Event delivery | Transactional Outbox + dispatcher in `cmd/worker` (PostgreSQL only) | No external message broker |
| Search | PostgreSQL FTS (`tsvector`, `russian` configuration) + `pg_trgm` | Full-text search with morphology and typo tolerance without a separate search cluster |
| Object storage | SeaweedFS (S3-compatible API), `aws-sdk-go-v2` client | Media and import files via presigned URLs |
| Payment provider | `payment.Provider` port + built-in PSP emulator (`internal/payment/infrastructure/psp/sandbox`) | End-to-end payments and failure testing without an external dependency; a real provider is a new adapter for the same port |
| Logs | `log/slog`, JSON | |
| Metrics | Prometheus | |
| Tracing | OpenTelemetry | |
| Tests | `testing`, `testify`, `testcontainers-go` | Integration tests on real PostgreSQL rather than mocks |
| Linter | `golangci-lint` | Including `depguard` to enforce context boundaries and `gocyclo`/`funlen` for complexity |

## Quick Start

```bash
cp .env.example .env
make up
make migrate-up
make run-api
curl localhost:8080/readyz
```

Full environment in containers:

```bash
docker compose -f deploy/docker-compose.yml --profile app up --build
```

## Checks

```bash
make lint          # golangci-lint
make test          # unit tests, -race
make test-integration  # integration tests on testcontainers (PostgreSQL, SeaweedFS)
make cover-gate     # unit + integration coverage, thresholds: domain 90%, application 75%, total 70%
make vuln           # govulncheck
```

Current status: `golangci-lint` — 0 issues, `govulncheck` — 0 applicable vulnerabilities, coverage: domain 95.9% / application 90.4% / total 85.1%.

## Repository Structure

```
cmd/                     process entry points (api, worker, migrate)
internal/shared/kernel   Shared Kernel: Money, Quantity, BasisPoints, ID[T], DomainEvent, errors
internal/shared/platform technical components: postgres/UoW, outbox, inbox, idempotency,
                         httpx, auth, ratelimit, cqrs, observability, scheduler, breaker, audit
internal/<context>/      domain · application · infrastructure · api · module.go · worker.go
migrations/              goose SQL migrations (one per context/feature)
api/openapi              REST API contract (marketplace.v1.yaml)
deploy/                  docker-compose (postgres, redis, seaweedfs, services)
test/integration         integration tests (testcontainers)
test/e2e, test/load      planned for the stabilization stage
```
