package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/syydee3/LLM-Gateway/internal/domain"
	"github.com/syydee3/LLM-Gateway/internal/middleware"
)

type Handler struct {
	router   *Router
	scrubber *middleware.Scrubber
}

func NewHandler(router *Router, scrubber *middleware.Scrubber) *Handler {
	return &Handler{router: router, scrubber: scrubber}
}

func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	var req domain.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	h.scrubber.ScrubRequest(&req)

	if req.Stream {
		h.handleStream(w, r, req)
		return
	}

	resp, err := h.router.RouteChat(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request, req domain.ChatRequest) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"error":"streaming not supported"}`, http.StatusInternalServerError)
		return
	}

	ch, cancel, err := h.router.RouteStream(r.Context(), req)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for chunk := range ch {
		if chunk.Err != nil {
			_, _ = fmt.Fprintf(w, "data: {\"error\": %q}\n\n", chunk.Err.Error())
			flusher.Flush()
			return
		}
		if chunk.Done {
			_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		b, err := json.Marshal(chunk)
		if err != nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", string(b))
		flusher.Flush()
	}
}
