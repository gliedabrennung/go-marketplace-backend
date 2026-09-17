# Контракты портов: inventory

Проверяются `test/integration/inventory/repository_contract_test.go` (in-memory и PostgreSQL), `concurrency_test.go` (гонки на реальной базе) и `http_test.go`.

## StockRepository

| Метод | Результат |
|-------|-----------|
| `FindBySKU` | позиция с резервами или `ErrStockNotFound`; без блокировки, только для чтения владельца |
| `Lock(skus)` | `SELECT … FOR UPDATE` в порядке возрастания SKU; отсутствующие SKU просто не возвращаются, проверку делает прикладной слой (`ErrStockNotFound`) |
| `LockByReservation` | все позиции, содержащие резерв, заблокированные в порядке SKU; пустой результат для неизвестного резерва |
| `LockExpired(before, limit)` | позиции с резервами `held` и `expires_at <= before` |
| `Save` | новая позиция — вставка, повтор SKU — `ErrStockExists`; существующая — оптимистичная проверка `version`; резервы сохраняются upsert-ом по `(id, sku)`; движения пишутся `ON CONFLICT (reason, reference_id, sku) DO NOTHING`; события — в outbox той же транзакции |

## Инварианты и их защита

| Инвариант | Где обеспечивается |
|-----------|-------------------|
| `available >= 0`, `reserved >= 0` | домен (`kernel.Quantity`), последний рубеж — `CHECK` в `inventory.stock_items` |
| `available + reserved` = физический остаток | переходы домена: резерв и отмена перекладывают единицы между пулами, списание уменьшает `reserved` |
| Нет oversell при конкуренции | блокировка строк `FOR UPDATE` в транзакции (ADR-0003); тест: 200 параллельных резервов на 10 единиц → ровно 10 успешных |
| Повторный резерв с тем же `reservation_id` | не меняет состояние и возвращает исходный `expires_at` |
| Подтверждение и отмена только из `held` | повтор той же операции идемпотентен, другой переход — `INVENTORY_RESERVATION_RESOLVED` |
| Многострочный резерв атомарен | все строки в одной транзакции: нехватка по любой SKU откатывает весь резерв |

## Журнал движений

`inventory.stock_movements` — append-only. `delta` — изменение физического остатка, поэтому сумма `delta` по SKU равна `available + reserved`.

| Причина | `delta` | `reference_id` |
|---------|---------|----------------|
| `restock` | `+q` | номер поставки |
| `correction` | новое `available` − старое | ссылка синхронизации продавца или сгенерированный ID |
| `reserve` | `0` | `reservation_id` |
| `release` (отмена и истечение TTL) | `0` | `reservation_id` |
| `commit` | `−q` | `reservation_id` |
| `return` | `+q` | номер возврата |

## Публичный контракт `internal/inventory/api`

| Метод | Результат |
|-------|-----------|
| `Reserver.Reserve(reservationID, orderID, lines)` | резерв с TTL 20 минут; `ErrInsufficientStock`, `ErrStockNotFound` |
| `Reserver.Commit` / `Reserver.Release` | идемпотентные переходы резерва |
| `Availability.Available(skus)` | доступный остаток по известным SKU; неизвестные SKU отсутствуют в ответе |

## Фоновые процессы

| Процесс | Поведение |
|---------|-----------|
| `inventory.track_offers` | на `catalog.offer_created.v1` создаёт позицию с нулевым остатком (SKU = ID оффера) |
| `inventory.expire_reservations` | каждые 30 секунд снимает просроченные резервы пачками по 200 позиций |

## Правила доступа

| Операция | Кто |
|----------|-----|
| Установка остатка, просмотр остатков и журнала | участник продавца-владельца; для остальных позиция не существует |
| Резерв, подтверждение, отмена | только внутренние вызовы через `inventory/api` (оркестратор заказа) |
