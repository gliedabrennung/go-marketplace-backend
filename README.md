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

## Применённые паттерны

| Паттерн | Где | Зачем |
|---------|-----|-------|
| **DDD: агрегаты и фабрики** | `internal/*/domain/*.go` — `Place`, `CreateOffer`, `Accrue`, `StartSaga` и т. д. | Приватные поля, единственный вход через фабричную функцию — невалидное состояние агрегата невозможно скомпилировать |
| **Value Objects** | `shared/kernel`: `Money`, `Quantity`, `BasisPoints`, типизированные `ID[T]` | Инварианты (валюта, неотрицательность) гарантированы типом, а не проверками в каждом месте использования |
| **Repository** | `internal/*/domain/repository.go` + `infrastructure/{postgres,memory}` | Домен не знает о SQL; один и тот же контракт проверяется контрактными тестами против обеих реализаций |
| **Unit of Work** | `internal/shared/platform/postgres/uow.go`, `application.UnitOfWork` в каждом контексте | Явная транзакционная граница вокруг одного агрегата на команду |
| **CQRS** | `internal/*/application/{command,query}`, `shared/platform/cqrs` | Разделение команд и чтений; у команд — доменные инварианты, у запросов — read-model под конкретный экран |
| **Decorator** | `shared/platform/cqrs/decorator.go` (`cqrs.Decorate`) | Трассировка, логирование и метрики вокруг любого обработчика без изменения его кода |
| **Transactional Outbox + Dispatcher** | `shared/platform/outbox` | Событие пишется в той же транзакции, что и агрегат; отдельный процесс (`cmd/worker`) публикует его подписчикам с ретраями и backoff — без внешнего брокера |
| **Inbox / дедупликация** | `shared/platform/inbox` | At-least-once доставка событий не создаёт повторных эффектов у подписчика |
| **Saga (оркестрация)** | `internal/ordering/domain/saga.go` + `application/command` | Оформление заказа — 7 шагов через `inventory`, `pricing`, `payment` с сохранённым состоянием и идемпотентными компенсациями при сбое любого шага |
| **Anticorruption Layer** | `internal/payment/infrastructure/psp/sandbox` | Модель платёжного провайдера (DTO эмулятора PSP) не протекает в домен `payment` — только через порт `application.Provider` |
| **Strategy** | `pricing.DiscountRule` (скидки), `shipping.Tariffs` (доставка), `payment.Provider` (провайдер оплаты) | Новый вид скидки / способ доставки / провайдер — новая реализация интерфейса, без правки существующего кода |
| **Specification** | `pricing.DiscountRule.IsApplicable`, `catalog` классификация атрибутов по дереву категорий | Комбинируемые бизнес-условия применимости правила |
| **Idempotency Key** | `shared/platform/idempotency` (HTTP), ключи `intent:`/`capture:`/`cancel:`/`refund:` в PSP-клиенте | Повтор запроса (сетевой таймаут, повтор клиента) не создаёт второй заказ, платёж или списание |
| **Optimistic Locking** | Поле `version` во всех агрегатах + `kernel.ErrConcurrentModification` | Конкурентные изменения одного агрегата обнаруживаются, а не перезаписывают друг друга |
| **Circuit Breaker** | `shared/platform/breaker`, обёрнут вокруг HTTP-клиента PSP | Изоляция отказа внешнего провайдера, метрика `circuit_breaker_state` |
| **Read Model / проекции** | `search` (Postgres FTS документы), `pricing.offer_prices`, `inventory` остатки | Данные для конкретного запроса денормализованы и обновляются подписчиками, а не JOIN'ами через границы контекстов |

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

## Процессы

| Бинарник | Назначение |
|----------|-----------|
| `cmd/api` | REST API, `/healthz`, `/readyz`; метрики на `METRICS_ADDR` |
| `cmd/worker` | Диспетчер outbox, превью изображений, пакетный импорт офферов, индексация поиска, истечение резервов, проекции цен, продвижение и компенсации саги оформления, завершение доставленных заказов, начисление продавцам, очистка анонимных корзин, сверка платежей |
| `cmd/migrate` | `up`, `down`, `reset`, `status`, `version` |

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

