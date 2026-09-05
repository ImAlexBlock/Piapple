package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/config"
	"github.com/ImAlexBlock/Piapple/internal/provider"
	"github.com/ImAlexBlock/Piapple/internal/session"
	"github.com/ImAlexBlock/Piapple/internal/tools"
	"github.com/ImAlexBlock/Piapple/internal/tui"
)

func main() {
	var cfg config.Config
	flag.StringVar(&cfg.Provider, "provider", "", "provider: openai, anthropic, or google")
	flag.StringVar(&cfg.Model, "model", "", "model ID")
	flag.StringVar(&cfg.BaseURL, "base-url", "", "provider base URL")
	flag.StringVar(&cfg.APIKey, "api-key", "", "API key (defaults to provider environment variable)")
	flag.StringVar(&cfg.SystemPrompt, "system", "You are Piapple, a concise expert coding assistant. Inspect before editing and explain completed work.", "system prompt")
	flag.IntVar(&cfg.MaxSteps, "max-steps", 12, "maximum model/tool rounds")
	flag.StringVar(&cfg.Workdir, "C", ".", "working directory")
	flag.StringVar(&cfg.SessionPath, "session", "", "JSONL session path")
	flag.Parse()
	var err error
	cfg.Workdir, err = filepath.Abs(cfg.Workdir)
	if err != nil {
		fatal(err.Error())
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = config.DefaultBaseURL(cfg.Provider)
	}
	if cfg.APIKey == "" {
		cfg.APIKey = config.APIKeyFromEnvironment(cfg.Provider)
	}
	if cfg.SessionPath != "" && !filepath.IsAbs(cfg.SessionPath) {
		cfg.SessionPath = filepath.Join(cfg.Workdir, cfg.SessionPath)
	}
	var p agent.Provider
	if cfg.Model != "" && cfg.APIKey != "" {
		p, err = provider.New(cfg.Provider, provider.Config{Model: cfg.Model, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, SystemPrompt: cfg.SystemPrompt})
		if err != nil {
			fatal(err.Error())
		}
	}
	builtins := tools.Builtins{Workdir: cfg.Workdir}
	loop := agent.NewLoop(p, builtins.All(), cfg.MaxSteps, func(e agent.Event) {
		if e.Type == "tool_start" {
			fmt.Fprintln(os.Stderr, "[tool]", e.Detail)
		}
	})
	transcript, err := session.Load(cfg.SessionPath)
	if err != nil {
		fatal(err.Error())
	}
	prompt := strings.TrimSpace(strings.Join(flag.Args(), " "))
	if prompt != "" {
		if p == nil {
			fatal(startupConfigurationError(cfg))
		}
		before := len(transcript)
		transcript = append(transcript, agent.Message{Role: "user", Content: prompt})
		updated, answer, err := loop.Run(context.Background(), transcript)
		if appendErr := session.Append(cfg.SessionPath, updated[before:]); appendErr != nil {
			fatal(appendErr.Error())
		}
		if err != nil {
			fatal(err.Error())
		}
		fmt.Println(answer)
		return
	}
	notice := startupNotice(cfg)
	runner := tui.Runner{Loop: loop, In: os.Stdin, Out: os.Stdout, Transcript: transcript, Notice: notice, Persist: func(messages []agent.Message) error { return session.Append(cfg.SessionPath, messages) }}
	if err := runner.Run(context.Background()); err != nil {
		fatal(err.Error())
	}
}
func startupConfigurationError(cfg config.Config) string {
	if cfg.Provider == "" || cfg.Model == "" {
		return "No model selected. Start with -provider <provider> -model <model-id>."
	}
	return fmt.Sprintf("No API key found for %s. Set the provider environment variable or pass -api-key.", cfg.Provider)
}

func startupNotice(cfg config.Config) string {
	if cfg.Provider == "" || cfg.Model == "" {
		return "No model selected. Use -provider <provider> -model <model-id> to start a model session."
	}
	if cfg.APIKey == "" {
		return fmt.Sprintf("No API key found for %s. Set its environment variable or pass -api-key, then restart Piapple.", cfg.Provider)
	}
	return ""
}

func fatal(message string) { fmt.Fprintln(os.Stderr, "piapple:", message); os.Exit(1) }
