-- Sliding-window log rate limiter. KEYS[1] is a sorted set with one member per request
-- admitted within the window, scored by its arrival time in microseconds. The time is taken
-- from the Redis server, so every replica of the service counts against the same clock.
--
-- ARGV[1]: number of requests admitted per window
-- ARGV[2]: window in milliseconds
-- ARGV[3]: random token that keeps two members of the same microsecond apart
--
-- Returns {allowed (1 or 0), remaining requests, milliseconds until a request fits again}.

local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window_ms = tonumber(ARGV[2])
local window = window_ms * 1000

local time = redis.call('TIME')
local now = tonumber(time[1]) * 1000000 + tonumber(time[2])

redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
local count = redis.call('ZCARD', key)

if count < limit then
  -- %d keeps every digit of the timestamp; Lua's own number formatting would round it.
  redis.call('ZADD', key, now, string.format('%d-%s', now, ARGV[3]))
  redis.call('PEXPIRE', key, window_ms)
  return {1, limit - count - 1, 0}
end

-- A request fits once enough entries have left the window to bring the count below the
-- limit. Unless the limit was lowered while the window was full, that is the oldest entry.
local freeing = redis.call('ZRANGE', key, count - limit, count - limit, 'WITHSCORES')
return {0, 0, math.ceil((tonumber(freeing[2]) + window - now) / 1000)}
