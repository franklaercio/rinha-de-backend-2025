package redis

import (
	"context"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type client struct {
	rdb *goredis.Client
}

func NewRedisClient(host, port, password string) (RedisClient, error) {
	addr := fmt.Sprintf("%s:%s", host, port)
	rdb := goredis.NewClient(&goredis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	if err := rdb.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("could not connect to redis: %w", err)
	}

	log.Printf("[INFO] Successfully connected to Redis.")
	return &client{rdb: rdb}, nil
}

func (c *client) BRPop(ctx context.Context, timeout time.Duration, keys ...string) ([]string, error) {
	return c.rdb.BRPop(ctx, timeout, keys...).Result()
}

func (c *client) LPush(ctx context.Context, key string, values ...interface{}) error {
	return c.rdb.LPush(ctx, key, values...).Err()
}
