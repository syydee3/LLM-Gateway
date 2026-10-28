package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

type TargetState struct {
	Provider       domain.Provider
	ModelName      string
	Timeout        time.Duration
	MaxFailures    int32
	CooldownPeriod time.Duration

	failures        atomic.Int32
	lastFailureTime atomic.Int64
}

func (t *TargetState) IsAvailable() bool {
	fails := t.failures.Load()
	if fails < t.MaxFailures {
		return true
	}
	lastFail := time.Unix(0, t.lastFailureTime.Load())
	return time.Since(lastFail) > t.CooldownPeriod
}

func (t *TargetState) RecordFailure() {
	f := t.failures.Add(1)
	t.lastFailureTime.Store(time.Now().UnixNano())
	slog.Warn("provider failure recorded", "provider", t.Provider.Name(), "failures", f)
}

func (t *TargetState) RecordSuccess() {
	if t.failures.Load() > 0 {
		t.failures.Store(0)
		slog.Info("provider circuit breaker reset", "provider", t.Provider.Name())
	}
}

type Router struct {
	routes map[string][]*TargetState
}

func NewRouter(routes map[string][]*TargetState) *Router {
	return &Router{routes: routes}
}

func (r *Router) RouteChat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	targets, ok := r.routes[req.Model]
	if !ok || len(targets) == 0 {
		return nil, fmt.Errorf("unknown virtual model: %s", req.Model)
	}

	var lastErr error
	for _, target := range targets {
		if !target.IsAvailable() {
			slog.Debug("skipping unavailable target (circuit open)", "provider", target.Provider.Name())
			continue
		}

		reqCopy := req
		reqCopy.Model = target.ModelName

		reqCtx, cancel := context.WithTimeout(ctx, target.Timeout)
		resp, err := target.Provider.Chat(reqCtx, reqCopy)
		cancel()

		if err == nil {
			target.RecordSuccess()
			return resp, nil
		}

		target.RecordFailure()
		lastErr = err
	}

	return nil, fmt.Errorf("all targets failed, last err: %w", lastErr)
}

func (r *Router) RouteStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamChunk, func(), error) {
	targets, ok := r.routes[req.Model]
	if !ok || len(targets) == 0 {
		return nil, nil, fmt.Errorf("unknown virtual model: %s", req.Model)
	}

	var lastErr error
	for _, target := range targets {
		if !target.IsAvailable() {
			continue
		}

		reqCopy := req
		reqCopy.Model = target.ModelName

		reqCtx, cancel := context.WithTimeout(ctx, target.Timeout)
		ch, err := target.Provider.ChatStream(reqCtx, reqCopy)
		if err == nil {
			target.RecordSuccess()
			return ch, cancel, nil
		}

		cancel()
		target.RecordFailure()
		lastErr = err
	}

	if lastErr == nil {
		lastErr = errors.New("no available targets (all circuits open)")
	}
	return nil, nil, fmt.Errorf("all stream targets failed: %w", lastErr)
}
