package middleware

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

func TestScrubber_ScrubRequest(t *testing.T) {
	patterns := []string{
		`(?i)(api[_-]?key|secret|token)[\s:=]+["']?[a-zA-Z0-9_\-]{16,}["']?`,
		`\b0x[a-fA-F0-9]{40}\b`,
	}

	scrubber, err := NewScrubber(true, patterns)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	req := domain.ChatRequest{
		Messages: []domain.Message{
			{
				Role:    "user",
				Content: "Send 1 ETH to 0x71C7656EC7ab88b098defB751B7401B5f6d8976F using api_key=sk_live_1234567890abcdef1234",
			},
		},
	}

	scrubber.ScrubRequest(&req)

	expected := "Send 1 ETH to [REDACTED_PII] using [REDACTED_PII]"
	if req.Messages[0].Content != expected {
		t.Errorf("got %q, want %q", req.Messages[0].Content, expected)
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rl := NewRateLimiter(ctx, 10)

	const goroutines = 50
	var allowedCount atomic.Int32
	var wg sync.WaitGroup

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			if rl.Allow("client-ip-1") {
				allowedCount.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowedCount.Load(); got != 10 {
		t.Errorf("expected exactly 10 allowed requests, got %d", got)
	}
}

func TestCache_TTLAndConcurrency(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cache := NewCache(ctx, 100*time.Millisecond)
	req := domain.ChatRequest{
		Model:    "smart-tier",
		Messages: []domain.Message{{Role: "user", Content: "Hello"}},
	}
	key := cache.BuildKey(req)

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			cache.Set(key, &domain.ChatResponse{ID: fmt.Sprintf("id-%d", id)})
			_, _ = cache.Get(key)
		}(i)
	}
	wg.Wait()

	if _, ok := cache.Get(key); !ok {
		t.Fatalf("expected cache HIT immediately after Set")
	}

	time.Sleep(150 * time.Millisecond)

	if _, ok := cache.Get(key); ok {
		t.Errorf("expected cache MISS after TTL expiration")
	}
}
