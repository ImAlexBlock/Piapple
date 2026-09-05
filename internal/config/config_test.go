package config

import (
	"os"
	"testing"
)

func TestCompatibleProviderDefaults(t *testing.T) {
	cases := map[string]string{"xai": "https://api.x.ai/v1", "groq": "https://api.groq.com/openai/v1", "deepseek": "https://api.deepseek.com/v1", "openrouter": "https://openrouter.ai/api/v1", "qwen": "https://dashscope.aliyuncs.com/compatible-mode/v1"}
	for provider, want := range cases {
		if got := DefaultBaseURL(provider); got != want {
			t.Fatalf("%s: got %q want %q", provider, got, want)
		}
	}
}
func TestProviderEnvironmentAliases(t *testing.T) {
	for provider, env := range map[string]string{"xai": "XAI_API_KEY", "deepseek": "DEEPSEEK_API_KEY", "kimi": "MOONSHOT_API_KEY", "qwen": "DASHSCOPE_API_KEY"} {
		_ = os.Setenv(env, "test-key")
		if got := APIKeyFromEnvironment(provider); got != "test-key" {
			t.Fatalf("%s: got %q", provider, got)
		}
		_ = os.Unsetenv(env)
	}
}
