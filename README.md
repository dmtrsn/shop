# Слой — интернет-магазин кондитерских изделий

Полноценное демонстрационное приложение: современный адаптивный storefront, каталог с быстрым поиском и фасетами, иерархия «производитель → бренд → серия → товар», подборки, избранное, корзина для больших заказов и безопасный двухфазный повтор заказа. Backend написан на Go в стиле чистой архитектуры; рабочее хранилище — PostgreSQL, а для быстрого локального просмотра есть in-memory режим.

## Что реализовано

- каталог с категориями и подкатегориями, производителями, брендами, сериями, тегами, ценой, наличием, сортировкой и пагинацией;
- подсказки поиска по товару, артикулу, производителю и бренду;
- редакционные подборки, карточка товара и группировка по производителям;
- авторизованные корзина, избранное, история заказов и оформление заказа;
- массовое выделение позиций и группировка большой корзины по производителям;
- предварительный просмотр повторного заказа: можно исключить позиции, изменить количество, увидеть дефицит, снятые товары, уже лежащий в корзине объём и изменение цены;
- атомарное добавление подтверждённых позиций повторного заказа;
- JSON-логи, request ID, security headers, ограничение JSON-body и graceful shutdown;
- PostgreSQL-миграции и воспроизводимые демоданные.

Демо-пользователь доступен в обоих backend-режимах:

```text
Email:    demo@sugar.ru
Пароль:   Sweet2026!
```

## Быстрый запуск через Docker Compose

Понадобятся Docker Engine и Docker Compose v2.

```bash
cp .env.example .env
docker compose config --quiet
docker compose up --build -d
docker compose ps
curl --fail http://127.0.0.1:8080/api/health
```

После запуска откройте <http://127.0.0.1:8080>. PostgreSQL и HTTP по умолчанию опубликованы только на loopback-интерфейсе. Для доступа снаружи задайте `APP_BIND=0.0.0.0`, но в production рекомендуется оставить приложение за TLS reverse proxy.

Логи и остановка:

```bash
docker compose logs -f app postgres
docker compose down
```

Именованный volume `postgres_data` сохраняется после `down`. Команда `docker compose down -v` необратимо удалит локальную базу и потребует повторной инициализации.

Перед публичным запуском обязательно замените `POSTGRES_PASSWORD` и ту же часть пароля в `DATABASE_URL`. Спецсимволы в URL-креденшалах должны быть percent-encoded.

## Локальный запуск без Docker

Требуется Go 1.22 или новее.

### In-memory demo mode

```bash
make run-memory
```

Или явно:

```bash
DATABASE_URL= \
WEB_DIR=web \
MIGRATIONS_DIR=migrations \
go run ./cmd/server
```

Если `DATABASE_URL` пуст, сервер автоматически использует заранее заполненный memory repository. Это удобный режим для интерфейса, API и тестирования сценариев, но все корзины, сессии и заказы исчезают при перезапуске процесса. Для нескольких реплик и любых сохраняемых данных он не подходит.

### PostgreSQL mode

Можно поднять только базу из Compose и запустить Go-процесс на хосте:

```bash
cp .env.example .env
docker compose up -d postgres

DATABASE_URL='postgres://sweetshop:sweetshop_local_2026@127.0.0.1:5432/sweetshop?sslmode=disable' \
RUN_MIGRATIONS=true \
WEB_DIR=web \
MIGRATIONS_DIR=migrations \
make run-postgres
```

При первом старте приложение создаст `schema_migrations`, применит все `*.up.sql` по имени и загрузит демоданные. Каждая миграция выполняется транзакционно и повторно не применяется. Для нескольких production-реплик миграции следует запускать ровно одним экземпляром приложения, а на остальных задавать `RUN_MIGRATIONS=false`.

## Конфигурация backend

Сервер читает переменные окружения напрямую; `.env` автоматически обрабатывает Docker Compose, но не команда `go run`.

