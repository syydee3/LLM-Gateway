package config

import (
	"encoding/json"
	"os"
)

type TargetConfig struct {
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	TimeoutMs   int    `json:"timeout_ms"`
	MaxFailures int32  `json:"max_failures"`
	CooldownSec int    `json:"cooldown_sec"`
}

type RouteConfig struct {
	Targets []TargetConfig `json:"targets"`
}

type Config struct {
	Port            int                    `json:"port"`
	ReadTimeoutSec  int                    `json:"read_timeout_sec"`
	WriteTimeoutSec int                    `json:"write_timeout_sec"`
	RateLimitRPM    int                    `json:"rate_limit_rpm"`
	ScrubPII        bool                   `json:"scrub_pii"`
	PIIPatterns     []string               `json:"pii_patterns"`
	VirtualModels   map[string]RouteConfig `json:"virtual_models"`
}

func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := json.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
