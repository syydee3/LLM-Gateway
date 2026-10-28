package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/config"
	"github.com/syydee3/LLM-Gateway/internal/domain"
	"github.com/syydee3/LLM-Gateway/internal/middleware"
	"github.com/syydee3/LLM-Gateway/internal/providers"
	"github.com/syydee3/LLM-Gateway/internal/proxy"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	cfg, err := config.Load("config/routes.json")
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	providerRegistry := map[string]domain.Provider{
		"gemini": providers.NewGeminiProvider(os.Getenv("GEMINI_API_KEY")),
		"mock":   providers.NewMockProvider(),
	}

	routes := make(map[string][]*proxy.TargetState)
	for vModel, routeCfg := range cfg.VirtualModels {
		for _, t := range routeCfg.Targets {
			prov, exists := providerRegistry[t.Provider]
			if !exists {
				slog.Warn("unknown provider in config", "provider", t.Provider)
				continue
			}
			routes[vModel] = append(routes[vModel], &proxy.TargetState{
				Provider:       prov,
				ModelName:      t.Model,
				Timeout:        time.Duration(t.TimeoutMs) * time.Millisecond,
				MaxFailures:    t.MaxFailures,
				CooldownPeriod: time.Duration(t.CooldownSec) * time.Second,
			})
		}
	}

	scrubber, err := middleware.NewScrubber(cfg.ScrubPII, cfg.PIIPatterns)
	if err != nil {
		slog.Error("invalid pii regex pattern", "err", err)
		os.Exit(1)
	}

	rateLimiter := middleware.NewRateLimiter(ctx, cfg.RateLimitRPM)
	router := proxy.NewRouter(routes)
	cache := middleware.NewCache(ctx, 5*time.Minute)
	handler := proxy.NewHandler(router, scrubber, cache)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", rateLimiter.Wrap(handler.HandleChatCompletions))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      mux,
		ReadTimeout:  time.Duration(cfg.ReadTimeoutSec) * time.Second,
		WriteTimeout: time.Duration(cfg.WriteTimeoutSec) * time.Second,
	}

	go func() {
		slog.Info("starting LLM Gateway", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("forced shutdown", "err", err)
	}
	slog.Info("server stopped")
}
