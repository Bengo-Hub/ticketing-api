package cache

import (
	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/redis/go-redis/v9"
)

// NewClient creates a new Redis client.
func NewClient(cfg config.RedisConfig) *redis.Client {
	opt, err := redis.ParseURL(cfg.URL)
	if err != nil {
		// Fallback to basic options
		opt = &redis.Options{
			Addr:     "localhost:6379",
			Password: cfg.Password,
			DB:       cfg.DB,
		}
	}

	if cfg.Password != "" {
		opt.Password = cfg.Password
	}
	if cfg.DB != 0 {
		opt.DB = cfg.DB
	}

	return redis.NewClient(opt)
}
