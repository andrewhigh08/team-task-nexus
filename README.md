# Team Task Nexus

REST API сервис для управления командами и задачами, построенный на Go с использованием Clean Architecture.

## Технологический стек

| Компонент | Библиотека |
|-----------|-----------|
| Роутер | [go-chi/chi/v5](https://github.com/go-chi/chi) |
| База данных | [jmoiron/sqlx](https://github.com/jmoiron/sqlx) + MySQL 8.0 |
| Кеш | [redis/go-redis/v9](https://github.com/redis/go-redis) |
| JWT | [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) |
| Конфигурация | [ilyakaznacheev/cleanenv](https://github.com/ilyakaznacheev/cleanenv) + [joho/godotenv](https://github.com/joho/godotenv) |
| Метрики | [prometheus/client_golang](https://github.com/prometheus/client_golang) |
| Миграции | [golang-migrate/migrate/v4](https://github.com/golang-migrate/migrate) |
| Тестирование | [testify](https://github.com/stretchr/testify) + [testcontainers-go](https://github.com/testcontainers/testcontainers-go) |
| Хеширование | [golang.org/x/crypto/bcrypt](https://pkg.go.dev/golang.org/x/crypto/bcrypt) |

## Архитектура

```
handler → service → repository
            ↓
          cache (Redis)
```

```
cmd/api/main.go                     — точка входа: конфигурация, логгер, сигналы
api/openapi.yaml                     — контракт API (OpenAPI 3.1), встроен в бинарник
migrations/                          — SQL-миграции, встроены в бинарник и применяются при старте
internal/
├── app/                             — сборка приложения, MySQL/Redis, миграции, HTTP-сервер, graceful shutdown
├── config/                          — конфигурация из переменных окружения (cleanenv, .env)
├── domain/                          — сущности, DTO, перечисления
├── port/                            — интерфейсы (границы архитектуры)
├── service/                         — бизнес-логика
├── pkg/apperror/                    — типизированные ошибки приложения
├── pkg/logger/                      — slog-логгер с request_id/user_id из контекста
├── pkg/metrics/                     — коллекторы Prometheus (префикс tasknexus_)
└── adapter/
    ├── http/handler/                — HTTP-обработчики
    ├── http/middleware/             — request id, recovery, access log, метрики, заголовки, JWT, rate limit
    ├── http/response/              — единый формат ответа API
    ├── mysqlrepo/                  — sqlx-репозитории, transactional outbox
    ├── notify/                     — доставка событий: webhook или лог
    └── redisstore/                 — кеш задач, rate limiter
```

## База данных

7 таблиц, 10 внешних ключей:

- **users** — пользователи
- **teams** — команды
- **team_members** — участники команд (роли: owner/admin/member)
- **tasks** — задачи (статусы: todo/in_progress/review/done)
- **task_history** — история изменений задач
- **task_comments** — комментарии к задачам
- **outbox** — события для доставки внешним системам (transactional outbox)

## API

Полный контракт со схемами запросов, ответов и кодов ошибок — [`api/openapi.yaml`](api/openapi.yaml);
запущенный сервис отдаёт его по `GET /openapi.yaml`.

### Аутентификация
| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/v1/register` | Регистрация |
| POST | `/api/v1/login` | Вход, возвращает JWT |

### Команды (требуется JWT)
| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/v1/teams` | Создать команду |
| GET | `/api/v1/teams` | Список команд пользователя |
| GET | `/api/v1/teams/{id}` | Детали команды |
| GET | `/api/v1/teams/{id}/members` | Участники команды |
| POST | `/api/v1/teams/{id}/members` | Добавить участника (owner/admin; admin — только owner) |
| DELETE | `/api/v1/teams/{id}/members/{user_id}` | Удалить участника или выйти из команды |

### Задачи (требуется JWT, только участники команды)
| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/v1/tasks` | Создать задачу |
| GET | `/api/v1/tasks?team_id=&status=&assignee_id=&page=&page_size=` | Список задач своих команд с фильтрацией и пагинацией |
| GET | `/api/v1/tasks/{id}` | Детали задачи |
| PATCH | `/api/v1/tasks/{id}` | Частичное обновление (с записью истории; `null` очищает assignee_id и due_date) |
| GET | `/api/v1/tasks/{id}/history` | История изменений |

### Комментарии (требуется JWT)
| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/v1/tasks/{id}/comments` | Добавить комментарий |
| GET | `/api/v1/tasks/{id}/comments` | Список комментариев |

### Аналитика (требуется JWT)
| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/v1/teams/stats` | Статистика по всем командам пользователя (коррелированные подзапросы) |
| GET | `/api/v1/teams/{id}/top-contributors` | Топ-3 по выполненным задачам за 30 дней (CTE + RANK) |
| GET | `/api/v1/teams/{id}/orphaned-assignees` | Задачи, назначенные бывшим участникам команды (owner/admin) |

### Системные
| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/health/live` | Liveness: процесс обслуживает запросы |
| GET | `/health/ready` | Readiness: MySQL и Redis отвечают (503, если нет) |
| GET | `/metrics` | Метрики Prometheus |
| GET | `/openapi.yaml` | Контракт API (OpenAPI 3.1) |

## Быстрый старт

### Docker Compose (рекомендуется)

```bash
make docker-up
```

Поднимет MySQL, Redis, Prometheus и приложение. API доступно на `http://localhost:8080`.

### Локально

Требуется запущенный MySQL и Redis. Конфигурация читается из переменных окружения
(`SERVER_*`, `LOG_*`, `DB_*`, `REDIS_*`, `JWT_SECRET`, `JWT_ISSUER`, `JWT_TTL`, `BCRYPT_COST`,
`RATE_LIMIT_API_PER_MINUTE`, `RATE_LIMIT_AUTH_PER_MINUTE`, `TRUST_PROXY_HEADERS`, `CACHE_TASK_LIST_TTL`,
`OUTBOX_POLL_INTERVAL`, `OUTBOX_BATCH_SIZE`, `OUTBOX_MAX_ATTEMPTS`, `OUTBOX_LEASE`, `NOTIFY_WEBHOOK_URL`,
`NOTIFY_WEBHOOK_TIMEOUT`);
файл `.env` в рабочем каталоге подхватывается, если есть, но не перекрывает уже заданные переменные.
Обязателен только `JWT_SECRET` (не короче 32 байт), остальное имеет значения по умолчанию.
Миграции применяются при старте.

```bash
export JWT_SECRET=local-development-only-jwt-secret-do-not-deploy
export DB_PASSWORD=apppassword   # пароль пользователя app из docker-compose.yml
make build
make run
```

## Примеры запросов

```bash
# Регистрация
curl -X POST http://localhost:8080/api/v1/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"secret123","full_name":"Иван Иванов"}'

# Вход
curl -X POST http://localhost:8080/api/v1/login \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","password":"secret123"}'

# Создание команды (подставить токен из ответа login)
curl -X POST http://localhost:8080/api/v1/teams \
  -H "Authorization: Bearer <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"name":"Backend Team","description":"Команда бэкенда"}'

# Создание задачи
curl -X POST http://localhost:8080/api/v1/tasks \
  -H "Authorization: Bearer <TOKEN>" \
  -H "Content-Type: application/json" \
  -d '{"title":"Реализовать авторизацию","team_id":1,"priority":3}'

# Список задач с фильтрацией
curl "http://localhost:8080/api/v1/tasks?team_id=1&status=todo&page=1&page_size=10" \
  -H "Authorization: Bearer <TOKEN>"
```

## Тестирование

```bash
# Юнит-тесты
make test

# Покрытие (85%+ на сервисном слое)
make test-coverage

# Интеграционные тесты (требуется Docker)
make test-integration

# Линтер
make lint
```

## Ключевые особенности

- **Кеширование**: списки задач одной команды (`team_id` задан) кешируются в Redis на `CACHE_TASK_LIST_TTL` (по умолчанию 5 минут); списки по всем командам пользователя не кешируются. Ключ страницы содержит номер версии списка команды (`tasks:team:<id>:v<ver>:...`), который хранится в счётчике `tasks:team:<id>:ver`; после коммита создания или изменения задачи счётчик увеличивается одним `INCR`, без поиска ключей через `SCAN`. Версия читается до запроса в MySQL, поэтому страница, прочитанная до параллельной записи, сохраняется под устаревшей версией и не отдаётся. Ошибки Redis не ломают запросы: чтение уходит в MySQL, а при неудачной инвалидации список может быть устаревшим не дольше TTL
- **Rate limiting**: скользящее окно в Redis; каждое решение принимает один Lua-скрипт, а время берётся из Redis (`TIME`), поэтому лимит общий для всех реплик и не превышается при конкурентных запросах. 100 запросов в минуту на пользователя (`RATE_LIMIT_API_PER_MINUTE`) и 10 запросов в минуту на регистрацию и вход с одного IP (`RATE_LIMIT_AUTH_PER_MINUTE`, IPv6 считается по /64). IP клиента берётся из соединения; `X-Forwarded-For`/`X-Real-IP` учитываются только при `TRUST_PROXY_HEADERS=true` (за прокси, который перезаписывает эти заголовки). Ответы содержат `X-RateLimit-Limit` и `X-RateLimit-Remaining`, при отказе ответ 429 с `Retry-After`. Если Redis недоступен, запросы пропускаются (fail open) с предупреждением в логе
- **История изменений**: все изменения задач записываются в таблицу `task_history`
- **Transactional outbox**: назначение задачи (`task.assigned`) и новый комментарий (`task.commented`) записывают событие в таблицу `outbox` в той же транзакции, что и само изменение, поэтому событие существует ровно тогда, когда изменение закоммичено. Диспетчер раз в `OUTBOX_POLL_INTERVAL` забирает до `OUTBOX_BATCH_SIZE` готовых событий (`SELECT ... FOR UPDATE SKIP LOCKED` в транзакции READ COMMITTED, чтобы gap-блокировки не задерживали вставку новых событий), берёт их в аренду на `OUTBOX_LEASE` и доставляет вне транзакции: `POST` JSON `{"id","type","occurred_at","payload"}` на `NOTIFY_WEBHOOK_URL` с заголовками `X-Event-ID`/`X-Event-Type` или, если URL не задан, в лог. Очередная доставка начинается, только если аренда пачки продлится дольше `NOTIFY_WEBHOOK_TIMEOUT`, поэтому другая реплика не заберёт событие, пока оно доставляется; остаток пачки сразу возвращается в очередь, и попытка, засчитанная ему при аренде, отменяется. Неудачная доставка повторяется с экспоненциальной задержкой (1 с, 2 с, 4 с, ... до 5 минут), после `OUTBOX_MAX_ATTEMPTS` попыток событие помечается как failed. Доставка at-least-once: если реплика упала посреди доставки, событие снова станет доступно по истечении аренды, поэтому получатели дедуплицируют по `id`
- **Изоляция команд**: задачи, история и комментарии видны только участникам команды; чужая команда или задача отвечает 404, а не 403
- **Сложные SQL**: коррелированные подзапросы для статистики, CTE с оконной функцией RANK, запрос задач бывших участников
- **Graceful shutdown**: корректное завершение HTTP-сервера с таймаутом; после него останавливается диспетчер outbox, дождавшись текущей доставки; запись её результата и возврат остатка пачки ограничены 5 с каждая, поэтому зависшая БД не затягивает остановку
- **HTTP-слой**: каждый ответ содержит `X-Request-ID` — id клиента, если это 1–64 символа `[A-Za-z0-9._-]`, иначе сгенерированный; он же есть в каждой записи лога запроса. Паника в обработчике логируется со стеком и превращается в 500 `INTERNAL_ERROR`. Access log пишет метод, шаблон маршрута, статус, размер ответа, длительность и пользователя; пробы и `/metrics` — на уровне debug. Ответы несут `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, ответы `/api` — ещё `Cache-Control: no-store`. Тело запроса принимается только как `application/json` (иначе 415), не больше 1 MiB (иначе 413), без неизвестных полей и данных после JSON (иначе 400); неизвестный путь получает 404 `NOT_FOUND` в общем формате ошибки
- **Health checks**: `/health/live` отвечает 200, пока процесс обслуживает запросы, и не проверяет зависимости — недоступность MySQL или Redis не повод перезапускать процесс. `/health/ready` параллельно пингует MySQL и Redis, каждую проверку не дольше 2 с, и при отказе отвечает 503 с именем упавшей проверки: `{"status":"not_ready","checks":{"mysql":"ok","redis":"unavailable"}}`
- **Метрики Prometheus**: HTTP-запросы `tasknexus_http_requests_total{method,route,status}`, их длительность `tasknexus_http_request_duration_seconds{method,route}` и число запросов в обработке `tasknexus_http_requests_in_flight`; `route` — шаблон маршрута (`/api/v1/tasks/{id}`), для путей без маршрута `unmatched`, а нестандартные методы считаются как `other`, поэтому число серий ограничено. Решения rate limiter (`tasknexus_rate_limit_decisions_total{scope,result}`), обращения к кешу (`tasknexus_cache_requests_total{cache,result}`), доставка событий outbox (`tasknexus_outbox_events_total{type,result}`)
