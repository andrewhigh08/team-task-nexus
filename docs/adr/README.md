# Architecture decision records

Short records of the decisions that shape this service. One file per decision. An accepted record is
revised in place only so that it keeps describing the code accurately; when the decision itself
changes, a new numbered record supersedes the old one.

| # | Decision |
|---|----------|
| [0001](0001-ports-and-adapters-with-sqlx.md) | Ports and adapters on sqlx, transactions carried in context |
| [0002](0002-team-scoped-authorization.md) | Team membership decides visibility, roles decide actions |
| [0003](0003-versioned-task-list-cache.md) | Task list cache invalidated through per-team versions |
| [0004](0004-rate-limiting-in-lua.md) | Sliding-window rate limiting in one Lua script, failing open |
| [0005](0005-transactional-outbox.md) | Notifications delivered through a transactional outbox |
| [0006](0006-chi-and-sqlx.md) | chi and sqlx on MySQL instead of a framework and an ORM |
