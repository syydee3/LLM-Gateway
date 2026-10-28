package domain

import "context"

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream"`
}

type ChatResponse struct {
	ID       string  `json:"id"`
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Message  Message `json:"message"`
}

type StreamChunk struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Content  string `json:"content"`
	Done     bool   `json:"done"`
	Err      error  `json:"-"`
}

type Provider interface {
	Name() string
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error)
}
