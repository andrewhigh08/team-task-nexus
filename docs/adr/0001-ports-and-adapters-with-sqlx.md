# 0001. Ports and adapters on sqlx, transactions carried in context

Status: accepted

## Context

The service talks to MySQL and Redis and delivers events over HTTP. Its rules (who may see a team,
how a task update is recorded, when an event is due again) must be testable without any of them.
Several operations span more than one repository call and must be atomic: a task update writes the
task, its history and possibly an outbox event. The first revision already passed a transaction
through the context, but read the task outside the transaction and without a lock, ignored errors
of the history writes and did not roll back when the function panicked; nothing kept a service from
importing an adapter.

## Decision

- `internal/service` depends only on `internal/domain`, `internal/port` and `internal/pkg`.
  `TestLayering` in `internal/service/deps_test.go` parses the imports of every file of the package
  and fails on any package under `internal/adapter` in any file, and on any other package of the module
  outside `domain`, `port` and `internal/pkg` in non-test files (tests may import `test/mocks`).
- Ports are expressed in domain types: no sqlx, go-redis or net/http type crosses them.
  `internal/domain` carries no JSON or SQL tags; adapters map their own row types and DTOs.
- Transactions are a port: `TxManager.WithinTx(ctx, fn)`. The implementation in
  `internal/adapter/mysqlrepo` begins an `*sqlx.Tx`, stores it in the context it passes to `fn`,
  commits when `fn` returns nil and rolls back when it returns an error or panics. The panic keeps
  propagating after the rollback; a failed rollback is joined to the error that caused it.
  Repositories run their statements on the transaction found in the context, on the pool otherwise.
- `WithinTx` is re-entrant: called with a context that already carries a transaction, it runs `fn`
  in that transaction, and the outermost call decides whether it commits.
- Operations that are wrong outside a transaction refuse to run there instead of silently
  auto-committing: `TaskRepository.GetByIDForUpdate` (the row lock would end with the statement) and
  `OutboxRepository.Add` (the event could outlive a rolled-back change) return an internal error.
- Request transactions run at the server's default isolation level (REPEATABLE READ in MySQL). The
  task update relies on the row lock taken by `SELECT ... FOR UPDATE`, not on the isolation level.
  The outbox claim is the one exception: it begins its own transaction at READ COMMITTED, because at
  REPEATABLE READ its locking scan would also lock the gaps of the outbox index, and every INSERT
  into the outbox, made by request transactions that hold the row lock of a task meanwhile, would
  wait for the claim to commit (ADR 0005).
- Wiring lives in `internal/app`; `cmd/api/main.go` loads the configuration, builds the logger,
  handles signals and calls `app.New` and `Run`.

## Consequences

- Services are unit-tested with gomock mocks generated from the ports (`test/mocks`), the SQL
  adapters with go-sqlmock, the Redis adapters with miniredis; `test/integration` runs the real
  adapters against MySQL and Redis containers.
- A repository call outside `WithinTx` is a single auto-committed statement. That is intended for
  reads and single writes; every multi-step operation (a team with its owner, task creation, task
  update, comment creation) is wrapped by its service.
- The transaction travels as a context value. A repository that ran a statement on the pool rather
  than through `getQuerier` would silently step outside the transaction; the integration tests
  (concurrent updates of one task, one outbox event per change) are what catches such a mistake.
- Another storage backend means implementing the ports; services and handlers stay as they are.
