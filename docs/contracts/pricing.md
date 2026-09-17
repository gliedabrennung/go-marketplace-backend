# Контракты портов: pricing

Проверяются `test/integration/pricing/repository_contract_test.go` (in-memory и PostgreSQL) и `http_test.go`.

## Репозитории

| Порт | Контракт |
|------|----------|
| `PromotionRepository` | `FindByID` или `ErrPromotionNotFound`; `Save` с оптимистичной блокировкой по `version` |
| `PromoCodeRepository.FindByCode` | счётчики и условия без списка применений (агрегат не растёт с числом заказов) |
| `PromoCodeRepository.CustomerUsage` | число применений кода покупателем |
| `PromoCodeRepository.Redeemed` | применён ли код к заказу |
| `PromoCodeRepository.Save` | повтор кода — `ErrPromoCodeExists`; новые применения вставляются до обновления счётчика, повтор `(code, order_id)` — `ErrPromoAlreadyUsed`; превышение `total_limit` на уровне базы — `ErrPromoCodeDepleted` |
| `OfferPriceRepository` | цена оффера и «цена до скидки», оптимистичная блокировка |
| `CategoryIndex` | путь категорий товара для правил, нацеленных на категорию |

## Расчёт цены

| Правило | Реализация |
|---------|-----------|
| Типы скидок | `DiscountRule`: процент, фиксированная сумма, «N за цену M»; новый тип — новая реализация интерфейса без изменения `PriceCalculator` |
| Цели акции | SKU, продавцы, категории (любой уровень пути) |
| Порядок | по убыванию приоритета; правило с признаком «не суммируется» останавливает цепочку для позиции |
| Неотрицательность | скидка обрезается до текущей суммы позиции; итог позиции не бывает меньше нуля |
| Промокод | применяется после скидок акций к итогу корзины, распределяется по позициям пропорционально их сумме, минимальная сумма корзины сравнивается с суммой после акций |
| Валюта | все позиции котировки в одной валюте, иначе `PRICING_CURRENCY_MISMATCH` |
| Недоступный оффер | архивный или приостановленный оффер — `PRICING_OFFER_PRICE_INACTIVE` |

## Публичный контракт `internal/pricing/api`

| Метод | Результат |
|-------|-----------|
| `Pricer.Quote` | котировка с детализацией скидок по позициям; оформление заказа сохраняет её как зафиксированную цену (FR-PR-06) |
| `Pricer.Redeem(code, orderID, customerID, subtotal, currency)` | фиксирует применение: повтор для заказа — `ErrPromoAlreadyUsed`, лимиты — `PRICING_PROMO_CODE_DEPLETED`, `PRICING_PROMO_CODE_PER_BUYER` |
| `Pricer.Release(code, orderID)` | возвращает применение при отмене заказа; повтор и неизвестный код — без ошибки |

## Синхронизация с каталогом

| Подписчик | События | Действие |
|-----------|---------|----------|
| `pricing.sync_offer_prices` | `catalog.offer_created.v1`, `catalog.offer_updated.v1`, `catalog.offer_status_changed.v1` | цена оффера; активна только при статусе `active` |
| `pricing.sync_product_categories` | `catalog.product_published.v1` | путь категорий товара |

## Правила доступа

| Операция | Кто |
|----------|-----|
| Акции и промокоды | `pricing.promotions.manage` (роль `platform_admin`) |
| «Цена до скидки» оффера | участник продавца-владельца |
| Котировка | любой клиент, включая анонимного; для авторизованного учитывается лимит промокода на покупателя |