| Переменная | Значение по умолчанию | Назначение |
|---|---:|---|
| `DATABASE_URL` | пусто | PostgreSQL DSN. При пустом значении включается memory demo mode. |
| `RUN_MIGRATIONS` | `true` | Автомиграции PostgreSQL; отключаются только точным значением `false`. |
| `WEB_DIR` | `web` | Каталог статического frontend. В образе — `/app/web`. |
| `MIGRATIONS_DIR` | `migrations` | Каталог SQL-миграций. В образе — `/app/migrations`. |
| `HTTP_ADDR` | `:$PORT` | Полный адрес HTTP listener; имеет приоритет над `PORT`. |
| `PORT` | `8080` | Порт, если `HTTP_ADDR` не задан. |
| `DB_MAX_CONNS` | `max(4, CPU)` | Максимальный размер пула PostgreSQL. |
| `DB_MIN_CONNS` | `1` | Минимальный размер пула PostgreSQL. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn` или `error`. |

Проверка готовности:

```bash
curl --fail http://127.0.0.1:8080/api/health
```

Ответ содержит `status`, UTC-время и активное хранилище (`memory` либо `postgres`).

## API

Все ответы — JSON, денежные значения с суффиксом `Minor` передаются целым числом в копейках. Защищённым маршрутам нужен заголовок `Authorization: Bearer <token>`. Сессия действует 30 дней; в базе хранится SHA-256-хэш, а не сам bearer token.

| Метод и путь | Назначение | Авторизация |
|---|---|:---:|
| `GET /api/health` | Состояние процесса и тип хранилища | — |
| `POST /api/auth/login` | Вход и выдача bearer token | — |
| `GET /api/me` | Текущий пользователь | ✓ |
| `GET /api/categories` | Дерево категорий через `parentId` | — |
| `GET /api/manufacturers` | Производители | — |
| `GET /api/brands` | Бренды и связь с производителем | — |
| `GET /api/collections` | Редакционные подборки | — |
| `GET /api/catalog` | Поиск, фильтры, сортировка, пагинация | — |
| `GET /api/catalog/facets` | Доступные значения фасетов и их счётчики | — |
| `GET /api/products/{product}` | Товар по ID, slug или SKU | — |
| `GET /api/search/suggest?q=...` | До восьми подсказок; поиск начинается с двух символов | — |
| `GET /api/cart` | Корзина, группы и итоговые счётчики | ✓ |
| `POST /api/cart/items` | Добавить/задать количество позиции | ✓ |
| `PATCH /api/cart/items/{product}` | Изменить количество или выделение | ✓ |
| `DELETE /api/cart/items/{product}` | Удалить позицию | ✓ |
| `DELETE /api/cart?selectedOnly=true` | Очистить выбранные или все позиции | ✓ |
| `PATCH /api/cart/selection` | Массово выделить позиции | ✓ |
| `GET /api/favorites` | Избранное, включая группы производителей | ✓ |
| `PUT /api/favorites/{product}` | Добавить в избранное | ✓ |
| `DELETE /api/favorites/{product}` | Удалить из избранного | ✓ |
| `GET /api/orders` | История заказов | ✓ |
| `GET /api/orders/{order}` | Один заказ | ✓ |
| `POST /api/orders` | Оформить выбранные позиции корзины | ✓ |
| `POST /api/orders/{order}/repeat/preview` | Рассчитать доступность старого заказа | ✓ |
| `POST /api/orders/{order}/repeat/commit` | Подтвердить отредактированный повтор | ✓ |

Параметры `GET /api/catalog`: `q`, `category`, `manufacturer`, `brand`, `collection`, `series`, повторяемый `tag` либо `tags=a,b`, `minPriceMinor`, `maxPriceMinor`, `inStock`, `sort`, `page`, `limit`. `limit` ограничен значением 100. Сортировки: `popular`, `new`, `price_asc`, `price_desc`, `rating`, `name`.

Пример входа:

```bash
curl --fail --request POST http://127.0.0.1:8080/api/auth/login \
  --header 'Content-Type: application/json' \
  --data '{"email":"demo@sugar.ru","password":"Sweet2026!"}'
