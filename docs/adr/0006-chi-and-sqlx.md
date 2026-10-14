# 0006. chi and sqlx on MySQL instead of a framework and an ORM

Status: accepted

## Context

The service was written on chi, sqlx and MySQL. A web framework with an ORM, such as Gin with GORM,
was considered during the rework around ports and adapters (ADR 0001). What the data layer has to
express is mostly SQL that an ORM would only pass through: a row lock (`SELECT ... FOR UPDATE`), a
claim with `FOR UPDATE SKIP LOCKED` in a transaction of its own isolation level, a CTE with the
`RANK()` window function, correlated subqueries for the statistics, multi-row inserts, `IN` lists,
and control over every column an `UPDATE` writes. The HTTP layer needs method and pattern routing,
middleware per route group and the matched route pattern as a metrics label.

## Decision

- chi as the router. Handlers are `http.HandlerFunc` and middleware `func(http.Handler) http.Handler`,
  tested with `net/http/httptest` and nothing else. chi supplies route groups with their own
  middleware, the matched pattern (`RoutePattern`) that labels the metrics and the access log, and
  `chi.Walk`, with which a test compares the served routes to the paths of `api/openapi.yaml`.
- sqlx over `database/sql` with `go-sql-driver/mysql`. SQL is written by hand with explicit column
  lists, never `SELECT *`; rows are scanned into adapter-local structs by `db` tags and converted to
  domain types; multi-row inserts use `NamedExecContext` with a slice and `IN` lists `sqlx.In`.
- MySQL 8.4, which has everything above: `SKIP LOCKED`, CTEs and window functions, and the `CHECK`
  constraint on the task priority.
- Migrations are plain SQL files in `migrations/`, embedded with `go:embed` and applied by
  golang-migrate at startup, over a connection of their own with multi-statements enabled and no I/O
  timeouts, so that a long `ALTER TABLE` is not cut off halfway.
- Request bodies are decoded by `encoding/json` through one helper that checks the content type,
  rejects unknown fields and trailing data and maps the body limit to 413. Validation rules live in
  the services, not in struct tags.

## Consequences

- Every query is visible where it runs, and locking and analytics need no escape hatch.
- Mapping is written by hand: row types, conversions to domain types, DTOs. A new column touches the
  migration, the row type, the column list and the mapping. Where two structs have the same fields,
  a type conversion (`domain.TeamStats(row)`, `cachedTask(task)`) makes the compiler reject a field
  added to one side only.
- SQL is not checked at compile time, as sqlc would do. go-sqlmock tests pin the text and arguments
  of the queries, and the integration suite runs them against MySQL 8.4.
- Migrations run forward only at startup. The down files exist and the integration suite applies
  them one at a time, checking that each restores the schema of the version before it, but rolling
  a deployment back is a manual golang-migrate run.
- Since Go 1.22 the standard mux matches methods and patterns as well; leaving chi would mean
  rebuilding route groups, the route pattern for metrics and the route walk of the contract test.
