# Контракты портов: seller

Проверяются `test/integration/seller/repository_contract_test.go` для in-memory и PostgreSQL реализаций.

## SellerRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | агрегат со всеми дочерними коллекциями (участники, документы, переопределения комиссий, рейтинг) или `ErrSellerNotFound` |
| `Save` (новый) | вставка; повтор → `kernel.ErrConcurrentModification` |
| `Save` (существующий) | оптимистичная блокировка по `version`; дочерние коллекции заменяются целиком в той же транзакции |
| `Save` | `ErrSellerAlreadyExists`, если у владельца есть другой нетерминированный продавец; `ErrTaxIDTaken` для БИН/ИИН; терминированный продавец освобождает владельца и БИН/ИИН |

## CategoryCommissionRepository

| Метод | Результат |
|-------|-----------|
| `FindByCategory` | ставка или `ErrCategoryCommissionNotFound` |
| `Save` | оптимистичная блокировка по `version` |

## Публичный контракт `internal/seller/api.Directory`

| Метод | Результат |
|-------|-----------|
| `Seller(sellerID)` | `SellerInfo{Status, CanSell, PayoutsAllowed}` или `ErrSellerNotFound` (в том числе для некорректного ID) |
| `MemberRole(sellerID, userID)` | `(role, true)` для участника, `("", false)` иначе; ошибка только при сбое хранилища |
| `CommissionRate(sellerID, categoryID)` | базисные пункты по `CommissionPolicy`: индивидуальная ставка продавца → ставка категории → ставка площадки по умолчанию (10%) |

## Правила доступа прикладного слоя

| Операция | Кто |
|----------|-----|
| Заявка, реквизиты, документы, отправка, участники | участник продавца с ролью `seller_admin`; для не-участника продавец не существует (`ErrSellerNotFound`) |
| Одобрение, отклонение, верификация реквизитов | `seller.applications.moderate` |
| Приостановка, восстановление, прекращение | `seller.sellers.manage` |
| Комиссии | `seller.commissions.manage` |
| Все действия персонала платформы | пишутся в `platform.audit_log` |
