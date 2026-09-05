package config

import (
	"os"
	"strings"
)

type Config struct {
	Provider     string
	Model        string
	BaseURL      string
	APIKey       string
	SystemPrompt string
	MaxSteps     int
	Workdir      string
	SessionPath  string
}

// DefaultModel returns a practical model for each built-in provider. The
// command remains fully configurable with -model, but a fresh installation
// should enter the interactive UI without requiring model flags.
func DefaultModel(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "claude-3-5-haiku-latest"
	case "google", "gemini":
		return "gemini-2.0-flash"
	case "openai", "openai-compatible":
		return "gpt-4o-mini"
	default:
		return "gpt-4o-mini"
	}
}

func APIKeyFromEnvironment(provider string) string {
	keys := map[string]string{
		"openai": "OPENAI_API_KEY", "anthropic": "ANTHROPIC_API_KEY", "google": "GEMINI_API_KEY",
	}
	return os.Getenv(keys[strings.ToLower(provider)])
}

func DefaultBaseURL(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "google":
		return "https://generativelanguage.googleapis.com/v1beta"
	default:
		return ""
	}
}

// EnvironmentVariable names the conventional key variable for a provider.
func EnvironmentVariable(provider string) string {
	switch strings.ToLower(provider) {
	case "anthropic":
		return "ANTHROPIC_API_KEY"
	case "google", "gemini":
		return "GEMINI_API_KEY"
	case "openai", "openai-compatible":
		return "OPENAI_API_KEY"
	default:
		return "<provider API key>"
	}
}
