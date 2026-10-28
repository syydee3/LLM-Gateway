package middleware

import (
	"regexp"

	"github.com/syydee3/LLM-Gateway/internal/domain"
)

type Scrubber struct {
	enabled  bool
	patterns []*regexp.Regexp
}

func NewScrubber(enabled bool, rawPatterns []string) (*Scrubber, error) {
	var compiled []*regexp.Regexp
	for _, p := range rawPatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, err
		}
		compiled = append(compiled, re)
	}
	return &Scrubber{
		enabled:  enabled,
		patterns: compiled,
	}, nil
}

func (s *Scrubber) ScrubRequest(req *domain.ChatRequest) {
	if !s.enabled {
		return
	}

	for i := range req.Messages {
		content := req.Messages[i].Content
		for _, re := range s.patterns {
			content = re.ReplaceAllString(content, "[REDACTED_PII]")
		}
		req.Messages[i].Content = content
	}
}
