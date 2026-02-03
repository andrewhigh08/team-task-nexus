# Team Task Nexus

REST API сервис для управления командами и задачами, построенный на Go с использованием Clean Architecture.

## Технологический стек

| Компонент | Библиотека |
|-----------|-----------|
| Роутер | [go-chi/chi/v5](https://github.com/go-chi/chi) |
| База данных | [jmoiron/sqlx](https://github.com/jmoiron/sqlx) + MySQL 8.0 |
| Кеш | [redis/go-redis/v9](https://github.com/redis/go-redis) |
| JWT | [golang-jwt/jwt/v5](https://github.com/golang-jwt/jwt) |
| Конфигурация | [spf13/viper](https://github.com/spf13/viper) |
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
cmd/api/main.go                     — точка входа, DI, graceful shutdown
internal/
├── config/                          — конфигурация (Viper, YAML + ENV)
├── domain/                          — сущности, DTO, перечисления
├── port/                            — интерфейсы (границы архитектуры)
├── service/                         — бизнес-логика
├── pkg/apperror/                    — типизированные ошибки приложения
└── adapter/
    ├── http/handler/                — HTTP-обработчики
    ├── http/middleware/             — JWT, rate limit, метрики, логирование
    ├── http/response/              — единый формат ответа API
    ├── repository/mysql/           — sqlx-репозитории
    └── cache/redis/                — кеш задач, rate limiter
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
| POST | `/api/v1/teams/{id}/invite` | Пригласить пользователя (owner/admin) |

### Задачи (требуется JWT, только участники команды)
| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/v1/tasks` | Создать задачу |
| GET | `/api/v1/tasks?team_id=&status=&assignee_id=&page=&page_size=` | Список с фильтрацией и пагинацией |
| PUT | `/api/v1/tasks/{id}` | Обновить задачу (с записью истории) |
| GET | `/api/v1/tasks/{id}/history` | История изменений |

### Комментарии (требуется JWT)
| Метод | Путь | Описание |
|-------|------|----------|
| POST | `/api/v1/tasks/{id}/comments` | Добавить комментарий |
| GET | `/api/v1/tasks/{id}/comments` | Список комментариев |

### Аналитика (требуется JWT)
| Метод | Путь | Описание |
|-------|------|----------|
| GET | `/api/v1/teams/stats` | Статистика по всем командам пользователя (JOIN 3+ таблиц) |
| GET | `/api/v1/teams/{id}/top-contributors` | Топ-3 контрибьютора (оконная функция) |
| GET | `/api/v1/tasks/orphaned-assignees` | Задачи с назначенными не из команды |

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

Требуется запущенный MySQL и Redis.

```bash
# Настроить подключения в configs/config.yaml
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
- **Rate limiting**: скользящее окно на базе Redis, 100 запросов в минуту на пользователя
- **История изменений**: все изменения задач записываются в таблицу `task_history`
- **Circuit breaker**: сервис уведомлений с паттерном circuit breaker
- **Сложные SQL**: JOIN 3+ таблиц с агрегацией, оконные функции (ROW_NUMBER), запрос проверки целостности данных
- **Graceful shutdown**: корректное завершение HTTP-сервера с таймаутом
- **Метрики Prometheus**: счётчики запросов, гистограммы latency, gauge активных соединений
