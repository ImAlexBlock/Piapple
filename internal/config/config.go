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