```

Скопируйте поле `token` из ответа:

```bash
TOKEN='<token>'
curl --fail --header "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/api/cart
```

### Повтор заказа

Сначала frontend вызывает preview. Для демозаказа в PostgreSQL:

```bash
curl --fail --request POST \
  --header "Authorization: Bearer $TOKEN" \
  http://127.0.0.1:8080/api/orders/ord-2026-002/repeat/preview
```

Ответ показывает для каждой позиции заказанное, доступное и уже находящееся в корзине количество, текущую цену, статус `available`, `limited`, `unavailable` или `discontinued`, а также возможную замену. После редактирования отправляется точный выбор пользователя:

```bash
curl --fail --request POST \
  --header "Authorization: Bearer $TOKEN" \
  --header 'Content-Type: application/json' \
  --data '{"items":[
    {"productId":"p01","quantity":2,"selected":true},
    {"productId":"p07","quantity":4,"selected":true},
    {"productId":"p18","quantity":0,"selected":false}
  ]}' \
  http://127.0.0.1:8080/api/orders/ord-2026-002/repeat/commit
```

Backend заново строит preview перед commit, валидирует актуальный остаток, минимальное количество и шаг, затем применяет все изменения корзины одной операцией repository. Если `items` пуст, используются безопасные предложенные количества из preview.

## Чистая архитектура

```text
cmd/server/                    composition root, конфигурация, HTTP lifecycle
internal/domain/               сущности, ошибки и интерфейс Repository
internal/application/          use cases и бизнес-валидация
internal/delivery/httpapi/     HTTP-маршруты, auth, JSON, middleware
internal/repository/memory/    временный демо-адаптер
internal/repository/postgres/  production-адаптер и runner миграций
migrations/                    версионированная схема и seed
web/                           статический frontend
```

Домен и application-слой не зависят от HTTP или PostgreSQL. `cmd/server` выбирает adapter по наличию `DATABASE_URL` и связывает зависимости через интерфейс `domain.Repository`.

Каталог моделируется двумя дополняющими осями: дерево товарных категорий отвечает на вопрос «что это», а цепочка «производитель → бренд → серия → товар» — «кто и в какой линейке это выпустил». Подборки остаются независимыми many-to-many наборами товаров, поэтому навигация не разрастается в дублирующее дерево.

## Проверки

```bash
make check       # gofmt-check + go vet + go test
make test-race   # детектор гонок
make build       # статический бинарник bin/sweetshop
make docker-build
make compose-config
```

Тесты покрывают вход и сессии, поиск/сортировку, ограничения корзины, повтор заказа, HTTP-контракты и защиту SQL-сортировки. Для smoke-проверки запущенного стека используйте `/api/health`, затем логин и защищённый `/api/cart`.

## Production checklist

- замените демонстрационный пароль PostgreSQL и при необходимости удалите/заблокируйте `demo@sugar.ru` отдельной production-миграцией;
- передавайте секреты через secret manager платформы, не коммитьте `.env`;
- завершайте TLS на reverse proxy/load balancer, задайте ограничения request body и rate limits на внешнем контуре;
- не публикуйте порт PostgreSQL наружу; loopback-bind в Compose предназначен только для администрирования одного хоста;
- включите регулярные backup/PITR, мониторинг пула, диска и latency, а JSON-логи направьте в централизованное хранилище;
- запускайте миграции одним release-job/экземпляром, после чего выставляйте `RUN_MIGRATIONS=false` на остальных репликах;
- используйте PostgreSQL для нескольких реплик: memory mode неперсистентен и изолирован внутри процесса;
- проверяйте `/api/health` readiness-пробой и давайте процессу до 10 секунд на graceful shutdown;
- образ уже запускается без root, а Compose дополнительно включает read-only root filesystem, `no-new-privileges` и удаляет Linux capabilities.
