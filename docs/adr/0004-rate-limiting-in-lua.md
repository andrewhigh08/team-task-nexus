# 0004. Sliding-window rate limiting in one Lua script, failing open

Status: accepted

## Context

The API runs on several replicas behind one Redis, so a limit has to be counted in Redis to hold for
the service as a whole. The first revision limited authenticated users only; registration and login
were open. It sent `ZREMRANGEBYSCORE`, `ZCARD`, `ZADD` and `EXPIRE` in a pipeline, which other
clients' commands can interleave, so concurrent requests could all see a count below the limit. It
scored entries with the application's clock in whole seconds and used that second as the member, so
all requests of one second collapsed into a single entry, and it recorded rejected requests as well.

## Decision

- `port.RateLimiter.Allow(ctx, key, limit, window)` answers allowed, remaining and retry-after. The
  Redis implementation is one Lua script (`internal/adapter/redisstore/ratelimit.lua`), run with
  `EVALSHA` through go-redis, which falls back to `EVAL` when the server does not have the script
  cached.
- Sliding-window log: a sorted set per key, one member per admitted request, scored by its arrival
  time in microseconds. The script takes the time from `redis.call('TIME')`, so every replica counts
  against the same clock; drops entries older than the window; and when the count is below the limit,
  adds a member `<microseconds>-<16 random hex characters>` and sets `PEXPIRE` to the window. The
  random part comes from Go, since two requests can arrive within one microsecond.
- Rejected requests are not recorded, so a client that retries too early does not push its own window
  further out. Retry-after is computed from the entry whose departure brings the count below the
  limit, which is the oldest one unless the limit was lowered while the window was full.
- Two scopes, both over a one-minute window:
  - per user on every authenticated `/api/v1` route: `rl:api:user:<id>`, `RATE_LIMIT_API_PER_MINUTE`,
    applied after authentication;
  - per client address on registration and login: `rl:auth:ip:<address>`,
    `RATE_LIMIT_AUTH_PER_MINUTE`. IPv4-mapped IPv6 addresses count as IPv4; other IPv6 clients are
    keyed by their /64 prefix, since a subscriber usually gets at least a /64 and could otherwise
    rotate addresses freely. A request whose address does not parse shares the key `unknown`.
- The client address is the peer address of the connection. Only with `TRUST_PROXY_HEADERS=true` it
  is the first valid entry of `X-Forwarded-For`, then `X-Real-IP`.
- Responses carry `X-RateLimit-Limit` and, when the limiter answered, `X-RateLimit-Remaining`. A
  rejected request gets 429 `TOO_MANY_REQUESTS` with `Retry-After` in whole seconds, at least 1, and
  the same value in `details.retry_after_seconds`.
- Fail open: when Redis cannot answer, the request proceeds, a warning is logged and
  `tasknexus_rate_limit_decisions_total{scope,result="error"}` is incremented (next to `allowed` and
  `limited`). These limits protect capacity, not secrets: passwords are stored as bcrypt hashes and
  the login limit is a speed bump for guessing, so an outage of Redis must not take the API down.

## Consequences

- The count is exact over a true sliding window, without the double burst a fixed window allows at
  its boundary. The price is memory: up to `limit` members per active key (100 per user at the
  default), each key expiring one window after its last admitted request.
- Every limited request costs one round trip to Redis.
- While Redis is down there is no limit at all; the `error` series of the metric shows it.
- Clients behind one NAT or proxy share a per-address limit. Behind a reverse proxy without
  `TRUST_PROXY_HEADERS`, every client shares the proxy's address; with it, the proxy must overwrite
  `X-Forwarded-For`, or every client can choose the address it is limited under.
- Requests with a missing or invalid token are answered 401 before the per-user limiter and are not
  counted; checking an HS256 signature is cheap.
- There is no per-account lockout: guesses against one account from many addresses are limited only
  per address, and bcrypt slows each of them down.
- Under `volatile-lru` a window, having a TTL, may be evicted under memory pressure, which resets the
  count of that client.
