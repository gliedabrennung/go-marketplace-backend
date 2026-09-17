# Контракты портов: catalog

Проверяются `test/integration/catalog/repository_contract_test.go` для in-memory и PostgreSQL реализаций, HTTP-поток — `test/integration/catalog/http_test.go`.

## CategoryRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | категория с собственными атрибутами или `ErrCategoryNotFound` |
| `FindChain` | цепочка root → leaf; отсутствующая категория — `ErrCategoryNotFound`, разорванная цепочка — `ErrBrokenCategoryChain` |
| `DescendantAttributeCodes` | коды атрибутов всех потомков (для запрета дублей при определении атрибута) |
| `Save` | оптимистичная блокировка по `version`; атрибуты заменяются целиком; уникальность `slug` среди соседей — `ErrCategorySlugTaken` |

## ProductRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | карточка с атрибутами и изображениями в порядке сортировки или `ErrProductNotFound` |
| `Save` | оптимистичная блокировка по `version`; атрибуты и изображения заменяются целиком; `published_at` заполняется при публикации |

## VariantGroupRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | группа с участниками и значениями осей или `ErrVariantGroupNotFound` |
| `Save` | товар состоит максимум в одной группе — `ErrProductAlreadyGrouped` |

## OfferRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | оффер или `ErrOfferNotFound` |
| `FindBySellerSKU` | неархивный оффер продавца по его SKU или `ErrOfferNotFound` |
| `Save` | `ErrSellerSKUTaken` при повторе SKU, `ErrOfferExists` при втором неархивном оффере на тот же товар; архивация освобождает и SKU, и товар |

## ImportJobRepository

| Метод | Результат |
|-------|-----------|
| `FindByID` | задание вместе с построчными ошибками или `ErrImportJobNotFound` |
| `Save` | оптимистичная блокировка по `version` |

## Порты инфраструктуры

| Порт | Контракт |
|------|----------|
| `ObjectStorage` | presigned `PUT` на 15 минут, presigned `GET` на час, `Head`/`ReadPrefix` для подтверждения загрузки; отсутствующий объект — `ErrUploadMissing` |
| `ImageProber` | тип по содержимому (JPEG/PNG/WebP) и размеры; иначе `ErrInvalidImage` |
| `ThumbnailRenderer` | `small.jpg` (256 px) и `large.jpg` (1024 px) из оригинала |
| `ImportSource` | построчное чтение CSV (`,` или `;`), XLSX и JSON; отсутствие обязательных колонок — `CATALOG_IMPORT_MISSING_COLUMNS`; номер строки соответствует строке файла |
| `ImportReportWriter` | CSV-отчёт `row,field,code,message` с BOM и экранированием формул |
| `ImportLimiter` | 10 заданий в час на продавца, превышение — `429` |
| `SellerDirectory` | членство и право продавать из `internal/seller/api` |

## Правила доступа прикладного слоя

| Операция | Кто |
|----------|-----|
| Категории и атрибуты | `catalog.categories.manage`; действия пишутся в `platform.audit_log` |
| Модерация карточек (публикация, отклонение) | `catalog.products.moderate`; действия пишутся в `platform.audit_log` |
| Карточки, изображения, варианты, офферы, импорт | участник продавца; для не-участника объект не существует (`ErrProductNotFound`, `ErrOfferNotFound`, `ErrVariantGroupNotFound`, `ErrImportJobNotFound`) |
| Создание оффера, активация, планирование импорта | дополнительно требуется `CanSell` продавца, иначе `CATALOG_SELLER_INACTIVE` |
| Чтение неопубликованной карточки | владелец или `catalog.products.moderate`; остальным — `ErrProductNotFound` |
| Публичный список офферов товара | только активные офферы продавцов с правом продавать, сортировка по цене |
