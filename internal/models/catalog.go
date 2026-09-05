// Package models contains Piapple's built-in model catalog and model reference
// parsing. It deliberately describes capabilities without selecting a default;
// the active model still comes from CLI, session, or settings.
package models

import (
	"fmt"
	"sort"
	"strings"
)

type Model struct {
	Provider      string
	ID            string
	Name          string
	ContextWindow int
	Reasoning     bool
}

func (m Model) Ref() string { return m.Provider + "/" + m.ID }

var catalog = []Model{
	{Provider: "openai", ID: "gpt-4o", Name: "GPT-4o", ContextWindow: 128000},
	{Provider: "openai", ID: "gpt-4o-mini", Name: "GPT-4o mini", ContextWindow: 128000},
	{Provider: "openai", ID: "o3-mini", Name: "o3-mini", ContextWindow: 200000, Reasoning: true},
	{Provider: "anthropic", ID: "claude-sonnet-4-5", Name: "Claude Sonnet 4.5", ContextWindow: 200000, Reasoning: true},
	{Provider: "anthropic", ID: "claude-3-7-sonnet-latest", Name: "Claude 3.7 Sonnet", ContextWindow: 200000, Reasoning: true},
	{Provider: "anthropic", ID: "claude-3-5-haiku-latest", Name: "Claude 3.5 Haiku", ContextWindow: 200000},
	{Provider: "google", ID: "gemini-2.5-pro", Name: "Gemini 2.5 Pro", ContextWindow: 1048576, Reasoning: true},
	{Provider: "google", ID: "gemini-2.5-flash", Name: "Gemini 2.5 Flash", ContextWindow: 1048576, Reasoning: true},
	{Provider: "xai", ID: "grok-3", Name: "Grok 3", ContextWindow: 131072},
	{Provider: "groq", ID: "llama-4-scout-17b-16e-instruct", Name: "Llama 4 Scout", ContextWindow: 131072},
	{Provider: "mistral", ID: "mistral-large-latest", Name: "Mistral Large", ContextWindow: 131072},
	{Provider: "deepseek", ID: "deepseek-chat", Name: "DeepSeek Chat", ContextWindow: 65536},
	{Provider: "deepseek", ID: "deepseek-reasoner", Name: "DeepSeek Reasoner", ContextWindow: 65536, Reasoning: true},
	{Provider: "openrouter", ID: "openai/gpt-4o-mini", Name: "OpenAI GPT-4o mini", ContextWindow: 128000},
	{Provider: "openrouter", ID: "anthropic/claude-sonnet-4", Name: "Claude Sonnet", ContextWindow: 200000, Reasoning: true},
	{Provider: "together", ID: "meta-llama/Llama-3.3-70B-Instruct-Turbo", Name: "Llama 3.3 70B", ContextWindow: 131072},
	{Provider: "fireworks", ID: "accounts/fireworks/models/llama-v3p3-70b-instruct", Name: "Llama 3.3 70B", ContextWindow: 131072},
	{Provider: "perplexity", ID: "sonar-pro", Name: "Sonar Pro", ContextWindow: 127072},
}

func Catalog() []Model { return append([]Model(nil), catalog...) }

func ParseRef(value string) (provider, id string, err error) {
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", fmt.Errorf("model must use provider/model format")
	}
	return strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1]), nil
}

func Find(provider, id string) (Model, bool) {
	for _, m := range catalog {
		if strings.EqualFold(m.Provider, provider) && m.ID == id {
			return m, true
		}
	}
	return Model{Provider: provider, ID: id, Name: id}, false
}

func Sort(items []Model) []Model {
	out := append([]Model(nil), items...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	return out
}
