# Карта контекстов

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
        │                              └──┬────────┬────────┬────────┘
        │                          ┌──────▼──┐ ┌───▼────┐ ┌─▼──────────┐
        │                          │ payment │ │shipping│ │notification│
        │                          └────┬────┘ └───┬────┘ └────────────┘
        │                               │ ACL      │ ACL
        │                          ┌────▼────┐ ┌───▼───────────┐
        │                          │  PSP    │ │ Delivery API  │
        │                          └─────────┘ └───────────────┘
   ┌────▼────────┐
   │ settlement  │◄── order.completed, payment.captured
   └─────────────┘
```

| Пара | Отношение | Техническая реализация |
|------|-----------|------------------------|
| `identity` → все | OHS + Published Language | `internal/identity/api`: `UserID`, роли, разрешения |
| `catalog` → `cart`, `ordering` | Customer/Supplier | `internal/catalog/api`, версионирование контракта |
| `catalog` → `search` | Customer/Supplier | события `internal/catalog/api/events.go` и `catalog.Feed` для полной переиндексации |
| `catalog` → `inventory`, `pricing` | Customer/Supplier | события офферов и публикации товаров; SKU остатка и цены = ID оффера |
| `inventory` → `search` | Customer/Supplier | `internal/inventory/api`: события остатка, `Availability`, `Reserver` для оформления заказа |
| `seller` → `catalog`, `search` | Customer/Supplier | `internal/seller/api`: `Directory` (членство, право продавать), события статусов |
| `pricing` → `cart`, `ordering` | Conformist | `cart` и `ordering` используют `internal/pricing/api` (`Quote`, `Redeem`, `Release`) как есть |
| `cart` → `ordering` | Customer/Supplier | `internal/cart/api.Carts`: состав корзины для оформления и удаление оформленных позиций |
| `payment` → `ordering` | Customer/Supplier | `internal/payment/api.Payments` и события `payment.*.v1` |
| `payment` → PSP | ACL | порт `application.Provider`, адаптер `internal/payment/infrastructure/psp/sandbox`; DTO провайдера не покидают пакет адаптера (ADR-0015) |
| `shipping` → `cart`, `ordering` | Customer/Supplier | `internal/shipping/api.Tariffs`; до этапа 5 — фиксированный тариф (ADR-0017) |
| `shipping` → Delivery API | ACL | `internal/shipping/infrastructure/acl` (этап 5) |
| `settlement` → `ordering` | Customer/Supplier | подписка на `ordering.order_completed.v1` (ADR-0018) |
| `settlement` → `seller` | Conformist | `seller.api.Directory.CommissionRate` как есть |
| `shared/kernel` | Shared Kernel | `Money`, `Quantity`, `BasisPoints`, типизированные ID, `DomainEvent`, базовые ошибки |
| `ordering` → `inventory`, `pricing`, `payment`, `shipping` | Saga (оркестрация) | `internal/ordering/application/command`, состояние `ordering.checkout_sagas` (ADR-0016) |

## Доставка событий

События фиксируются в `platform.outbox` в транзакции агрегата и доставляются подписчикам диспетчером outbox в процессе `worker` (ADR-0004). Внешний брокер не используется. Контракты payload — `internal/<context>/api/events.go`.

| Подписчик | События | Действие |
|-----------|---------|----------|
| `identity.revoke_sessions_on_block` | `identity.user_blocked.v1` | отзыв всех активных сессий заблокированного пользователя |
| `catalog.image_thumbnails` | `catalog.product_image_uploaded.v1` | генерация превью `small.jpg`, `large.jpg` и перевод изображения в статус `processed` |
| `catalog.offer_import_runner` | `catalog.offer_import_scheduled.v1` | обработка файла пакетной загрузки офферов, отчёт об ошибках |
| `search.index_products` | `catalog.product_published.v1`, `catalog.product_image_processed.v1` | документ товара в поисковом индексе, обновление обложки |
| `search.index_offers` | `catalog.offer_created.v1`, `catalog.offer_updated.v1`, `catalog.offer_status_changed.v1` | состояние офферов и пересчёт `search.product_stats` |
| `inventory.track_offers` | `catalog.offer_created.v1` | позиция остатка с нулевым количеством (SKU = ID оффера) |
| `pricing.sync_offer_prices` | `catalog.offer_created.v1`, `catalog.offer_updated.v1`, `catalog.offer_status_changed.v1` | проекция цены оффера для котировок |
| `pricing.sync_product_categories` | `catalog.product_published.v1` | путь категорий товара для акций на категорию |
| `search.index_stock` | `inventory.stock_changed.v1` | доступный остаток оффера и пересчёт наличия |
| `ordering.checkout_payment_authorized` | `payment.authorized.v1` | подтверждение резерва, списание платежа, перевод заказа в `paid`; при отказе — компенсации саги |
| `settlement.accrue_on_order_completed` | `ordering.order_completed.v1` | начисление продавцам заказа за вычетом комиссии (ADR-0018) |
| `search.index_sellers` | `seller.application_approved.v1`, `seller.suspended.v1`, `seller.reinstated.v1`, `seller.terminated.v1` | право продавца на продажу и пересчёт наличия |

## Задачи по расписанию

| Задача | Период | Действие |
|--------|--------|----------|
| `inventory.expire_reservations` | 30 с | снятие просроченных резервов |
| `cart.purge_expired` | 1 ч | удаление анонимных корзин старше 30 дней |
| `ordering.expire_checkouts` | 15 с | компенсация саг без оплаты в срок |
| `ordering.run_compensations` | 15 с | повтор незавершённых компенсаций с паузой, перевод в `manual` |
| `ordering.resume_stalled_checkouts` | 1 мин | продолжение подтверждения оплаты после сбоя процесса |
| `ordering.complete_delivered_orders` | 1 ч | завершение доставленных заказов по истечении срока возврата (ADR-0018) |
| `payment.reconciliation` | 1 ч | сверка предыдущего дня с реестром провайдера, если отчёта ещё нет |
| `payment.webhook_cleanup` | 24 ч | удаление журнала вебхуков старше 30 дней |

## Правило Shared Kernel

Добавление в `internal/shared/kernel` — только универсальная семантика (деньги, количество, проценты, идентификаторы, контракт события) и только с обоснованием в PR. Бизнес-сущности (`Product`, `Order`, `Seller`) в ядро не попадают. `kernel` не импортирует ничего, кроме стандартной библиотеки (правило `kernel-purity`).
