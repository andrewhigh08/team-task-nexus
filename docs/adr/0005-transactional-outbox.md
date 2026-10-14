# 0005. Notifications delivered through a transactional outbox

Status: accepted

## Context

Assigning a task and commenting on it should reach systems outside the service, such as a notifier.
The first revision called a notification service synchronously in the request after the change and
ignored its errors. The call sat behind a "circuit breaker" whose failure counter nothing ever
incremented, and all it did was log a mock email with the assignee's address. Sending from the
request has no good place either way: before the commit it announces changes that may still roll
back, after the commit a crash or a failed send loses the notification, and in both cases a slow
receiver delays the client.

## Decision

- Events are rows of the `outbox` table (migration 000008), written by `OutboxRepository.Add` in the
  transaction of the change they describe: `task.assigned` when a task is created with an assignee or
  an update assigns it to someone, `task.commented` for every new comment. `Add` refuses to run
  outside a transaction. An event exists exactly when its change was committed.
- `service.Dispatcher` runs in every replica next to the HTTP server. Every `OUTBOX_POLL_INTERVAL` it
  claims up to `OUTBOX_BATCH_SIZE` due events in a short transaction of its own at READ COMMITTED
  (ADR 0001):

  ```sql
  SELECT id, event_type, payload, attempts, created_at
  FROM outbox FORCE INDEX (idx_outbox_pending)
  WHERE sent_at IS NULL AND failed_at IS NULL AND next_attempt_at <= UTC_TIMESTAMP(3)
  ORDER BY next_attempt_at, id
  LIMIT ?
  FOR UPDATE SKIP LOCKED
  ```

  followed by `UPDATE ... SET attempts = attempts + 1, next_attempt_at = UTC_TIMESTAMP(3) + <lease>`
  and the commit. `SKIP LOCKED` lets concurrent dispatchers pass over the rows another one is claiming
  instead of waiting for them; the lease, written before the commit, keeps those rows from being
  claimed again once the locks are released. The lease and the `sent_at` and `failed_at` times come
  from the database clock, which every replica shares; the time of a retry is computed by the
  dispatcher.
- Events are claimed in the order they became due, which is the order of the index
  `idx_outbox_pending (sent_at, failed_at, next_attempt_at)` and the `id` it implicitly ends in, so
  the scan stops after the batch. A locking read keeps a lock on every row it reads that matches the
  condition: ordered by `id`, the claim would read, sort and lock the whole due backlog to return one
  batch, and a second dispatcher would find nothing to claim until the first committed.
  `FORCE INDEX` keeps the index scan when most rows of the table are due, where the optimizer would
  otherwise choose a full scan and a sort.
- Delivery happens outside any transaction, one event at a time, each bounded by
  `NOTIFY_WEBHOOK_TIMEOUT`. A delivery starts only while the lease of its batch outlasts it, which is
  why the configuration requires `OUTBOX_LEASE` to exceed the timeout. When the lease runs short or
  the dispatcher is stopping, the rest of the batch is released: the attempt the claim counted is
  taken back and the events are due at once. Events whose lease has already ended are left alone,
  since another dispatcher may hold them by then.
- A delivered event gets `sent_at`. A failed one gets `last_error` (truncated to the column, invalid
  UTF-8 replaced) and is due again after a delay that doubles from 1 s up to 5 min; once it has had
  `OUTBOX_MAX_ATTEMPTS` attempts it gets `failed_at` and is not retried any more. Outcomes are counted
  in `tasknexus_outbox_events_total{type,result}` with `sent`, `retry` and `failed`.
- On shutdown the dispatcher keeps running while the HTTP server drains, so the events of the last
  requests can still go out. Then it is stopped: the delivery in flight runs to its end, the rest of
  the batch is released, and each of these final writes is bounded by 5 seconds.
- Delivery goes through `port.EventSender`:
  - `WebhookSender` (when `NOTIFY_WEBHOOK_URL` is set) POSTs `{"id","type","occurred_at","payload"}`
    with `Content-Type: application/json`, `X-Event-ID` and `X-Event-Type`. Any status outside 2xx,
    redirects included, is a failed delivery. The URL may carry a secret token, so it never appears
    in logs, in errors or in `last_error`.
  - `LogSender` (otherwise) writes the event to the log. Payloads hold ids and the task title, never
    emails.

## Consequences

- Delivery is at least once. A replica that stops between sending an event and recording it leaves
  the event to its lease, after which another dispatcher delivers it again. Consumers deduplicate by
  the event id (`id`, `X-Event-ID`).
- There is no ordering guarantee: an event waiting for its retry is overtaken by later ones, and
  several replicas deliver concurrently.
- The response time of a request no longer depends on the receiver. A slow receiver delays only the
  events behind it in the same batch.
- Each replica polls the outbox once per interval even when there is nothing to send, with an indexed
  query on `(sent_at, failed_at, next_attempt_at)`.
- Sent and failed rows stay in the table; there is no purge job, and an event given up on is sent
  again only after someone resets it by hand.
- The webhook request is not signed. A receiver can only authenticate the sender through a secret in
  the URL or on the network level.
