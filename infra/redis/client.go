package redis

import (
	"context"
	"time"
)

type RedisClient interface {
	BRPop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error)
	LPush(ctx context.Context, key string, values ...interface{}) error
}
