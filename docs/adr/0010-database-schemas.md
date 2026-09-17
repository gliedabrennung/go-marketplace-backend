# ADR-0010. Разделение схем БД и переход к отдельным базам

**Статус:** Принято · **Дата:** 2026-09-15

## Решение

- PostgreSQL 16, одна база, **схема на контекст**: `identity`, `seller`, `catalog`, `inventory`, `pricing`, `cart`, `ordering`, `payment`, `shipping`, `settlement`, `review`, `notification`.
- Схема `platform` — технические таблицы: `outbox`, `idempotency_keys`, `processed_messages`, `audit_log`.
- FK между схемами не создаются; ссылки на чужие агрегаты — UUID без ограничений.
- Все запросы используют полностью квалифицированные имена (`ordering.orders`); `search_path` не используется.
- Первичные ключи — UUID v7 (`kernel.NewID`), генерируются приложением.
- Таблицы изменяемых агрегатов содержат `version INT NOT NULL`.
- Миграции — goose, SQL-файлы в `migrations/`, встраиваются в бинарник `migrate`; каждая миграция обратима (`-- +goose Down`), обратимость проверяется интеграционным тестом `TestMigrations_AreReversible`. Изменения схемы — expand/contract.

## Права доступа

В проде каждому контексту — роль `<context>_app` с правами только на свою схему и `INSERT` в `platform.outbox`, `platform.audit_log`; `SELECT/INSERT/UPDATE/DELETE` на `platform.idempotency_keys`, `platform.processed_messages` — ролям процессов `api` и `worker`. Выдача прав — SQL-скрипт администратора БД, применяемый после миграций. Миграции применяются ролью-владельцем.

## Переход к отдельной базе

1. Схема контекста переносится в отдельную базу логической репликацией.
2. Сервис получает собственные таблицы `outbox`, `idempotency_keys`, `processed_messages` (`outbox.NewWriter/NewRelay` параметризованы схемой и таблицей).
3. Запросы не меняются: имена схем сохраняются.
