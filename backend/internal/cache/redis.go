package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"coralogix-alert-analyzer/internal/models"

	"github.com/redis/go-redis/v9"
)

const defaultTTL = 30 * time.Minute

// Store wraps a Redis client for caching analysis results.
type Store struct {
	client *redis.Client
	ttl    time.Duration
}

// NewStore connects to Redis at the given address (e.g., "localhost:6379").
func NewStore(addr string) (*Store, error) {
	client := redis.NewClient(&redis.Options{
		Addr: addr,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis connect %s: %w", addr, err)
	}

	log.Printf("connected to Redis at %s", addr)
	return &Store{client: client, ttl: defaultTTL}, nil
}

// Close shuts down the Redis connection.
func (s *Store) Close() error {
	return s.client.Close()
}

func clientKey(client string) string {
	return "analyze:" + client
}

// Get retrieves a cached analysis result for the given client.
// Returns nil if not found or expired.
func (s *Store) Get(ctx context.Context, client string) *models.AnalyzeResponse {
	data, err := s.client.Get(ctx, clientKey(client)).Bytes()
	if err != nil {
		return nil
	}

	var resp models.AnalyzeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		log.Printf("WARN [cache] failed to unmarshal cached data for %s: %v", client, err)
		return nil
	}

	log.Printf("INFO [cache] HIT for client=%s", client)
	return &resp
}

// Set stores an analysis result with TTL.
func (s *Store) Set(ctx context.Context, client string, resp *models.AnalyzeResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		log.Printf("WARN [cache] failed to marshal data for %s: %v", client, err)
		return
	}

	if err := s.client.Set(ctx, clientKey(client), data, s.ttl).Err(); err != nil {
		log.Printf("WARN [cache] failed to set cache for %s: %v", client, err)
		return
	}

	log.Printf("INFO [cache] SET for client=%s (TTL=%s, size=%dKB)", client, s.ttl, len(data)/1024)
}

// Invalidate removes a cached result for the given client.
func (s *Store) Invalidate(ctx context.Context, client string) {
	s.client.Del(ctx, clientKey(client))
	log.Printf("INFO [cache] INVALIDATED client=%s", client)
}

// GetString retrieves a raw string value by key.
func (s *Store) GetString(ctx context.Context, key string) (string, bool) {
	val, err := s.client.Get(ctx, key).Result()
	if err != nil {
		return "", false
	}
	return val, true
}

// SetString stores a raw string value with an explicit TTL.
func (s *Store) SetString(ctx context.Context, key, value string, ttl time.Duration) {
	if err := s.client.Set(ctx, key, value, ttl).Err(); err != nil {
		log.Printf("WARN [cache] SetString %s: %v", key, err)
	}
}
