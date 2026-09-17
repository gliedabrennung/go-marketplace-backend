# Контракты портов: payment

Проверяются `test/integration/payment/repository_contract_test.go` (in-memory и PostgreSQL), `http_test.go` и `internal/payment/infrastructure/psp/sandbox/sandbox_test.go`.

## Репозитории

| Порт | Контракт |
|------|----------|
| `PaymentRepository.FindByID` | платёж с возвратами или `ErrPaymentNotFound`; в транзакции строка блокируется |
| `PaymentRepository.FindByProviderID(provider, providerPaymentID)` | поиск по ID намерения у провайдера; пустой ID не находит ничего |
| `PaymentRepository.Save` | оптимистичная блокировка по `version`; возвраты сохраняются вместе с платежом; события уходят в outbox той же транзакцией; `CHECK` суммы: `authorized ≤ amount`, `captured ≤ authorized`, `refunded ≤ captured` |
| `MethodRepository` | поиск по ID и по `(provider, token)`; удаление — отметка `removed_at`; токен уникален у провайдера |
| `WebhookLog.Record(provider, eventID, type, at)` | `true` для нового события, `false` для повтора; ключ `(provider, event_id)` |
| `Reconciliations.Ledger(provider, day)` | платежи дня (UTC) с ID у провайдера: статус, списано, возвращено |
| `Reconciliations.Save` | отчёт за день перезаписывается при повторном запуске |

## Жизненный цикл платежа

| Переход | Условие |
|---------|---------|
| `created → pending` | намерение создано у провайдера, получена ссылка на оплату |
| `pending → authorized` | вебхук `payment.authorized`, сумма и валюта совпадают с платежом |
| `authorized → captured` | списание не больше суммы авторизации |
| `created/pending → failed` | ошибка создания намерения или вебхук `payment.failed` |
| `created/pending/authorized → cancelled` | отмена заказа или таймаут; у провайдера отменяется намерение или hold |
| `captured → refunded` | сумма успешных возвратов достигла суммы списания |

Терминальные статусы `failed`, `cancelled`, `refunded` не меняются. Возврат — частичный или полный, `RefundableAmount = captured − (возвраты в ожидании + успешные)`; повтор с тем же `refund_id` возвращает существующий возврат.

## Публичный контракт `internal/payment/api`

| Метод | Результат |
|-------|-----------|
| `Payments.Create(CreateRequest)` | идемпотентен по `PaymentID`; статус `pending` и `RedirectURL` либо `failed`; `MethodID` чужого или удалённого способа — `PAYMENT_METHOD_NOT_FOUND` |
| `Payments.Capture(paymentID, amount)` | `0` — вся авторизованная сумма; повтор после списания без ошибки; не `authorized` — `PAYMENT_INVALID_TRANSITION` |
| `Payments.Cancel(paymentID, reason)` | повтор и уже `failed` без ошибки; после списания — `PAYMENT_INVALID_TRANSITION` |
| `Payments.Refund(paymentID, refundID, amount, reason)` | `0` — весь доступный остаток; отказ провайдера отмечает возврат `failed` и возвращает ошибку |
| `Payments.Info(paymentID)` | статус и суммы для оркестратора |
| `IsInvalidTransition(err)` | признак недопустимого перехода статуса |

События: `payment.pending.v1`, `payment.authorized.v1`, `payment.failed.v1`, `payment.captured.v1`, `payment.cancelled.v1` (`PaymentV1`), `payment.refund_requested.v1`, `payment.refund_completed.v1` (`RefundV1`).

## Порт провайдера

| Метод | Контракт |
|-------|----------|
| `CreateIntent` | ключ идемпотентности `intent:<payment_id>`; `ReturnURL` — абсолютный http(s) |
| `Capture`, `Cancel`, `Refund` | ключи `capture:`, `cancel:`, `refund:<refund_id>`, `late-void:`; отказ провайдера — `PAYMENT_PROVIDER_REJECTED` (не открывает circuit breaker) |
| `Transactions(day)` | намерения дня со статусом и суммами списания и возврата |
| `Verify(headers, body, now)` | заголовки в нижнем регистре; неверная или просроченная подпись — `PAYMENT_INVALID_WEBHOOK_SIGNATURE`; битый payload — `PAYMENT_INVALID_WEBHOOK` |

## Вебхуки

| Правило | Реализация |
|---------|-----------|
| Подпись | `X-Sandbox-Signature: t=<unix>,v1=<hex>`, HMAC-SHA256 от `<t>.<body>`, окно ±5 минут, допускается несколько `v1` |
| Источник | `PSP_WEBHOOK_ALLOWED_CIDRS`; иначе 403 |
| Размер | тело до 64 КБ |
| Повтор | ответ `{"received":true,"duplicate":true}` без изменений |
| Неизвестный платёж | 404, провайдер повторяет доставку |
| Поздняя авторизация | платёж в терминальном статусе — отмена hold у провайдера |
| Сохранение способа | при `save_method` токен из вебхука сохраняется за покупателем; повтор токена не создаёт дубль |

## Правила доступа

| Операция | Кто |
|----------|-----|
| `GET /api/v1/payments/{id}` | покупатель-владелец или `ordering.orders.support` |
| Сохранённые способы оплаты | только владелец |
| Отчёт сверки | `payment.refunds.initiate` |
| Вебхук | без пользовательской авторизации, подпись и allowlist |
