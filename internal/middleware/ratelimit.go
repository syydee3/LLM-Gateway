package middleware

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"
)

type bucket struct {
	tokens   float64
	lastTime time.Time
}

type RateLimit struct {
	mu      sync.Mutex
	clients map[string]*bucket
	rate    float64
	burst   float64
}

func NewRateLimiter(ctx context.Context, rpm int) *RateLimit {
	rl := &RateLimit{
		clients: make(map[string]*bucket),
		rate:    float64(rpm) / 60.0,
		burst:   float64(rpm),
	}
	go rl.cleanupLoop(ctx)
	return rl
}

func (rl *RateLimit) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.clients[key]
	if !exists {
		rl.clients[key] = &bucket{
			tokens:   rl.burst - 1,
			lastTime: now,
		}
		return true
	}

	elapsed := now.Sub(b.lastTime).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > rl.burst {
		b.tokens = rl.burst
	}
	b.lastTime = now

	if b.tokens >= 1.0 {
		b.tokens -= 1.0
		return true
	}
	return false
}

func (rl *RateLimit) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for k, b := range rl.clients {
				if now.Sub(b.lastTime) > 10*time.Minute {
					delete(rl.clients, k)
				}
			}
			rl.mu.Unlock()
		}
	}
}

func (rl *RateLimit) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-API-Key")
		if key == "" {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				key = r.RemoteAddr
			} else {
				key = host
			}
		}

		if !rl.Allow(key) {
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}
