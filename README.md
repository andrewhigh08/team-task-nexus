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
    ├── http/middleware/             — JWT, rate limit, метрики, логирование
    ├── http/response/              — единый формат ответа API
    ├── mysqlrepo/                  — sqlx-репозитории
    └── redisstore/                 — кеш задач, rate limiter
```

## База данных

6 таблиц, 10 внешних ключей:

- **users** — пользователи
- **teams** — команды
- **team_members** — участники команд (роли: owner/admin/member)
- **tasks** — задачи (статусы: todo/in_progress/review/done)
- **task_history** — история изменений задач
- **task_comments** — комментарии к задачам

## API

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
| GET | `/health` | Health check |
| GET | `/metrics` | Метрики Prometheus |

## Быстрый старт

### Docker Compose (рекомендуется)

```bash
make docker-up
```

Поднимет MySQL, Redis, Prometheus и приложение. API доступно на `http://localhost:8080`.

### Локально

Требуется запущенный MySQL и Redis. Конфигурация читается из переменных окружения
(`SERVER_*`, `LOG_*`, `DB_*`, `REDIS_*`, `JWT_SECRET`, `JWT_ISSUER`, `JWT_TTL`, `BCRYPT_COST`,
`RATE_LIMIT_API_PER_MINUTE`, `RATE_LIMIT_AUTH_PER_MINUTE`, `TRUST_PROXY_HEADERS`);
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

- **Кеширование**: списки задач кешируются в Redis с TTL 5 минут, кеш инвалидируется при создании/обновлении задач
- **Rate limiting**: скользящее окно в Redis; каждое решение принимает один Lua-скрипт, а время берётся из Redis (`TIME`), поэтому лимит общий для всех реплик и не превышается при конкурентных запросах. 100 запросов в минуту на пользователя (`RATE_LIMIT_API_PER_MINUTE`) и 10 запросов в минуту на регистрацию и вход с одного IP (`RATE_LIMIT_AUTH_PER_MINUTE`, IPv6 считается по /64). IP клиента берётся из соединения; `X-Forwarded-For`/`X-Real-IP` учитываются только при `TRUST_PROXY_HEADERS=true` (за прокси, который перезаписывает эти заголовки). Ответы содержат `X-RateLimit-Limit` и `X-RateLimit-Remaining`, при отказе ответ 429 с `Retry-After`. Если Redis недоступен, запросы пропускаются (fail open) с предупреждением в логе
- **История изменений**: все изменения задач записываются в таблицу `task_history`
- **Circuit breaker**: сервис уведомлений с паттерном circuit breaker
- **Изоляция команд**: задачи, история и комментарии видны только участникам команды; чужая команда или задача отвечает 404, а не 403
- **Сложные SQL**: коррелированные подзапросы для статистики, CTE с оконной функцией RANK, запрос задач бывших участников
- **Graceful shutdown**: корректное завершение HTTP-сервера с таймаутом
- **Метрики Prometheus**: счётчики запросов, гистограммы latency, gauge активных соединений, решения rate limiter (`tasknexus_rate_limit_decisions_total{scope,result}`)
