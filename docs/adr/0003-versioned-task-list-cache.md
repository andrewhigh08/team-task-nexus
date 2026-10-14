# 0003. Task list cache invalidated through per-team versions

Status: accepted

## Context

Task lists are read much more often than tasks change, and a list query counts and pages over the
team's tasks. The first revision cached pages under a key built from the filter alone, so a list
without `team_id` was cached under team 0 for whoever asked first and then served to every other
user. It invalidated a team by `SCAN`ning for `tasks:team:<id>:*` and deleting what it found: a walk
over the whole keyspace on every write, and a race with readers, since a page read from MySQL before
the write could be stored after the deletion and served until it expired.

## Decision

- Only lists narrowed to one team (`team_id` given) are cached, and only after the membership check
  (ADR 0002). Every member sees the same tasks of a team, so the key holds no viewer. Lists across
  the caller's teams differ per viewer and always come from MySQL.
- Two kinds of keys:

  ```text
  tasks:team:<id>:ver                                               version counter, no TTL
  tasks:team:<id>:v<ver>:s=<status>:a=<assignee>:p=<page>:n=<size>  JSON page, CACHE_TASK_LIST_TTL
  ```

  The page key is built from the normalized query, with the default page and page size filled in.
- A reader takes the version first, looks up the page under it and, on a miss, queries MySQL and
  stores the result under the version it took before the query.
- After the transaction of a task creation, or of a task update that changed a field, commits, the
  service runs `INCR` on the team's counter. The call ignores the cancellation of the request, since
  the change is committed whether or not the client still waits for the answer.
- A page read before a write commits can therefore only be stored under a version that the write
  retires; readers that come after the `INCR` look under the new version and never see it.
- The cache only saves database work, so it never fails a request. A failed version read bypasses
  the cache, a failed page read falls back to MySQL, a failed store or `INCR` is logged as a warning.
  All of them count as `tasknexus_cache_requests_total{cache="task_list",result="error"}`, next to
  `hit` and `miss`.
- Counters have no TTL; pages do. The compose Redis runs `maxmemory-policy volatile-lru`, which under
  memory pressure evicts only keys with a TTL (cached pages, rate limit windows) and never a counter.
  A counter that disappeared would start over from zero and could meet pages cached under the same
  version number earlier and still alive.

`SCAN` with `DEL` was rejected: its cost grows with the whole keyspace rather than with the team, and
deleting keys cannot stop a reader that is about to store a page read before the write. The version
needs neither a search nor a delete.

## Consequences

- An invalidation is one `INCR`. Retired pages are not deleted; they stay until their TTL ends, so a
  team with frequent writes leaves some dead pages behind, bounded by the TTL and evictable.
- One small counter per team that ever had a task written lives as long as Redis keeps its data.
- When an `INCR` fails, the team's cached pages may be stale for up to `CACHE_TASK_LIST_TTL`. The same
  holds for changes made to MySQL outside the API, and after Redis is restored from an older
  snapshot, which brings back the counters and the pages of that moment.
- Redis must not evict keys without a TTL: an `allkeys-*` policy breaks the argument above. With
  `noeviction`, a full Redis refuses writes, and the cache falls back to MySQL.
- Every combination of status, assignee, page and page size is a page of its own, so the hit rate
  depends on how uniform the clients' queries are.
