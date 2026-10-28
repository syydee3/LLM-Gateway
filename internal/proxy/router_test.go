package proxy

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

type stubProvider struct {
	name       string
	shouldFail atomic.Bool
	calls      atomic.Int32
}

func (s *stubProvider) Name() string { return s.name }

func (s *stubProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	s.calls.Add(1)
	if s.shouldFail.Load() {
		return nil, errors.New("provider internal error")
	}
	return &domain.ChatResponse{Provider: s.name, Model: req.Model}, nil
}

func (s *stubProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamChunk, error) {
	return nil, errors.New("not implemented")
}

func TestRouter_CircuitBreakerAndFallback(t *testing.T) {
	primary := &stubProvider{name: "primary"}
	primary.shouldFail.Store(true)

	fallback := &stubProvider{name: "fallback"}

	routes := map[string][]*TargetState{
		"smart-tier": {
			{
				Provider:       primary,
				ModelName:      "m1",
				Timeout:        time.Second,
				MaxFailures:    2,
				CooldownPeriod: 100 * time.Millisecond,
			},
			{
				Provider:       fallback,
				ModelName:      "m2",
				Timeout:        time.Second,
				MaxFailures:    5,
				CooldownPeriod: time.Second,
			},
		},
	}

	router := NewRouter(routes)
	req := domain.ChatRequest{Model: "smart-tier"}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := router.RouteChat(context.Background(), req)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if resp.Provider != "fallback" {
				t.Errorf("expected response from fallback, got %s", resp.Provider)
			}
		}()
	}
	wg.Wait()

	callsBefore := primary.calls.Load()
	for i := 0; i < 5; i++ {
		_, _ = router.RouteChat(context.Background(), req)
	}
	if primary.calls.Load() != callsBefore {
		t.Errorf("expected primary provider to be skipped when circuit is OPEN")
	}

	time.Sleep(150 * time.Millisecond)
	primary.shouldFail.Store(false)

	resp, err := router.RouteChat(context.Background(), req)
	if err != nil || resp.Provider != "primary" {
		t.Errorf("expected primary provider to recover after cooldown, got %v (err: %v)", resp, err)
	}
}
