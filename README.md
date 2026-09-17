# Go Marketplace Backend

Учебный проект: бэкенд многопродавцовой торговой площадки (marketplace) на Go — модульный монолит, спроектированный по DDD и Clean Architecture с прицелом на прод-качество: транзакционная согласованность, идемпотентность, сага оформления заказа с компенсациями, полное покрытие тестами.

Цель проекта — не готовый к запуску продукт, а демонстрация того, как применяются архитектурные паттерны и практики (DDD, CQRS, Saga, Outbox, ACL и т. д.) в реальном по объёму бэкенде, а не в игрушечном примере на 200 строк. Часть контекстов (`shipping`, `settlement`) реализована в облегчённом виде — с портом и минимальной реализацией вместо полной интеграции с внешними системами; это задокументировано соответствующими ADR.

## Архитектура

**Модульный монолит.** Один деплойный юнит (`cmd/api` + `cmd/worker`), но код разделён на независимые ограниченные контексты (bounded contexts) со своими схемами БД и явными портами наружу (`internal/<context>/api`). Контексты общаются только через порты и доменные события — не через прямой импорт чужих `domain`/`application`/`infrastructure` пакетов (это правило зафиксировано в `.golangci.yml` через `depguard`, отдельный список запретов на каждый контекст).

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
        │                               │ ACL      │ ACL (порт, этап 5)
        │                          ┌────▼────┐ ┌───▼───────────┐
        │                          │  PSP    │ │ Delivery API  │ (внешние)
        │                          └─────────┘ └───────────────┘
   ┌────▼────────┐
   │ settlement  │◄── ordering.order_completed.v1
   └─────────────┘
```

Каждый контекст внутри устроен по слоям Clean Architecture:

```
internal/<context>/
├── domain/           агрегаты, value objects, доменные события, порты репозиториев
├── application/      команды и запросы (CQRS), порты к другим контекстам
├── infrastructure/   postgres, memory (для тестов), httpapi, внешние адаптеры
├── api/              публичный контракт контекста: DTO событий и интерфейсы для других контекстов
├── module.go         сборка зависимостей, регистрация HTTP-маршрутов
└── worker.go         подписки на события и фоновые задачи
```

Зависимости идут только внутрь и через `api`: `infrastructure` знает про `domain` и `application`, `application` знает про `domain`, `domain` не знает ни про что снаружи стандартной библиотеки и `shared/kernel`.

## Технологический стек

| Компонент | Технология | Почему |
|-----------|-----------|--------|
| Язык | Go 1.26 | |
| HTTP | стандартный `net/http` (`http.ServeMux` с шаблонами `METHOD /path/{param}`) | Без сторонних роутеров — маршрутизация по методу и параметрам пути есть в стандартной библиотеке с Go 1.22 |
| БД | PostgreSQL 16 | Транзакции, JSONB, зрелость |
| Драйвер БД | `pgx/v5`, без ORM | Контроль над SQL, домен не утекает в таблицы |
| Миграции | `goose` | Версионируемые SQL-миграции |
| Кэш / сессии / rate limit | Redis 7 | |
| Доставка событий | Transactional Outbox + диспетчер в `cmd/worker` (только PostgreSQL) | Без внешнего брокера сообщений |
| Поиск | PostgreSQL FTS (`tsvector`, конфигурация `russian`) + `pg_trgm` | Полнотекст с морфологией и исправлением опечаток без отдельного поискового кластера |
| Объектное хранилище | SeaweedFS (S3-совместимое API), клиент `aws-sdk-go-v2` | Медиафайлы и файлы импорта через presigned URL |
| Платёжный провайдер | Порт `payment.Provider` + встроенный эмулятор PSP (`internal/payment/infrastructure/psp/sandbox`) | Сквозная оплата и тесты отказов без внешней зависимости; реальный провайдер — новый адаптер того же порта |
| Логи | `log/slog`, JSON | |
| Метрики | Prometheus | |
| Трейсинг | OpenTelemetry | |
| Тесты | `testing`, `testify`, `testcontainers-go` | Интеграционные тесты на реальном PostgreSQL, а не на моках |
| Линтер | `golangci-lint` | В т.ч. `depguard` для соблюдения границ контекстов и `gocyclo`/`funlen` для сложности |

## Быстрый старт

```bash
cp .env.example .env
make up
make migrate-up
make run-api
curl localhost:8080/readyz
```

Оплата на стендах идёт через встроенный эмулятор PSP (`PSP_PROVIDER=sandbox`): `payment_url` из ответа `POST /api/v1/orders` открывает страницу `/sandbox/psp/pay/{id}` с кнопками «Оплатить» и «Отклонить», эмулятор отправляет подписанный вебхук на `/webhooks/payments/sandbox`.

Полный стенд в контейнерах:

```bash
docker compose -f deploy/docker-compose.yml --profile app up --build
```

## Проверки

```bash
make lint          # golangci-lint
make test          # unit-тесты, -race
make test-integration  # интеграционные тесты на testcontainers (PostgreSQL, SeaweedFS)
make cover-gate     # unit + integration coverage, пороги: domain 90%, application 75%, total 70%
make vuln           # govulncheck
```

Интеграционные тесты сами поднимают PostgreSQL и SeaweedFS через testcontainers; для внешних сервисов можно задать `TEST_DATABASE_URL` и `TEST_S3_ENDPOINT`.

Текущее состояние: `golangci-lint` — 0 замечаний, `govulncheck` — 0 применимых уязвимостей, покрытие domain 95.9% / application 90.4% / total 85.1%.

## Структура репозитория

```
cmd/                     точки входа процессов (api, worker, migrate)
internal/shared/kernel   Shared Kernel: Money, Quantity, BasisPoints, ID[T], DomainEvent, ошибки
internal/shared/platform технические компоненты: postgres/UoW, outbox, inbox, idempotency,
                         httpx, auth, ratelimit, cqrs, observability, scheduler, breaker, audit
internal/<context>/      domain · application · infrastructure · api · module.go · worker.go
migrations/              SQL-миграции goose (по одной на контекст/фичу)
api/openapi              контракт REST API (marketplace.v1.yaml)
deploy/                  docker-compose (postgres, redis, seaweedfs, сервисы)
docs/adr                 architecture decision records
docs/contracts           контракт каждого контекста: инварианты, HTTP, события
docs/context-map.md      карта контекстов и правило Shared Kernel
test/integration         интеграционные тесты (testcontainers)
test/e2e, test/load      план на этап стабилизации
```
