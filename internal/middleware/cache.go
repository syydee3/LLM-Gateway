package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

type cacheEntry struct {
	resp      *domain.ChatResponse
	expiresAt time.Time
}

type Cache struct {
	mu      sync.RWMutex
	storage map[string]cacheEntry
	ttl     time.Duration
}

func NewCache(ctx context.Context, ttl time.Duration) *Cache {
	c := &Cache{
		storage: make(map[string]cacheEntry),
		ttl:     ttl,
	}
	if ttl > 0 {
		go c.cleanupLoop(ctx)
	}
	return c
}

func (c *Cache) BuildKey(req domain.ChatRequest) string {
	b, _ := json.Marshal(struct {
		Model    string           `json:"m"`
		Messages []domain.Message `json:"msg"`
		Temp     float64          `json:"t"`
	}{
		Model:    req.Model,
		Messages: req.Messages,
		Temp:     req.Temperature,
	})
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

func (c *Cache) Get(key string) (*domain.ChatResponse, bool) {
	if c.ttl <= 0 {
		return nil, false
	}

	c.mu.RLock()
	entry, exists := c.storage[key]
	c.mu.RUnlock()

	if !exists || time.Now().After(entry.expiresAt) {
		return nil, false
	}

	copied := *entry.resp
	return &copied, true
}

func (c *Cache) Set(key string, resp *domain.ChatResponse) {
	if c.ttl <= 0 || resp == nil {
		return
	}

	copied := *resp
	c.mu.Lock()
	c.storage[key] = cacheEntry{
		resp:      &copied,
		expiresAt: time.Now().Add(c.ttl),
	}
	c.mu.Unlock()
}

func (c *Cache) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			c.mu.Lock()
			for k, v := range c.storage {
				if now.After(v.expiresAt) {
					delete(c.storage, k)
				}
			}
			c.mu.Unlock()
		}
	}
}
