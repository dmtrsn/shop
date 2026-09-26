# Слой — интернет-магазин кондитерских изделий

Полноценное демонстрационное приложение: современный адаптивный storefront, каталог с быстрым поиском и фасетами, иерархия «производитель → бренд → серия → товар», подборки, избранное, корзина для больших заказов и предпросмотр и подтверждение повторного заказа. Backend написан на Go в стиле чистой архитектуры; рабочее хранилище — PostgreSQL, а для быстрого локального просмотра есть in-memory режим.

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

### Изолированная будущая витрина

Первая frontend-основа находится в `apps/storefront` и запускается отдельно от Go-приложения.
Для неё нужен Node.js 22.12 или новее. Установка и запуск:

```bash
cd apps/storefront
npm ci
npm run dev
```

Новая витрина будет доступна на <http://127.0.0.1:3000>. Она не забирает production-маршрут
`/`: действующий storefront по-прежнему обслуживается Go из `web/` на
<http://127.0.0.1:8080>. Первый подключённый read-only маршрут —
`/products/:slug`; остальные ссылки новой шапки указывают на утверждённые будущие
канонические маршруты и до следующих миграционных срезов могут вести на страницу 404.

Чтобы запустить Go API и Next storefront вместе, откройте два терминала из корня репозитория:

```bash
# Терминал 1: действующий Go API с временными демоданными
make run-memory

# Терминал 2: изолированная Next-витрина
cd apps/storefront
cp .env.example .env.local
npm ci
npm run dev
```

После этого реальный демотовар доступен, например, по адресу
<http://127.0.0.1:3000/products/dark-chocolate-sea-salt>. Go остаётся источником названия,
цен, наличия, правил количества, описания, атрибутов и изображений. Next также переписывает
текущие относительные `/assets/*` на настроенный Go origin, чтобы существующие изображения
работали в изолированной локальной разработке.

`STOREFRONT_API_BASE_URL` — server-side origin Go API. В локальной development-среде без
переменной используется `http://127.0.0.1:8080`. Для product API-запроса в production
переменная обязательна: без неё server-side transport возвращает понятную ошибку и никогда не
обращается к localhost. Сама production-сборка не требует доступного или настроенного backend;
если переменная при сборке отсутствует, `/assets/*` rewrite на localhost не создаётся. Пример
локальной настройки находится в `apps/storefront/.env.example`. Переменная намеренно не имеет
префикса `NEXT_PUBLIC_` и не предназначена для browser-кода. При изменении значения
перезапустите Next dev server.

Структура текущего frontend-среза:

```text
apps/storefront/
  src/app/                 App Router, layout, структурная главная и product route
  src/components/ui/       доступные базовые примитивы
  src/components/storefront/  композиционные элементы витрины
  src/lib/api/             server-side конфигурация, transport и проверка DTO
  src/styles/              семантические tokens и небольшой global foundation
  src/test/                общая настройка компонентных тестов
```

Токены охватывают тёплые нейтральные цвета, типографику, интервалы, радиусы, границы,
ограниченную elevation, motion, responsive-контейнеры, focus, density и z-index. Реализованы
`Button`, `IconButton`, `TextInput`, `SearchInput`, `Badge`, `QuantityControl`, `Container`,
визуально скрытый текст и индикатор загрузки. Сторонняя UI-библиотека не используется.

Проверки frontend:

```bash
cd apps/storefront
npm run typecheck
npm run lint
npm test
npm run build
```

Product route использует текущий `GET /api/products/{product}`, который принимает ID, slug или
регистронезависимый SKU. Канонический Next URL использует возвращённый API slug; обращение по
ID/SKU перенаправляется на него. Запрос выполняется на сервере с `cache: no-store`, потому что
цена и остаток изменяемы. Ответ проверяется на typed boundary; 404 открывает отдельное
not-found состояние, а сетевой/серверный сбой — восстанавливаемое состояние с повтором.

Страница намеренно read-only: количество — только локальное состояние, кнопки корзины и
избранного явно отключены, API мутаций не вызываются. Разделы главной остаются нейтральными
заполнителями. Также пока не реализованы полный каталог, реальный поиск, сессия, аккаунт,
избранное, корзина, checkout, заказы, администрирование и production route ownership.

### In-memory demo mode

```bash
make run-memory
```

Или явно:

```bash
APP_MODE=demo DATABASE_URL= \
WEB_DIR=web \
MIGRATIONS_DIR=migrations \
go run ./cmd/server
```

Memory repository включается при пустом `DATABASE_URL` только с явным `APP_MODE=demo` или `APP_MODE=development`. По умолчанию действует `APP_MODE=production`: без PostgreSQL DSN сервер завершает запуск с ошибкой. Это удобный режим для интерфейса, API и тестирования сценариев, но все корзины, сессии и заказы исчезают при перезапуске процесса. Для нескольких реплик и любых сохраняемых данных он не подходит.

