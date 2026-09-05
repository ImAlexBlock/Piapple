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
	Thinking     string
	MaxSteps     int
	Workdir      string
	SessionPath  string
}

var providerEnvironmentKeys = map[string][]string{
	"openai":      {"OPENAI_API_KEY"},
	"anthropic":   {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_OAUTH_TOKEN"},
	"google":      {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"gemini":      {"GEMINI_API_KEY", "GOOGLE_API_KEY"},
	"xai":         {"XAI_API_KEY"},
	"groq":        {"GROQ_API_KEY"},
	"mistral":     {"MISTRAL_API_KEY"},
	"deepseek":    {"DEEPSEEK_API_KEY"},
	"openrouter":  {"OPENROUTER_API_KEY"},
	"together":    {"TOGETHER_API_KEY"},
	"fireworks":   {"FIREWORKS_API_KEY"},
	"perplexity":  {"PERPLEXITY_API_KEY"},
	"moonshot":    {"MOONSHOT_API_KEY", "KIMI_API_KEY"},
	"kimi":        {"KIMI_API_KEY", "MOONSHOT_API_KEY"},
	"zai":         {"ZAI_API_KEY", "ZAI_CODING_CN_API_KEY"},
	"minimax":     {"MINIMAX_API_KEY"},
	"siliconflow": {"SILICONFLOW_API_KEY"},
	"qwen":        {"DASHSCOPE_API_KEY", "QWEN_API_KEY", "QWEN_TOKEN_PLAN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"},
	"dashscope":   {"DASHSCOPE_API_KEY", "QWEN_API_KEY", "QWEN_TOKEN_PLAN_API_KEY", "QWEN_TOKEN_PLAN_CN_API_KEY"},
	"github":      {"GITHUB_TOKEN"},
}

// APIKeyEnvironmentNames returns the accepted environment variable names in
// precedence order. It is exported so startup diagnostics can tell users the
// exact variables accepted by a channel without duplicating this table.
func APIKeyEnvironmentNames(provider string) []string {
	keys := providerEnvironmentKeys[strings.ToLower(strings.TrimSpace(provider))]
	return append([]string(nil), keys...)
}

func APIKeyFromEnvironment(provider string) string {
	for _, key := range APIKeyEnvironmentNames(provider) {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
func DefaultBaseURL(provider string) string {
	switch strings.ToLower(provider) {
	case "openai":
		return "https://api.openai.com/v1"
	case "anthropic":
		return "https://api.anthropic.com/v1"
	case "google", "gemini":
		return "https://generativelanguage.googleapis.com/v1beta"
	case "xai":
		return "https://api.x.ai/v1"
	case "groq":
		return "https://api.groq.com/openai/v1"
	case "mistral":
		return "https://api.mistral.ai/v1"
	case "deepseek":
		return "https://api.deepseek.com/v1"
	case "openrouter":
		return "https://openrouter.ai/api/v1"
	case "together":
		return "https://api.together.xyz/v1"
	case "fireworks":
		return "https://api.fireworks.ai/inference/v1"
	case "perplexity":
		return "https://api.perplexity.ai"
	case "moonshot", "kimi":
		return "https://api.moonshot.ai/v1"
	case "zai":
		return "https://api.z.ai/api/paas/v4"
	case "minimax":
		return "https://api.minimax.io/v1"
	case "siliconflow":
		return "https://api.siliconflow.cn/v1"
	case "qwen", "dashscope":
		return "https://dashscope.aliyuncs.com/compatible-mode/v1"
	case "github":
		return "https://models.inference.ai.azure.com"
	default:
		return ""
	}
}
