package cache

import (
	"context"
	"fmt"
	"github.com/redis/go-redis/v9"
)

func NewRedisClient(host, port, password string) (*redis.Client, error) {
	addr := fmt.Sprintf("%s:%s", host, port)
	rbd := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       0,
	})

	if err := rbd.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("could not connect to redis: %w", err)
	}

	fmt.Println("Connected to Redis")
	return rbd, nil
}
