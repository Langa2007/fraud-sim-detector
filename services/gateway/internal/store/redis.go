package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStore persists idempotency entries so retries can be recognized across
// gateway processes. Redis keys contain only a digest of the tenant/event key.
type RedisStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewRedis(rawURL, ttl string) (*RedisStore, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse Redis URL: %w", err)
	}
	client := redis.NewClient(options)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	duration, err := time.ParseDuration(ttl)
	if err != nil || duration <= 0 {
		duration = 24 * time.Hour
	}
	return &RedisStore{client: client, ttl: duration}, nil
}

func (s *RedisStore) Get(key string, body []byte) (json.RawMessage, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	value, err := s.client.Get(ctx, s.redisKey(key)).Bytes()
	if err == redis.Nil {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var entry Entry
	if err := json.Unmarshal(value, &entry); err != nil {
		return nil, false, fmt.Errorf("decode Redis idempotency entry: %w", err)
	}
	if entry.BodyHash != Hash(body) {
		return nil, false, ErrConflict
	}
	return entry.Response, true, nil
}

func (s *RedisStore) Put(key string, body, response []byte) error {
	entry := Entry{
		BodyHash: Hash(body),
		Response: append(json.RawMessage(nil), response...),
		Expires:  time.Now().Add(s.ttl),
	}
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	return s.client.Set(ctx, s.redisKey(key), encoded, s.ttl).Err()
}

func (s *RedisStore) Close() error { return s.client.Close() }

func (s *RedisStore) redisKey(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "kinga:idempotency:" + hex.EncodeToString(digest[:])
}
