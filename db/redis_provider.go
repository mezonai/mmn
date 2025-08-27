package db

import (
	"context"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/mezonai/mmn/logx"
	"github.com/redis/go-redis/v9"
)

// RedisProvider implements DatabaseProvider for Redis
type RedisProvider struct {
	client *redis.Client
	ctx    context.Context
}

// convertKeyToHumanReadable converts binary keys to human-readable format for Redis
func convertKeyToHumanReadable(key []byte) string {
	keyStr := string(key)

	// Check if this is a blocks key with binary slot number
	if strings.HasPrefix(keyStr, "blocks:") {
		// Extract the binary part (last 8 bytes)
		if len(key) >= len("blocks:")+8 {
			binaryPart := key[len("blocks:"):]
			if len(binaryPart) == 8 {
				// Convert binary to slot number
				slot := binary.BigEndian.Uint64(binaryPart)
				return fmt.Sprintf("blocks:%d", slot)
			}
		}
	}

	// For non-block keys or invalid format, return as string
	return keyStr
}

// NewRedisProvider creates a new Redis provider
func NewRedisProvider(address string) (DatabaseProvider, error) {
	client := redis.NewClient(&redis.Options{
		Addr: address,
		DB:   3, // use default DB
	})

	ctx := context.Background()

	// Test connection
	_, err := client.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &RedisProvider{
		client: client,
		ctx:    ctx,
	}, nil
}

// Get retrieves a value by key
func (p *RedisProvider) Get(key []byte) ([]byte, error) {
	redisKey := convertKeyToHumanReadable(key)
	value, err := p.client.Get(p.ctx, redisKey).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Return nil for not found, consistent with interface
		}
		return nil, err
	}
	return []byte(value), nil
}

// Put stores a key-value pair
func (p *RedisProvider) Put(key, value []byte) error {
	redisKey := convertKeyToHumanReadable(key)
	logx.Info("REDIS", "Put key:", redisKey, "value length:", len(value))
	return p.client.Set(p.ctx, redisKey, value, 0).Err()
}

// Delete removes a key-value pair
func (p *RedisProvider) Delete(key []byte) error {
	redisKey := convertKeyToHumanReadable(key)
	return p.client.Del(p.ctx, redisKey).Err()
}

// Has checks if a key exists
func (p *RedisProvider) Has(key []byte) (bool, error) {
	redisKey := convertKeyToHumanReadable(key)
	count, err := p.client.Exists(p.ctx, redisKey).Result()
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Close closes the database connection
func (p *RedisProvider) Close() error {
	return p.client.Close()
}

// Batch returns a new batch for atomic operations
func (p *RedisProvider) Batch() DatabaseBatch {
	return &RedisBatch{
		client: p.client,
		ctx:    p.ctx,
		pipe:   p.client.Pipeline(),
	}
}

// IteratePrefix implements IterableProvider for Redis using SCAN
func (p *RedisProvider) IteratePrefix(prefix []byte, fn func(key, value []byte) bool) error {
	pattern := string(prefix) + "*"
	var cursor uint64
	for {
		keys, newCursor, err := p.client.Scan(p.ctx, cursor, pattern, 1000).Result()
		if err != nil {
			return err
		}
		cursor = newCursor
		for _, k := range keys {
			val, err := p.client.Get(p.ctx, k).Bytes()
			if err != nil {
				if err == redis.Nil {
					continue
				}
				return err
			}
			if !fn([]byte(k), val) {
				return nil
			}
		}
		if cursor == 0 {
			break
		}
	}
	return nil
}

// RedisBatch implements DatabaseBatch for Redis
type RedisBatch struct {
	client *redis.Client
	ctx    context.Context
	pipe   redis.Pipeliner
}

// Put adds a key-value pair to the batch
func (b *RedisBatch) Put(key, value []byte) {
	redisKey := convertKeyToHumanReadable(key)
	b.pipe.Set(b.ctx, redisKey, value, 0)
}

// Delete adds a deletion to the batch
func (b *RedisBatch) Delete(key []byte) {
	redisKey := convertKeyToHumanReadable(key)
	b.pipe.Del(b.ctx, redisKey)
}

// Write commits all operations in the batch
func (b *RedisBatch) Write() error {
	_, err := b.pipe.Exec(b.ctx)
	return err
}

// Reset clears the batch
func (b *RedisBatch) Reset() {
	b.pipe.Discard()
	b.pipe = b.client.Pipeline()
}

// Close releases batch resources
func (b *RedisBatch) Close() error {
	b.pipe.Discard()
	return nil
}
