package provider

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

type Config struct {
	Model, BaseURL, APIKey, SystemPrompt string
	Client                               *http.Client
}

func New(name string, cfg Config) (agent.Provider, error) {
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	switch strings.ToLower(name) {
	case "openai", "openai-compatible":
		return &openAI{Config: cfg}, nil
	case "anthropic":
		return &anthropic{Config: cfg}, nil
	case "google", "gemini":
		return &google{Config: cfg}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q (use openai, anthropic, or google)", name)
	}
}
