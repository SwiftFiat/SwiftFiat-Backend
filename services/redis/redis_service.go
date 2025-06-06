package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisService struct {
	client *redis.Client
}

type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

func NewRedisService(config *RedisConfig) (*RedisService, error) {
    // Sanitize the Host to remove "redis://" if present
    host := config.Host
    if len(host) > 8 && host[:8] == "redis://" {
        host = host[8:] // Remove the "redis://" prefix
    }

    client := redis.NewClient(&redis.Options{
        Addr:     fmt.Sprintf("%s:%s", host, config.Port),
        Password: config.Password,
        DB:       config.DB,
    })

    // Test the connection
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    _, err := client.Ping(ctx).Result()
    if err != nil {
        return nil, fmt.Errorf("failed to connect to Redis: %v", err)
    }

    return &RedisService{
        client: client,
    }, nil
}

// Set stores a key-value pair with optional expiration
func (r *RedisService) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

// Get retrieves a value by key
func (r *RedisService) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

// Delete removes a key
func (r *RedisService) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

// SetHash stores a hash map
func (r *RedisService) SetHash(ctx context.Context, key string, fields interface{}) error {
	return r.client.HSet(ctx, key, fields).Err()
}

// GetHash retrieves all fields from a hash
func (r *RedisService) GetHash(ctx context.Context, key string) (map[string]string, error) {
	return r.client.HGetAll(ctx, key).Result()
}

// GetHashScan retrieves all fields from a hash and scans it into the passed value
func (r *RedisService) GetHashScan(ctx context.Context, key string, dest interface{}) error {
	return r.client.HGetAll(ctx, key).Scan(dest)
}

// Close closes the Redis connection
func (r *RedisService) Close() error {
	return r.client.Close()
}
