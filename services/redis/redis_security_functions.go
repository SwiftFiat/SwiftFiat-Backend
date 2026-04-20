package redis

// redis_security_functions.go
//
// Extends RedisService with the primitives needed by the session manager and
// rate-limit middleware:
//
//   Sets  : SAdd, SMembers, SRem
//   Sorted sets: ZAdd, ZRemRangeByScore, ZCard  (thin wrappers for pipeline use)
//   TTL   : TTL

import (
	"context"
	"time"
 
	goredis "github.com/redis/go-redis/v9"
)

// ── Sets ──────────────────────────────────────────────────────────────────────
 
// SAdd adds one or more members to a set.
func (r *RedisService) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return r.client.SAdd(ctx, key, members...).Err()
}
 
// SMembers returns all members of a set. Returns nil slice (not an error) when
// the key does not exist.
func (r *RedisService) SMembers(ctx context.Context, key string) ([]string, error) {
	return r.client.SMembers(ctx, key).Result()
}
 
// SRem removes one or more members from a set.
func (r *RedisService) SRem(ctx context.Context, key string, members ...interface{}) error {
	return r.client.SRem(ctx, key, members...).Err()
}

// ── Sorted sets ───────────────────────────────────────────────────────────────
 
// ZAdd adds elements to a sorted set. score is a float64; member is the value.
func (r *RedisService) ZAdd(ctx context.Context, key string, score float64, member interface{}) error {
	return r.client.ZAdd(ctx, key, goredis.Z{Score: score, Member: member}).Err()
}
 
// ZRemRangeByScore removes elements with scores between min and max (inclusive).
// Pass "-inf" / "+inf" strings for unbounded ranges.
func (r *RedisService) ZRemRangeByScore(ctx context.Context, key, min, max string) error {
	return r.client.ZRemRangeByScore(ctx, key, min, max).Err()
}
 
// ZCard returns the number of elements in a sorted set.
func (r *RedisService) ZCard(ctx context.Context, key string) (int64, error) {
	return r.client.ZCard(ctx, key).Result()
}
 
// ── TTL ───────────────────────────────────────────────────────────────────────
 
// TTL returns the remaining time to live for a key.
// Returns -1 if the key has no expiry, -2 if the key does not exist.
func (r *RedisService) TTL(ctx context.Context, key string) (time.Duration, error) {
	return r.client.TTL(ctx, key).Result()
}
 
// ── Pipeline helpers for sorted-set rate limiting ─────────────────────────────
// These return *goredis.IntCmd so callers can read counts after Exec().
 
// PipeZAdd queues a ZADD into an existing pipeliner and returns its command.
func PipeZAdd(pipe goredis.Pipeliner, ctx context.Context, key string, score float64, member interface{}) *goredis.IntCmd {
	return pipe.ZAdd(ctx, key, goredis.Z{Score: score, Member: member})
}
 
// PipeZRemRangeByScore queues a ZREMRANGEBYSCORE into an existing pipeliner.
func PipeZRemRangeByScore(pipe goredis.Pipeliner, ctx context.Context, key, min, max string) *goredis.IntCmd {
	return pipe.ZRemRangeByScore(ctx, key, min, max)
}

// PipeZCard queues a ZCARD into an existing pipeliner and returns its command.
func PipeZCard(pipe goredis.Pipeliner, ctx context.Context, key string) *goredis.IntCmd {
	return pipe.ZCard(ctx, key)
}

// ── Config + health ───────────────────────────────────────────────────────────
 
// ConfigGet retrieves a single Redis config value by name.
// Returns ("", err) if the key doesn't exist or CONFIG GET is disallowed by ACL.
func (r *RedisService) ConfigGet(ctx context.Context, param string) (string, error) {
	result, err := r.client.ConfigGet(ctx, param).Result()
	if err != nil {
		return "", err
	}
	// go-redis v9 returns map[string]string
	if val, ok := result[param]; ok {
		return val, nil
	}
	return "", nil
}
 
// Ping checks Redis liveness. Returns nil on success.
func (r *RedisService) Ping(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}
 
// LPush prepends values to a list.
func (r *RedisService) LPush(ctx context.Context, key string, values ...interface{}) error {
	return r.client.LPush(ctx, key, values...).Err()
}
 
// LTrim trims a list to the specified range.
func (r *RedisService) LTrim(ctx context.Context, key string, start, stop int64) error {
	return r.client.LTrim(ctx, key, start, stop).Err()
}
 
// LRange returns a range of elements from a list.
func (r *RedisService) LRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return r.client.LRange(ctx, key, start, stop).Result()
}