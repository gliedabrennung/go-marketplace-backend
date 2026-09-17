# Контракты портов: ordering

Проверяются `test/integration/ordering/repository_contract_test.go` (in-memory и PostgreSQL), `checkout_test.go` (сквозной сценарий через HTTP, эмулятор PSP и PostgreSQL) и unit-тестами `internal/ordering/application/command`.

## Репозитории

| Порт | Контракт |
|------|----------|
| `OrderRepository.FindByID` | заказ с позициями, частями продавцов и историей статусов или `ErrOrderNotFound` |
| `OrderRepository.Save` | позиции и части пишутся один раз при создании; далее меняются статус и платёж с оптимистичной блокировкой; история только дополняется; события — в outbox той же транзакцией; `CHECK total = subtotal − discount + shipping` |
| `SagaRepository.FindByOrder`, `FindByPayment` | сага или `ErrSagaNotFound`; ID текущего платежа уникален |
| `SagaRepository.Expired(now, limit)` | `running` в шагах до подтверждения оплаты с истёкшим сроком |
| `SagaRepository.Compensating(now, limit)` | `compensating`, у которых прошла пауза `min(10 с · 2^(попытки−1), 10 мин)` |
| `SagaRepository.Stalled(before, limit)` | `running` в шагах подтверждения без изменений с `before` |
| `SagaRepository.Save` | оптимистичная блокировка по `version` |

## Заказ

| Правило | Реализация |
|---------|-----------|
| Состав | 1–100 позиций с разными SKU; `base = unit_price × quantity`, `0 ≤ final ≤ base`; одна валюта |
| Части продавцов (FR-OR-02) | сумма, скидка, доставка и итог по каждому продавцу; доставка задана ровно для каждого продавца |
| Адрес | получатель, телефон 10–15 цифр, город, адрес, страна ISO alpha-2 (по умолчанию `KZ`), индекс до 20 символов |
| Статусы | `created → awaiting_payment → paid → in_fulfilment → shipped → delivered → completed`; `returning → returned`; `cancelled`, `failed`; прочие переходы — `ORDER_INVALID_TRANSITION` |
| Отмена (FR-OR-06) | покупатель-владелец или поддержка в статусах `created`, `awaiting_payment`, `paid`, `in_fulfilment`; обязательная причина до 500 символов; не во время подтверждения оплаты |
| История (FR-OR-10) | каждый переход с инициатором (`buyer`, `seller`, `support`, `system`), причиной и временем |
| Оплата | `paid` только при сумме, равной итогу заказа |

## Сага оформления (ADR-0016)

| Шаг | Действие | Компенсация |
|-----|----------|-------------|
| `stock_reserved` | котировка, `inventory.Reserve`, заказ `created` + сага | `return_stock` |
| `promo_redeemed` | `pricing.Redeem` | `release_promo` |
| `awaiting_payment` | `payment.Create`, заказ `awaiting_payment` | `cancel_payment` |
| `committing_stock` → `stock_committed` | событие `payment.authorized.v1`, `inventory.Commit` | `return_stock` (`Release` или `Restore`) |
| `payment_captured` | `payment.Capture` | `refund_payment` |
| `completed` | заказ `paid` | отмена заказа: возврат средств и товара |

Статусы саги: `running`, `compensating`, `completed`, `compensated`, `manual`. Последний шаг компенсации `close_order` переводит неоплаченный заказ в `failed`.

## Исполнение (ADR-0018)

Все продавцы заказа исполняют его синхронно — упрощение до появления отдельной сущности `Shipment` на этапе 5: у заказа один статус на все части.

| Действие | Правило |
|----------|---------|
| `MarkShipped` | продавец-участник любой части заказа или поддержка; `paid → in_fulfilment → shipped` одним вызовом; повтор без ошибки |
| `MarkDelivered` | тот же круг лиц; `shipped → delivered`, фиксируется `delivered_at`; повтор без ошибки |
| `Complete` | только система; `delivered → completed` не раньше `delivered_at + Policy.ReturnWindow` (`ORDER_RETURN_WINDOW`, по умолчанию 336 ч); задача `ordering.complete_delivered_orders` (раз в час) |

## HTTP

| Эндпоинт | Контракт |
|----------|----------|
| `POST /api/v1/orders` | `Idempotency-Key` обязателен; адрес, способ доставки, `payment.method_id` или `payment.save_method`, необязательный `expected_total` (расхождение — `ORDER_TOTAL_CHANGED`); 201 с `payment_url`; недоступные позиции — `ORDER_ITEMS_UNAVAILABLE`, нехватка остатка — `INVENTORY_INSUFFICIENT_STOCK` |
| `GET /api/v1/orders?status=&cursor=` | заказы покупателя, новые первыми |
| `GET /api/v1/orders/{id}` | заказ покупателя или для поддержки; ссылка на оплату только пока платёж `pending`, срок оплаты `pay_before` |
| `POST /api/v1/orders/{id}/cancel` | `Idempotency-Key`; ответ — заказ после отмены |
| `POST /api/v1/payments/{id}/retry` | `Idempotency-Key`; для отклонённого или отменённого текущего платежа до истечения срока — новый платёж; для платежа в ожидании — текущая ссылка; иначе `ORDER_PAYMENT_RETRY_NOT_ALLOWED` |
| `GET /api/v1/seller/orders?seller_id=&status=&cursor=` | участник продавца или поддержка; только оплаченные и отменённые заказы, только позиции этого продавца, без адреса покупателя |
| `POST /api/v1/seller/orders/{id}/ship` | участник любой части заказа или поддержка; 204; чужой продавец — 404 |
| `POST /api/v1/seller/orders/{id}/deliver` | то же; 204 |
| `GET /api/v1/admin/checkout-sagas?status=` | `ordering.orders.support` |
| `POST /api/v1/admin/checkout-sagas/{id}/resume` | `ordering.orders.support`; только для `manual` |

Адрес возврата после оплаты — `CHECKOUT_RETURN_URL` с параметром `order_id`; клиент его не передаёт.

## События

| Событие | Payload |
|---------|---------|
| `ordering.order_created.v1`, `ordering.order_paid.v1`, `ordering.order_completed.v1` | `OrderV1`: суммы, промокод, позиции (с категорией — для начисления в `settlement`), части продавцов, платёж |
| `ordering.order_awaiting_payment.v1` | `OrderStatusV1` с текущим платежом |
| `ordering.order_failed.v1` | `OrderStatusV1` с причиной |
| `ordering.order_cancelled.v1` | `OrderStatusV1`: инициатор, причина, нужен ли возврат, итог |
| `ordering.order_shipped.v1`, `ordering.order_delivered.v1` | `OrderShipmentV1`: заказ, покупатель, момент |
