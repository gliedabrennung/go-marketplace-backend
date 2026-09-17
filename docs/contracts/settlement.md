# Контракты портов: settlement

Проверяются `test/integration/settlement/repository_contract_test.go` (in-memory и PostgreSQL) и сквозным тестом `test/integration/ordering/checkout_test.go` (`TestFulfilmentAndSettlement`). Полное описание решения — ADR-0018.

## Репозиторий

| Метод | Контракт |
|-------|----------|
| `FindByOrderAndSeller(orderID, sellerID)` | запись начисления или `ErrEntryNotFound` |
| `Save` | одна запись на пару (заказ, продавец) (`uq_settlement_entries_order_seller`); повтор — `ErrAlreadyAccrued`, не ошибка обработки события |
| `ListBySeller(sellerID, from, to)` | записи в полуоткрытом интервале `[from, to)`, по времени начисления |

## Начисление

| Правило | Реализация |
|---------|-----------|
| Момент | подписчик `settlement.accrue_on_order_completed` на `ordering.order_completed.v1` — то есть не раньше `ReturnWindow` после доставки (FR-ST-02) |
| Группировка | по продавцу заказа; валовая сумма — сумма позиций этого продавца после скидок, без стоимости доставки |
| Комиссия | по каждой позиции через `seller.api.Directory.CommissionRate(sellerID, categoryID)`; неизвестная категория или ошибка — ставка по умолчанию (`Policy.DefaultCommissionRate`, 10%) |
| Инвариант | `net = gross − commission ≥ 0`; иное — `SETTLEMENT_INVALID_AMOUNT` |
| Идемпотентность | повторная доставка события не создаёт вторую запись |

## HTTP

| Эндпоинт | Контракт |
|----------|----------|
| `GET /api/v1/seller/settlements?seller_id=&period=YYYY-MM` | записи и итоги (`gross`, `commission`, `net`) за календарный месяц; участник продавца или поддержка; неверный формат периода — `SETTLEMENT_INVALID_PERIOD` |

## Ограничения текущей реализации

- Запись — бухгалтерская проекция «сколько причитается», а не выплата: перевод денег продавцу, реестры и статус выплаты добавляются на этапе 6 поверх `Entry`.
- Сторнирование при возврате товара (FR-ST-03) появится вместе с контекстом `review` на этапе 7.
