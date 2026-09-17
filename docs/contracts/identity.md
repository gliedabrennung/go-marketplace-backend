# Контракты портов: identity

Контракты проверяются тестом `test/integration/identity/repository_contract_test.go`, который прогоняется для in-memory и PostgreSQL реализаций.

## Общие правила репозиториев

| Правило | Проверка |
|---------|----------|
| `Save` агрегата с `Version() == 0` вставляет запись; повторная вставка того же ID → `kernel.ErrConcurrentModification` | `Save with stale version returns ErrConcurrentModification` |
| `Save` агрегата с `Version() > 0` обновляет запись только при совпадении версии, иначе `kernel.ErrConcurrentModification` | то же |
| Успешный `Save` увеличивает `Version()` на 1 и очищает буфер событий | `Save inserts, increments version and round-trips state` |
| События пишутся в `platform.outbox` в той же транзакции и доставляются подписчикам диспетчером `worker` | `TestHTTP_AdminBlockRevokesSessionsThroughOutbox` |
| Загруженный агрегат полностью инициализирован: `Snapshot()` совпадает с сохранённым | round-trip тесты |

## UserRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | агрегат или `ErrUserNotFound` |
| `FindByVerifiedEmail` | только пользователь с подтверждённым email, иначе `ErrUserNotFound` |
| `Save` | `ErrEmailTaken` при конфликте среди подтверждённых; неподтверждённые дубли допустимы — повторная регистрация выдаёт новую ссылку |

## SessionRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | сессия или `ErrSessionNotFound` |
| `FindByRefreshDigest` | сессия по текущему **или ранее выданному** хэшу refresh-токена — основа обнаружения повторного использования; иначе `ErrSessionNotFound` |
| `Save` | сохраняет текущий хэш в истории `identity.refresh_tokens` |

## ChallengeRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | вызов подтверждения email или `ErrChallengeNotFound` |
| `Save` | сохраняет число попыток и статус с оптимистичной блокировкой |

## Прикладные порты

| Порт | Контракт |
|------|----------|
| `PasswordHasher.Verify(hash, password)` | при нулевом `hash` выполняет вычисление над фиктивным хэшем и возвращает `false` — время ответа не раскрывает существование пользователя |
| `AttemptLimiter.Allow` | ошибка с `KindRateLimited` и `RetryAfter()` при превышении; при недоступности хранилища — внутренняя ошибка (fail-closed) |
| `ConfirmationSender` | вне dev-окружения без почтового провайдера возвращает `IDENTITY_DELIVERY_UNAVAILABLE` |
| `AccessTokenIssuer.Issue` | JWT ES256 с `sub`, `sid`, `roles`, TTL из конфигурации |

## Подписчики событий

| Подписчик | Событие | Контракт |
|-----------|---------|----------|
| `identity.revoke_sessions_on_block` | `identity.user_blocked.v1` | отзывает все активные сессии; повторная доставка не меняет результат; некорректный payload пропускается с ошибкой в логе |
