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
	flag.StringVar(&cfg.Provider, "provider", "openai", "provider: openai, anthropic, or google")
	flag.StringVar(&cfg.Model, "model", "", "model ID")
	flag.StringVar(&cfg.BaseURL, "base-url", "", "provider base URL")
	flag.StringVar(&cfg.APIKey, "api-key", "", "API key (defaults to provider environment variable)")
	flag.StringVar(&cfg.SystemPrompt, "system", "You are Piapple, a concise expert coding assistant. Inspect before editing and explain completed work.", "system prompt")
	flag.IntVar(&cfg.MaxSteps, "max-steps", 12, "maximum model/tool rounds")
	flag.StringVar(&cfg.Workdir, "C", ".", "working directory")
	flag.StringVar(&cfg.SessionPath, "session", "", "JSONL session path")
	flag.Parse()
	if cfg.Model == "" {
		fatal("-model is required")
	}
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
	if cfg.APIKey == "" {
		fatal("API key is required: pass -api-key or configure provider environment variable")
	}
	if cfg.SessionPath != "" && !filepath.IsAbs(cfg.SessionPath) {
		cfg.SessionPath = filepath.Join(cfg.Workdir, cfg.SessionPath)
	}
	p, err := provider.New(cfg.Provider, provider.Config{Model: cfg.Model, BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, SystemPrompt: cfg.SystemPrompt})
	if err != nil {
		fatal(err.Error())
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
	runner := tui.Runner{Loop: loop, In: os.Stdin, Out: os.Stdout, Transcript: transcript, Persist: func(messages []agent.Message) error { return session.Append(cfg.SessionPath, messages) }}
	if err := runner.Run(context.Background()); err != nil {
		fatal(err.Error())
	}
}
func fatal(message string) { fmt.Fprintln(os.Stderr, "piapple:", message); os.Exit(1) }
