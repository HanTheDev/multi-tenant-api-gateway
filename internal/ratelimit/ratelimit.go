package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RateLimiter struct {
	client *redis.Client
}

func NewRateLimiter(redisURL string) (*RateLimiter, error) {
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, err
	}

	client := redis.NewClient(opt)

	return &RateLimiter{client: client}, nil
}

func (rl *RateLimiter) Allow(ctx context.Context, tenantID int, limit int) (bool, error) {
	key := fmt.Sprintf("ratelimit:tenant:%d:%s", tenantID, time.Now().Format("2006-01-02-15"))

	count, err := rl.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}

	if count == 1 {
		rl.client.Expire(ctx, key, time.Hour)
	}

	return count <= int64(limit), nil
}

func (rl *RateLimiter) Close() error {
	return rl.client.Close()
}
