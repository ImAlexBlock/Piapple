package provider

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

// Config is the provider-neutral request configuration. Provider selects the
// wire adapter and is also persisted on assistant messages; BaseURL, APIKey,
// and Client are intentionally injectable for compatible gateways and tests.
type Config struct {
	Provider, Model, BaseURL, APIKey, SystemPrompt string
	Thinking                                       string
	Client                                         *http.Client
	Headers                                        map[string]string
	MaxRetries                                     int
	RetryBackoff                                   time.Duration
}

type Protocol string

const (
	ProtocolOpenAI    Protocol = "openai-chat-completions"
	ProtocolAnthropic Protocol = "anthropic-messages"
	ProtocolGoogle    Protocol = "google-generate-content"
)

// Channel describes a built-in model channel. It is metadata, not a plugin
// mechanism: all built-in factories are compiled into Piapple.
type Channel struct {
	Name        string
	Protocol    Protocol
	DefaultURL  string
	Environment string
}

type Factory func(Config) (agent.Provider, error)

var (
	registryMu sync.RWMutex
	custom     = map[string]Factory{}
)

var builtInChannels = []Channel{
	{Name: "anthropic", Protocol: ProtocolAnthropic, DefaultURL: "https://api.anthropic.com/v1", Environment: "ANTHROPIC_API_KEY"},
	{Name: "deepseek", Protocol: ProtocolOpenAI, DefaultURL: "https://api.deepseek.com/v1", Environment: "DEEPSEEK_API_KEY"},
	{Name: "fireworks", Protocol: ProtocolOpenAI, DefaultURL: "https://api.fireworks.ai/inference/v1", Environment: "FIREWORKS_API_KEY"},
	{Name: "github", Protocol: ProtocolOpenAI, DefaultURL: "https://models.inference.ai.azure.com", Environment: "GITHUB_TOKEN"},
	{Name: "google", Protocol: ProtocolGoogle, DefaultURL: "https://generativelanguage.googleapis.com/v1beta", Environment: "GEMINI_API_KEY"},
	{Name: "groq", Protocol: ProtocolOpenAI, DefaultURL: "https://api.groq.com/openai/v1", Environment: "GROQ_API_KEY"},
	{Name: "minimax", Protocol: ProtocolOpenAI, DefaultURL: "https://api.minimax.io/v1", Environment: "MINIMAX_API_KEY"},
	{Name: "mistral", Protocol: ProtocolOpenAI, DefaultURL: "https://api.mistral.ai/v1", Environment: "MISTRAL_API_KEY"},
	{Name: "moonshot", Protocol: ProtocolOpenAI, DefaultURL: "https://api.moonshot.ai/v1", Environment: "MOONSHOT_API_KEY"},
	{Name: "openai", Protocol: ProtocolOpenAI, DefaultURL: "https://api.openai.com/v1", Environment: "OPENAI_API_KEY"},
	{Name: "openrouter", Protocol: ProtocolOpenAI, DefaultURL: "https://openrouter.ai/api/v1", Environment: "OPENROUTER_API_KEY"},
	{Name: "perplexity", Protocol: ProtocolOpenAI, DefaultURL: "https://api.perplexity.ai", Environment: "PERPLEXITY_API_KEY"},
	{Name: "qwen", Protocol: ProtocolOpenAI, DefaultURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", Environment: "DASHSCOPE_API_KEY"},
	{Name: "siliconflow", Protocol: ProtocolOpenAI, DefaultURL: "https://api.siliconflow.cn/v1", Environment: "SILICONFLOW_API_KEY"},
	{Name: "together", Protocol: ProtocolOpenAI, DefaultURL: "https://api.together.xyz/v1", Environment: "TOGETHER_API_KEY"},
	{Name: "xai", Protocol: ProtocolOpenAI, DefaultURL: "https://api.x.ai/v1", Environment: "XAI_API_KEY"},
	{Name: "zai", Protocol: ProtocolOpenAI, DefaultURL: "https://api.z.ai/api/paas/v4", Environment: "ZAI_API_KEY"},
}

func normalizeName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	switch name {
	case "gemini":
		return "google"
	case "kimi":
		return "moonshot"
	case "dashscope":
		return "qwen"
	case "openai-compatible":
		return "openai"
	default:
		return name
	}
}

// Register adds a compiled provider factory for applications embedding
// Piapple. It is deliberately a Go API, not a runtime extension/plugin path.
func Register(name string, factory Factory) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || factory == nil {
		return fmt.Errorf("provider name and factory are required")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	custom[name] = factory
	return nil
}

func builtinChannelInfo(name string) (Channel, bool) {
	for _, channel := range builtInChannels {
		if channel.Name == name {
			return channel, true
		}
	}
	return Channel{}, false
}

func ChannelInfo(name string) (Channel, bool) { return builtinChannelInfo(normalizeName(name)) }

func Channels() []Channel {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := append([]Channel(nil), builtInChannels...)
	for name := range custom {
		if _, ok := builtinChannelInfo(normalizeName(name)); !ok {
			out = append(out, Channel{Name: name})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func New(name string, cfg Config) (agent.Provider, error) {
	name = normalizeName(name)
	cfg.Provider = name
	if cfg.BaseURL == "" {
		if info, ok := builtinChannelInfo(name); ok {
			cfg.BaseURL = info.DefaultURL
		}
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if cfg.Client == nil {
		cfg.Client = http.DefaultClient
	}
	registryMu.RLock()
	factory := custom[name]
	registryMu.RUnlock()
	if factory != nil {
		return factory(cfg)
	}
	if info, ok := ChannelInfo(name); ok {
		switch info.Protocol {
		case ProtocolOpenAI:
			return &openAI{Config: cfg}, nil
		case ProtocolAnthropic:
			return &anthropic{Config: cfg}, nil
		case ProtocolGoogle:
			return &google{Config: cfg}, nil
		}
	}
	return nil, fmt.Errorf("unsupported provider %q (supported: %s)", name, strings.Join(Supported(), ", "))
}

func Supported() []string {
	channels := Channels()
	out := make([]string, 0, len(channels))
	for _, channel := range channels {
		out = append(out, channel.Name)
	}
	return out
}

type ThinkingProvider interface{ SetThinking(string) }