## Ограниченные контексты

| Контекст | Возможности | Контракт |
|----------|-------------|----------|
| `identity` | регистрация и вход по email/паролю (Argon2id) с подтверждением по ссылке, JWT ES256 + ротируемые refresh-токены с обнаружением повторного использования, список и отзыв сессий, блокировка пользователей, RBAC, лимиты попыток | [`docs/contracts/identity.md`](docs/contracts/identity.md) |
| `seller` | заявка ТОО/ИП с проверкой БИН/ИИН и IBAN, документы, модерация, жизненный цикл `draft → pending_review → active ⇄ suspended → terminated`, сотрудники продавца, комиссии по категориям и индивидуальные, рейтинг с автоприостановкой | [`docs/contracts/seller.md`](docs/contracts/seller.md) |
| `catalog` | дерево категорий с наследованием типизированных атрибутов, карточки товаров с модерацией, изображения через presigned URL с автогенерацией превью, группы вариантов, офферы продавцов, пакетная загрузка (CSV/XLSX/JSON) с отчётом об ошибках | [`docs/contracts/catalog.md`](docs/contracts/catalog.md) |
| `search` | полнотекстовый поиск PostgreSQL с морфологией и исправлением опечаток, фасеты, сортировки, курсорная пагинация, индексация от событий, полная переиндексация без простоя | [`docs/contracts/search.md`](docs/contracts/search.md) |
| `inventory` | остатки по SKU с резервами на 20 минут, подтверждение/отмена/истечение резервов, возвраты, append-only журнал движений, защита от oversell блокировкой строк | [`docs/contracts/inventory.md`](docs/contracts/inventory.md) |
| `pricing` | цены офферов и «цена до скидки», акции (процент, фиксированная сумма, N за M) с целями по SKU/продавцу/категории, приоритеты и несуммируемые скидки, промокоды с лимитами | [`docs/contracts/pricing.md`](docs/contracts/pricing.md) |
| `cart` | анонимная корзина по `X-Device-ID` со слиянием при входе, лимиты позиций и остатка, актуализация доступности и цен, промокод, разбивка по продавцам с доставкой, TTL 30 дней | [`docs/contracts/cart.md`](docs/contracts/cart.md) |
| `ordering` | оформление из корзины с идемпотентностью, заказ с частями продавцов и историей статусов, сага оформления с персистентным состоянием, таймаутом и повтором оплаты, компенсациями и ручным разбором, отмена с возвратом средств и товара | [`docs/contracts/ordering.md`](docs/contracts/ordering.md) |
| `payment` | порт PSP и встроенный эмулятор, hold/capture/cancel, частичные и полные возвраты, подписанные вебхуки с дедупликацией и allowlist, сохранённые способы оплаты (только токены), circuit breaker, ежесуточная сверка | [`docs/contracts/payment.md`](docs/contracts/payment.md) |
| `shipping` | порт тарифов доставки; сейчас — фиксированный тариф вместо интеграции с логистикой | [`docs/contracts/shipping.md`](docs/contracts/shipping.md) |
| `settlement` | начисление продавцу за вычетом комиссии по завершении заказа, отчёт за период; без реестра выплат | [`docs/contracts/settlement.md`](docs/contracts/settlement.md) |

Полный список архитектурных решений — [`docs/adr`](docs/adr/README.md), карта связей между контекстами — [`docs/context-map.md`](docs/context-map.md).

## Что дальше

Не реализовано (осознанно, вне текущего объёма учебного проекта): контексты `review` (отзывы, возвраты) и `notification`, полная интеграция `shipping` с реальной службой доставки, реестр и выгрузка выплат в `settlement`. Порты под них уже определены (`shipping.api.Tariffs`, `payment.api.Payments` и т. д.), так что подключение — это новый адаптер, а не переделка существующего кода.
