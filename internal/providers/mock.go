package providers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

type MockProvider struct{}

func NewMockProvider() *MockProvider { return &MockProvider{} }

func (m *MockProvider) Name() string { return "mock" }

func (m *MockProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	lastMsg := ""
	if len(req.Messages) > 0 {
		lastMsg = req.Messages[len(req.Messages)-1].Content
	}
	return &domain.ChatResponse{
		ID:       fmt.Sprintf("mock-%d", time.Now().UnixNano()),
		Provider: m.Name(),
		Model:    req.Model,
		Message: domain.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("[Fallback Mock Response] Обработан текст: %s", lastMsg),
		},
	}, nil
}

func (m *MockProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamChunk, error) {
	out := make(chan domain.StreamChunk)
	lastMsg := "пусто"
	if len(req.Messages) > 0 {
		lastMsg = req.Messages[len(req.Messages)-1].Content
	}

	words := strings.Fields(fmt.Sprintf("[Fallback Stream] Получено сообщение: %s", lastMsg))
	streamID := fmt.Sprintf("mock-stream-%d", time.Now().UnixNano())

	go func() {
		defer close(out)
		for _, w := range words {
			select {
			case <-ctx.Done():
				return
			case <-time.After(120 * time.Millisecond):
				out <- domain.StreamChunk{
					ID:       streamID,
					Provider: m.Name(),
					Model:    req.Model,
					Content:  w + " ",
				}
			}
		}
		out <- domain.StreamChunk{ID: streamID, Provider: m.Name(), Model: req.Model, Done: true}
	}()

	return out, nil
}
