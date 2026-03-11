package cache

import (
	"github.com/bengobox/ticketing-service/internal/config"
	"github.com/redis/go-redis/v9"
)

// NewClient creates a new Redis client. Uses REDIS_URL if set, else REDIS_ADDR + REDIS_PASSWORD (uniform with other Go backends).
func NewClient(cfg config.RedisConfig) *redis.Client {
	var opt *redis.Options
	if cfg.URL != "" {
		var err error
		opt, err = redis.ParseURL(cfg.URL)
		if err != nil {
			opt = &redis.Options{Addr: cfg.Addr, Password: cfg.Password, DB: cfg.DB}
		} else if cfg.Password != "" {
			opt.Password = cfg.Password
		}
	} else {
		opt = &redis.Options{
			Addr:     cfg.Addr,
			Password: cfg.Password,
			DB:       cfg.DB,
		}
	}
	return redis.NewClient(opt)
}
