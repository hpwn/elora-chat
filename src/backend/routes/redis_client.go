package routes

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// RedisClient interface defines the Redis operations we need
type RedisClient interface {
	XAdd(ctx context.Context, a *redis.XAddArgs) *redis.StringCmd
	XRead(ctx context.Context, a *redis.XReadArgs) *redis.XStreamSliceCmd
	XRevRangeN(ctx context.Context, stream, end, start string, count int64) *redis.XMessageSliceCmd
	Ping(ctx context.Context) *redis.StatusCmd
}
