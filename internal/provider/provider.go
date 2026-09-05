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
	case "openai", "openai-compatible", "xai", "groq", "mistral", "deepseek", "openrouter", "together", "fireworks", "perplexity", "moonshot", "kimi", "zai", "minimax", "siliconflow", "qwen", "dashscope", "github":
		return &openAI{Config: cfg}, nil
	case "anthropic":
		return &anthropic{Config: cfg}, nil
	case "google", "gemini":
		return &google{Config: cfg}, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q (supported: %s)", name, strings.Join(Supported(), ", "))
	}
}

// Supported returns provider channel names that can be selected from the CLI
// or /model. OpenAI-compatible channels share the same request and streaming
// protocol but retain separate endpoints and credential environment variables.
func Supported() []string {
	return []string{"anthropic", "deepseek", "fireworks", "github", "google", "groq", "minimax", "mistral", "moonshot", "openai", "openrouter", "perplexity", "qwen", "siliconflow", "together", "xai", "zai"}
}