### PostgreSQL mode

Можно поднять только базу из Compose и запустить Go-процесс на хосте:

```bash
cp .env.example .env
docker compose up -d postgres

DATABASE_URL='postgres://sweetshop:LOCAL_PASSWORD@127.0.0.1:5432/sweetshop?sslmode=disable' \
RUN_MIGRATIONS=true \
WEB_DIR=web \
MIGRATIONS_DIR=migrations \
make run-postgres
```

Замените `LOCAL_PASSWORD` значением из вашего локального `.env`. Для подключения с хоста используйте `POSTGRES_PORT` (в `.env.example` и примере выше — 5432), а внутри Compose — `postgres:5432`.

При первом старте приложение создаст `schema_migrations`, применит все `*.up.sql` по имени и загрузит демоданные. Каждая миграция выполняется транзакционно и повторно не применяется. Для нескольких production-реплик миграции следует запускать ровно одним экземпляром приложения, а на остальных задавать `RUN_MIGRATIONS=false`.

## Конфигурация backend

Сервер читает переменные окружения напрямую; `.env` автоматически обрабатывает Docker Compose, но не команда `go run`.

| Переменная | Значение по умолчанию | Назначение |
|---|---:|---|
| `APP_MODE` | `production` | `production`, `development` или `demo`. Только два последних допускают memory repository без DSN. |
| `DATABASE_URL` | пусто | PostgreSQL DSN. Обязателен в production; ошибочный или недоступный DSN никогда не вызывает переход в memory. |
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

Машиночитаемый контракт: [api/openapi.yaml](api/openapi.yaml). Ограничения, отличия адаптеров и результаты baseline-проверок: [docs/baseline.md](docs/baseline.md).

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

Backend заново строит preview перед commit, валидирует актуальный остаток, минимальное количество и шаг, затем применяет все изменения корзины одной операцией repository. Если `items` отсутствует, равен `null` или пуст, используются предложенные количества из нового preview. Это текущий legacy-контракт: обязательного предварительного review, фиксации цены и идемпотентности нет.

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

Домен и application-слой не зависят от HTTP или PostgreSQL. `cmd/server` проверяет `APP_MODE`, выбирает adapter по наличию `DATABASE_URL` и связывает зависимости через интерфейс `domain.Repository`.

Каталог моделируется двумя дополняющими осями: дерево товарных категорий отвечает на вопрос «что это», а цепочка «производитель → бренд → серия → товар» — «кто и в какой линейке это выпустил». Подборки остаются независимыми many-to-many наборами товаров, поэтому навигация не разрастается в дублирующее дерево.

## Проверки

```bash
make check       # gofmt + vet + unit/OpenAPI tests + JS syntax
make test-race   # детектор гонок
make test-integration # отдельный временный PostgreSQL, миграции и конкурентные транзакции
make check-api   # OpenAPI + HTTP-контракт
make check-js    # синтаксис legacy JavaScript
make build       # статический бинарник bin/sweetshop
make docker-build
make compose-config
```

Тесты покрывают вход и сессии, поиск/сортировку, ограничения корзины, повтор заказа, HTTP-контракты и защиту SQL-сортировки. Для smoke-проверки запущенного стека используйте `/api/health`, затем логин и защищённый `/api/cart`.

## Production checklist

`APP_MODE=production` закрывает только опасный fallback без базы. Миграция `002` по-прежнему добавляет демоаккаунт даже в production; этот этап не делает приложение готовым к публичным продажам. Если отслеживаемый ранее `.env` использовался в развёртывании, соответствующие учётные данные необходимо ротировать. Удаление из индекса не удаляет секреты из истории Git.

- замените демонстрационный пароль PostgreSQL и при необходимости удалите/заблокируйте `demo@sugar.ru` отдельной production-миграцией;
- передавайте секреты через secret manager платформы, не коммитьте `.env`;
- завершайте TLS на reverse proxy/load balancer, задайте ограничения request body и rate limits на внешнем контуре;
- не публикуйте порт PostgreSQL наружу; loopback-bind в Compose предназначен только для администрирования одного хоста;
- включите регулярные backup/PITR, мониторинг пула, диска и latency, а JSON-логи направьте в централизованное хранилище;
- запускайте миграции одним release-job/экземпляром, после чего выставляйте `RUN_MIGRATIONS=false` на остальных репликах;
- используйте PostgreSQL для нескольких реплик: memory mode неперсистентен и изолирован внутри процесса;
- проверяйте `/api/health` readiness-пробой и давайте процессу до 10 секунд на graceful shutdown;
- образ уже запускается без root, а Compose дополнительно включает read-only root filesystem, `no-new-privileges` и удаляет Linux capabilities.
