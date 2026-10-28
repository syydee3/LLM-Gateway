package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

type GeminiProvider struct {
	apiKey string
	client *http.Client
}

func NewGeminiProvider(apiKey string) *GeminiProvider {
	return &GeminiProvider{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

func (g *GeminiProvider) Name() string { return "gemini" }

type geminiPart struct {
	Text string `json:"text"`
}
type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}
type geminiReq struct {
	Contents []geminiContent `json:"contents"`
}
type geminiResp struct {
	Candidates []struct {
		Content geminiContent `json:"content"`
	} `json:"candidates"`
}

func (g *GeminiProvider) buildPayload(req domain.ChatRequest) ([]byte, error) {
	var contents []geminiContent
	for _, m := range req.Messages {
		role := "user"
		if m.Role == "assistant" || m.Role == "model" {
			role = "model"
		}
		contents = append(contents, geminiContent{
			Role:  role,
			Parts: []geminiPart{{Text: m.Content}},
		})
	}
	return json.Marshal(geminiReq{Contents: contents})
}

func (g *GeminiProvider) Chat(ctx context.Context, req domain.ChatRequest) (*domain.ChatResponse, error) {
	if g.apiKey == "" {
		return nil, errors.New("gemini api key is empty")
	}

	payload, err := g.buildPayload(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", req.Model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("gemini status %d: %s", resp.StatusCode, string(body))
	}

	var gResp geminiResp
	if err := json.NewDecoder(resp.Body).Decode(&gResp); err != nil {
		return nil, err
	}

	text := ""
	if len(gResp.Candidates) > 0 && len(gResp.Candidates[0].Content.Parts) > 0 {
		text = gResp.Candidates[0].Content.Parts[0].Text
	}

	return &domain.ChatResponse{
		ID:       fmt.Sprintf("gem-%d", time.Now().UnixNano()),
		Provider: g.Name(),
		Model:    req.Model,
		Message:  domain.Message{Role: "assistant", Content: text},
	}, nil
}

func (g *GeminiProvider) ChatStream(ctx context.Context, req domain.ChatRequest) (<-chan domain.StreamChunk, error) {
	if g.apiKey == "" {
		return nil, errors.New("gemini api key is empty")
	}

	payload, err := g.buildPayload(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:streamGenerateContent?alt=sse&key=%s", req.Model, g.apiKey)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("gemini stream status %d: %s", resp.StatusCode, string(body))
	}

	out := make(chan domain.StreamChunk)
	go func() {
		defer close(out)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		streamID := fmt.Sprintf("gem-stream-%d", time.Now().UnixNano())

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var gResp geminiResp
			if err := json.Unmarshal([]byte(data), &gResp); err != nil {
				continue
			}
			if len(gResp.Candidates) > 0 && len(gResp.Candidates[0].Content.Parts) > 0 {
				out <- domain.StreamChunk{
					ID:       streamID,
					Provider: g.Name(),
					Model:    req.Model,
					Content:  gResp.Candidates[0].Content.Parts[0].Text,
				}
			}
		}
		out <- domain.StreamChunk{ID: streamID, Provider: g.Name(), Model: req.Model, Done: true}
	}()

	return out, nil
}
