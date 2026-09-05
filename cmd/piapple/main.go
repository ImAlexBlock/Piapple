package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/auth"
	"github.com/ImAlexBlock/Piapple/internal/config"
	"github.com/ImAlexBlock/Piapple/internal/models"
	"github.com/ImAlexBlock/Piapple/internal/projectcontext"
	"github.com/ImAlexBlock/Piapple/internal/provider"
	"github.com/ImAlexBlock/Piapple/internal/session"
	"github.com/ImAlexBlock/Piapple/internal/settings"
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
	flag.StringVar(&cfg.SessionPath, "session", "", "session JSONL file")
	continueSession := flag.Bool("continue", false, "continue the most recent project session")
	flag.BoolVar(continueSession, "c", false, "continue the most recent project session")
	noSession := flag.Bool("no-session", false, "do not persist session history")
	printMode := flag.Bool("p", false, "print the answer without starting the TUI")
	flag.BoolVar(printMode, "print", false, "print the answer without starting the TUI")
	jsonMode := flag.Bool("json", false, "print the final answer as JSON")
	flag.Parse()
	// Keep raw CLI overrides separate from resolved settings so switching models
	// later does not accidentally reuse an API key or endpoint for another provider.
	cliAPIKey := cfg.APIKey
	cliBaseURL := cfg.BaseURL
	var err error
	cfg.Workdir, err = filepath.Abs(cfg.Workdir)
	if err != nil {
		fatal(err.Error())
	}
	home, homeErr := os.UserHomeDir()
	if homeErr != nil {
		fatal(homeErr.Error())
	}
	projectSettings, settingsErr := settings.Load(settings.ProjectPath(cfg.Workdir))
	if settingsErr != nil {
		fatal(settingsErr.Error())
	}
	userSettings, settingsErr := settings.Load(settings.UserPath(home))
	if settingsErr != nil {
		fatal(settingsErr.Error())
	}
	var cliModel *settings.ModelRef
	if cfg.Provider != "" && cfg.Model != "" {
		cliModel = &settings.ModelRef{Provider: cfg.Provider, ID: cfg.Model}
	}
	if selected := settings.Resolve(cliModel, projectSettings, userSettings); selected != nil {
		cfg.Provider, cfg.Model = selected.Provider, selected.ID
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = config.DefaultBaseURL(cfg.Provider)
	}
	contextFiles, err := projectcontext.Load(cfg.Workdir)
	if err != nil {
		fatal(err.Error())
	}
	cfg.SystemPrompt += projectcontext.Format(contextFiles)
	if cfg.APIKey == "" {
		cfg.APIKey = config.APIKeyFromEnvironment(cfg.Provider)
	}
	if cfg.APIKey == "" && cfg.Provider != "" {
		home, homeErr := os.UserHomeDir()
		if homeErr != nil {
			fatal(homeErr.Error())
		}
		credentials, loadErr := auth.Load(auth.Path(home))
		if loadErr != nil {
			fatal(loadErr.Error())
		}
		cfg.APIKey = credentials.Get(cfg.Provider)
	}
	if cfg.SessionPath != "" && !filepath.IsAbs(cfg.SessionPath) {
		cfg.SessionPath = filepath.Join(cfg.Workdir, cfg.SessionPath)
	}
	var repository *session.Repository
	var sessionDir string
	if !*noSession {
		if cfg.SessionPath != "" {
			repository, err = session.Open(cfg.SessionPath)
		} else {
			sessionDir = session.DefaultDirectory(home, cfg.Workdir)
			if *continueSession {
				repository, err = session.Continue(sessionDir)
			} else {
				repository, err = session.Create(sessionDir, cfg.Workdir)
			}
		}
		if err != nil {
			fatal(fmt.Sprintf("session: %v", err))
		}
	}
	// A resumed Pi session records its active model in the session tree. It has
	// higher precedence than settings, but an explicit CLI model still wins.
	if cliModel == nil && repository != nil {
		if providerID, modelID, ok := repository.Model(); ok {
			cfg.Provider, cfg.Model = providerID, modelID
			if cliBaseURL == "" {
				cfg.BaseURL = config.DefaultBaseURL(providerID)
			}
			cfg.APIKey = cliAPIKey
			if cfg.APIKey == "" {
				cfg.APIKey = config.APIKeyFromEnvironment(providerID)
			}
			if cfg.APIKey == "" {
				credentials, loadErr := auth.Load(auth.Path(home))
				if loadErr != nil {
					fatal(loadErr.Error())
				}
				cfg.APIKey = credentials.Get(providerID)
			}
		}
	}
	initialProvider := cfg.Provider
	createLoop := func(providerID, modelID string) (*agent.Loop, error) {
		key := ""
		if providerID == initialProvider {
			key = cliAPIKey
		}
		if key == "" {
			key = config.APIKeyFromEnvironment(providerID)
		}
		if key == "" {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return nil, homeErr
			}
			credentials, loadErr := auth.Load(auth.Path(home))
			if loadErr != nil {
				return nil, loadErr
			}
			key = credentials.Get(providerID)
		}
		if key == "" {
			return nil, fmt.Errorf("no API key found for %s; use /login %s <api-key>", providerID, providerID)
		}
		baseURL := config.DefaultBaseURL(providerID)
		if providerID == initialProvider && cliBaseURL != "" {
			baseURL = cliBaseURL
		}
		p, createErr := provider.New(providerID, provider.Config{Model: modelID, BaseURL: baseURL, APIKey: key, SystemPrompt: cfg.SystemPrompt})
		if createErr != nil {
			return nil, createErr
		}
		builtins := tools.Builtins{Workdir: cfg.Workdir}
		return agent.NewLoop(p, builtins.All(), cfg.MaxSteps, func(e agent.Event) {
			if e.Type == "tool_start" {
				fmt.Fprintln(os.Stderr, "[tool]", e.Detail)
			}
		}), nil
	}
	var loop *agent.Loop
	if cfg.Provider != "" && cfg.Model != "" {
		loop, err = createLoop(cfg.Provider, cfg.Model)
		if err != nil {
			fatal(err.Error())
		}
	}
	if loop == nil {
		builtins := tools.Builtins{Workdir: cfg.Workdir}
		loop = agent.NewLoop(nil, builtins.All(), cfg.MaxSteps, nil)
	}
	transcript := []agent.Message{}
	if repository != nil {
		transcript = repository.Context()
	}
	persist := func(messages []agent.Message) error {
		if repository == nil {
			return nil
		}
		for _, message := range messages {
			if err := repository.AppendMessage(message); err != nil {
				return err
			}
		}
		return nil
	}
	prompt := strings.TrimSpace(strings.Join(flag.Args(), " "))
	// Match Pi's non-interactive behavior: a prompt supplied on the command
	// line, -p/--print, or stdin redirected from a pipe uses print mode.
	stdinInfo, _ := os.Stdin.Stat()
	nonInteractive := stdinInfo != nil && stdinInfo.Mode()&os.ModeCharDevice == 0
	if prompt == "" && nonInteractive {
		data, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			fatal(readErr.Error())
		}
		prompt = strings.TrimSpace(string(data))
	}
	if prompt != "" && (*printMode || nonInteractive || len(flag.Args()) > 0) {
		if loop.Provider == nil {
			fatal(startupConfigurationError(cfg))
		}
		before := len(transcript)
		transcript = append(transcript, agent.Message{Role: "user", Content: prompt})
		updated, answer, err := loop.Run(context.Background(), transcript)
		if appendErr := persist(updated[before:]); appendErr != nil {
			fatal(appendErr.Error())
		}
		if err != nil {
			fatal(err.Error())
		}
		if *jsonMode {
			payload, marshalErr := json.Marshal(map[string]any{"type": "result", "answer": answer, "model": cfg.Provider + "/" + cfg.Model})
			if marshalErr != nil {
				fatal(marshalErr.Error())
			}
			fmt.Println(string(payload))
		} else {
			fmt.Println(answer)
		}
		return
	}
	notice := startupNotice(cfg)
	authPath := auth.Path(home)
	runner := tui.Runner{Loop: loop, In: os.Stdin, Out: os.Stdout, Transcript: transcript, Notice: notice, Persist: persist, ModelOptions: models.Catalog(), NewSession: func() error {
		if *noSession {
			return nil
		}
		if sessionDir == "" {
			sessionDir = filepath.Dir(cfg.SessionPath)
		}
		newRepository, createErr := session.Create(sessionDir, cfg.Workdir)
		if createErr != nil {
			return createErr
		}
		repository = newRepository
		return nil
	}, SessionInfo: func() string {
		if repository == nil {
			return "Session persistence is disabled."
		}
		providerID, modelID, ok := repository.Model()
		modelText := "not selected"
		if ok {
			modelText = providerID + "/" + modelID
		}
		name := repository.Name()
		if name == "" {
			name = "(unnamed)"
		}
		return fmt.Sprintf("Session %s\nName: %s\nMessages: %d\nModel: %s\nFile: %s", repository.Header().ID, name, len(repository.Context()), modelText, repository.Path())
	}, SetName: func(name string) error {
		if repository == nil {
			return fmt.Errorf("session persistence is disabled")
		}
		return repository.AppendName(name)
	}, Shell: func(ctx context.Context, command string) (string, error) {
		return tools.RunShell(ctx, cfg.Workdir, command)
	}, Login: func(provider, key string) error {
		credentials, err := auth.Load(authPath)
		if err != nil {
			return err
		}
		credentials.Set(provider, key)
		return auth.Save(authPath, credentials)
	}, Logout: func(provider string) error {
		credentials, err := auth.Load(authPath)
		if err != nil {
			return err
		}
		credentials.Delete(provider)
		return auth.Save(authPath, credentials)
	}}

	runner.SelectModel = func(providerID, modelID string) error {
		selected, selectErr := createLoop(providerID, modelID)
		if selectErr != nil {
			return selectErr
		}
		runner.Loop = selected
		cfg.Provider, cfg.Model = providerID, modelID
		if repository != nil {
			if appendErr := repository.AppendModelChange(providerID, modelID); appendErr != nil {
				return appendErr
			}
		}
		projectSettings, loadErr := settings.Load(settings.ProjectPath(cfg.Workdir))
		if loadErr != nil {
			return loadErr
		}
		projectSettings.DefaultModel = &settings.ModelRef{Provider: providerID, ID: modelID}
		return settings.Save(settings.ProjectPath(cfg.Workdir), projectSettings)
	}
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
