package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/auth"
	"github.com/ImAlexBlock/Piapple/internal/cli"
	"github.com/ImAlexBlock/Piapple/internal/clipboard"
	"github.com/ImAlexBlock/Piapple/internal/config"
	"github.com/ImAlexBlock/Piapple/internal/models"
	"github.com/ImAlexBlock/Piapple/internal/projectcontext"
	"github.com/ImAlexBlock/Piapple/internal/provider"
	"github.com/ImAlexBlock/Piapple/internal/rpc"
	"github.com/ImAlexBlock/Piapple/internal/session"
	"github.com/ImAlexBlock/Piapple/internal/settings"
	"github.com/ImAlexBlock/Piapple/internal/systemprompt"
	"github.com/ImAlexBlock/Piapple/internal/tools"
	"github.com/ImAlexBlock/Piapple/internal/tui"
)

func main() {
	opts, parseErr := cli.Parse(os.Args[1:])
	if parseErr != nil {
		fmt.Fprintln(os.Stderr, "piapple:", parseErr)
		fmt.Fprintln(os.Stderr, cli.Usage())
		os.Exit(2)
	}
	if opts.Help {
		fmt.Println(cli.Usage())
		return
	}
	if opts.Version {
		fmt.Println("piapple dev")
		return
	}
	if opts.ListModels {
		items := models.Sort(models.Catalog())
		filter := strings.ToLower(strings.TrimSpace(opts.ModelFilter))
		for _, item := range items {
			if filter != "" && !strings.Contains(strings.ToLower(item.Ref()), filter) && !strings.Contains(strings.ToLower(item.Name), filter) {
				continue
			}
			fmt.Printf("%-14s %-48s %s\n", item.Provider, item.ID, item.Name)
		}
		return
	}
	cfg := opts.Config
	if opts.Mode == cli.ModeJSON {
		opts.JSON, opts.Print = true, true
	}
	if cfg.Provider == "" && strings.Contains(cfg.Model, "/") {
		if providerID, modelID, modelErr := models.ParseRef(cfg.Model); modelErr == nil {
			cfg.Provider, cfg.Model = providerID, modelID
		}
	} // Keep raw CLI overrides separate from resolved settings so switching models
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
	if cfg.Thinking == "" {
		if projectSettings.DefaultThinkingLevel != "" {
			cfg.Thinking = projectSettings.DefaultThinkingLevel
		} else {
			cfg.Thinking = userSettings.DefaultThinkingLevel
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = config.DefaultBaseURL(cfg.Provider)
	}
	baseSystemPrompt := cfg.SystemPrompt
	builtins := tools.Builtins{Workdir: cfg.Workdir}
	selectedTools, toolErr := builtins.Select(opts.Tools, opts.ExcludeTools, opts.NoTools || opts.NoBuiltinTools)
	if toolErr != nil {
		fatal(toolErr.Error())
	}
	toolNames := tools.NamesOf(selectedTools)
	promptFiles := []systemprompt.ContextFile(nil)
	if !opts.NoContextFiles {
		contextFiles, loadErr := projectcontext.Load(cfg.Workdir)
		if loadErr != nil {
			fatal(loadErr.Error())
		}
		promptFiles = make([]systemprompt.ContextFile, 0, len(contextFiles))
		for _, file := range contextFiles {
			promptFiles = append(promptFiles, systemprompt.ContextFile{Path: file.Path, Content: file.Content})
		}
	}
	cfg.SystemPrompt = systemprompt.Build(cfg.SystemPrompt, cfg.Workdir, promptFiles, toolNames)
	for _, addition := range opts.AppendSystemPrompt {
		if strings.TrimSpace(addition) != "" {
			cfg.SystemPrompt += "\n\n" + strings.TrimSpace(addition)
		}
	}
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
	if !opts.NoSession {
		if opts.SessionDir != "" {
			sessionDir = resolvePath(cfg.Workdir, opts.SessionDir)
		} else if cfg.SessionPath != "" {
			sessionDir = filepath.Dir(cfg.SessionPath)
		} else {
			sessionDir = session.DefaultDirectory(home, cfg.Workdir)
		}
		switch {
		case opts.Fork != "":
			sourcePath := resolvePath(cfg.Workdir, opts.Fork)
			if _, statErr := os.Stat(sourcePath); statErr != nil {
				if resolved, findErr := session.FindByID(sessionDir, opts.Fork); findErr == nil {
					sourcePath = resolved
				} else {
					err = fmt.Errorf("fork source %q not found", opts.Fork)
					break
				}
			}
			source, openErr := session.Open(sourcePath)
			if openErr != nil {
				err = openErr
				break
			}
			repository, err = source.Clone(sessionDir, true)
		case opts.SessionID != "":
			repository, err = session.OpenByID(sessionDir, opts.SessionID)
		case cfg.SessionPath != "":
			repository, err = session.Open(cfg.SessionPath)
		case opts.Continue:
			repository, err = session.Continue(sessionDir)
		default:
			repository, err = session.Create(sessionDir, cfg.Workdir)
		}
		if err != nil {
			fatal(fmt.Sprintf("session: %v", err))
		}
		if opts.Name != "" {
			if err = repository.AppendName(opts.Name); err != nil {
				fatal(fmt.Sprintf("session name: %v", err))
			}
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
	if repository != nil && cfg.Thinking == "" {
		cfg.Thinking = repository.Thinking()
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
		p, createErr := provider.New(providerID, provider.Config{Model: modelID, BaseURL: baseURL, APIKey: key, SystemPrompt: cfg.SystemPrompt, Thinking: cfg.Thinking})
		if createErr != nil {
			return nil, createErr
		}
		return agent.NewLoop(p, selectedTools, cfg.MaxSteps, func(e agent.Event) {
			if e.Type == "tool_start" {
				fmt.Fprintln(os.Stderr, "[tool]", e.Detail)
			}
		}), nil
	}
	var loop *agent.Loop
	var loopStartupErr error
	if cfg.Provider != "" && cfg.Model != "" {
		loop, loopStartupErr = createLoop(cfg.Provider, cfg.Model)
	}
	if loop == nil {
		loop = agent.NewLoop(nil, selectedTools, cfg.MaxSteps, nil)
	}
	if repository != nil && cfg.Provider != "" && cfg.Model != "" {
		if _, _, ok := repository.Model(); !ok {
			if appendErr := repository.AppendModelChange(cfg.Provider, cfg.Model); appendErr != nil {
				fatal(appendErr.Error())
			}
		}
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
	prompt := strings.TrimSpace(strings.Join(opts.Messages, " "))
	if len(opts.FileArgs) > 0 {
		promptWithFiles, fileErr := appendFileArguments(cfg.Workdir, prompt, opts.FileArgs)
		if fileErr != nil {
			fatal(fileErr.Error())
		}
		prompt = promptWithFiles
	}
	modelRefs := opts.ModelRefs
	if len(modelRefs) == 0 {
		for _, configured := range [][]settings.ModelRef{projectSettings.EnabledModels, userSettings.EnabledModels} {
			if len(configured) > 0 {
				modelRefs = make([]string, 0, len(configured))
				for _, ref := range configured {
					modelRefs = append(modelRefs, ref.Provider+"/"+ref.ID)
				}
				break
			}
		}
	}
	modelOptions := models.Sort(models.Catalog())
	if len(modelRefs) > 0 {
		modelOptions = make([]models.Model, 0, len(modelRefs))
		for _, ref := range modelRefs {
			providerID, modelID, parseErr := models.ParseRef(ref)
			if parseErr != nil {
				fatal(fmt.Sprintf("--models: %v (%q)", parseErr, ref))
			}
			item, _ := models.Find(providerID, modelID)
			modelOptions = append(modelOptions, item)
		}
	}
	if opts.Export != "" {
		if repository == nil {
			fatal("session export requires session persistence")
		}
		if exportErr := repository.Export(resolvePath(cfg.Workdir, opts.Export)); exportErr != nil {
			fatal(fmt.Sprintf("export: %v", exportErr))
		}
		return
	} // Match Pi's non-interactive behavior: a prompt supplied on the command
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
	if prompt != "" && (opts.Print || nonInteractive || len(opts.Messages) > 0 || len(opts.FileArgs) > 0) {
		if loop.Provider == nil {
			if loopStartupErr != nil {
				fatal(loopStartupErr.Error())
			}
			fatal(startupConfigurationError(cfg))
		}
		before := len(transcript)
		transcript = append(transcript, agent.Message{Role: "user", Content: prompt, Timestamp: time.Now().UnixMilli()})
		var restoreSink func()
		if opts.JSON {
			restoreSink = loop.SetEventSink(func(event agent.Event) {
				writeJSONEvent(event)
			})
		}
		updated, answer, err := loop.Run(context.Background(), transcript)
		if restoreSink != nil {
			restoreSink()
		}
		if appendErr := persist(updated[before:]); appendErr != nil {
			fatal(appendErr.Error())
		}
		if err != nil {
			if opts.JSON {
				writeJSONEvent(agent.Event{Type: "error", Detail: err.Error()})
			}
			fatal(err.Error())
		}
		if opts.JSON {
			writeJSONResult(answer, cfg.Provider+"/"+cfg.Model)
		} else {
			fmt.Println(answer)
		}
		return
	}
	notice := startupNotice(cfg)
	if loopStartupErr != nil {
		notice = loopStartupErr.Error()
	}
	tuiMode := strings.ToLower(strings.TrimSpace(opts.TUI))
	if tuiMode == "" {
		tuiMode = strings.ToLower(strings.TrimSpace(projectSettings.TUIMode))
	}
	if tuiMode == "" {
		tuiMode = strings.ToLower(strings.TrimSpace(userSettings.TUIMode))
	}
	if tuiMode == "" {
		tuiMode = "fullscreen"
	}
	authPath := auth.Path(home)
	runner := tui.Runner{Loop: loop, In: os.Stdin, Out: os.Stdout, Transcript: transcript, Notice: notice, Persist: persist, ModelOptions: modelOptions, Fullscreen: tuiMode != "regular", NewSession: func() error {
		if opts.NoSession {
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
	}, CopyText: clipboard.Write, OpenSessionPicker: opts.Resume}
	runner.CurrentModel = func() string {
		if cfg.Provider == "" || cfg.Model == "" {
			return "no model selected"
		}
		return cfg.Provider + "/" + cfg.Model
	}
	runner.CurrentThinking = func() string { return cfg.Thinking }
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
	resolveSessionPath := func(value string) string {
		value = strings.TrimSpace(value)
		if !filepath.IsAbs(value) {
			value = filepath.Join(cfg.Workdir, value)
		}
		return filepath.Clean(value)
	}
	adoptRepository := func(next *session.Repository) error {
		if next == nil {
			return fmt.Errorf("session is nil")
		}
		repository = next
		sessionDir = filepath.Dir(next.Path())
		runner.Transcript = next.Context()
		if providerID, modelID, ok := next.Model(); ok && (providerID != cfg.Provider || modelID != cfg.Model) {
			selected, selectErr := createLoop(providerID, modelID)
			if selectErr != nil {
				return selectErr
			}
			runner.Loop = selected
			cfg.Provider, cfg.Model = providerID, modelID
		}
		return nil
	}
	runner.SessionOptions = func() []tui.SessionChoice {
		if sessionDir == "" {
			return nil
		}
		items, listErr := session.List(sessionDir)
		if listErr != nil {
			return nil
		}
		choices := make([]tui.SessionChoice, 0, len(items))
		for _, item := range items {
			label := item.Name
			if label == "" {
				label = filepath.Base(item.Path)
			}
			detail := fmt.Sprintf("%d messages", item.Messages)
			if item.Model != "" {
				detail += "  " + item.Model
			}
			choices = append(choices, tui.SessionChoice{Path: item.Path, Label: label, Detail: detail})
		}
		return choices
	}
	runner.SelectSession = func(path string) ([]agent.Message, error) {
		next, openErr := session.Open(resolveSessionPath(path))
		if openErr != nil {
			return nil, openErr
		}
		if err := adoptRepository(next); err != nil {
			return nil, err
		}
		return runner.Transcript, nil
	}
	runner.TreeOptions = func() []tui.TreeChoice {
		if repository == nil {
			return nil
		}
		entries := repository.TreeItems()
		choices := make([]tui.TreeChoice, 0, len(entries))
		for _, item := range entries {
			choices = append(choices, tui.TreeChoice{ID: item.ID, Label: item.Label, Depth: item.Depth, Active: item.Active})
		}
		return choices
	}
	runner.SelectTreeEntry = func(id string) ([]agent.Message, error) {
		if repository == nil {
			return nil, fmt.Errorf("session persistence is disabled")
		}
		if err := repository.Branch(strings.TrimSpace(id)); err != nil {
			return nil, err
		}
		if providerID, modelID, ok := repository.Model(); ok {
			if providerID != cfg.Provider || modelID != cfg.Model {
				selected, createErr := createLoop(providerID, modelID)
				if createErr != nil {
					return nil, createErr
				}
				runner.Loop = selected
				cfg.Provider, cfg.Model = providerID, modelID
			}
		}
		return repository.Context(), nil
	}

	runner.SettingsView = func() string {
		projectModel := "not set"
		if projectSettings.DefaultModel != nil {
			projectModel = projectSettings.DefaultModel.Provider + "/" + projectSettings.DefaultModel.ID
		}
		userModel := "not set"
		if userSettings.DefaultModel != nil {
			userModel = userSettings.DefaultModel.Provider + "/" + userSettings.DefaultModel.ID
		}
		return fmt.Sprintf("Project settings: %s\nUser settings: %s\nActive model: %s/%s\nThinking: %s\nSystem prompt: %d chars", projectModel, userModel, cfg.Provider, cfg.Model, cfg.Thinking, len(cfg.SystemPrompt))
	}
	runner.Reload = func() error {
		var loadErr error
		projectSettings, loadErr = settings.Load(settings.ProjectPath(cfg.Workdir))
		if loadErr != nil {
			return loadErr
		}
		userSettings, loadErr = settings.Load(settings.UserPath(home))
		if loadErr != nil {
			return loadErr
		}
		promptFiles := []systemprompt.ContextFile(nil)
		if !opts.NoContextFiles {
			contextFiles, contextErr := projectcontext.Load(cfg.Workdir)
			if contextErr != nil {
				return contextErr
			}
			promptFiles = make([]systemprompt.ContextFile, 0, len(contextFiles))
			for _, file := range contextFiles {
				promptFiles = append(promptFiles, systemprompt.ContextFile{Path: file.Path, Content: file.Content})
			}
		}
		cfg.SystemPrompt = systemprompt.Build(baseSystemPrompt, cfg.Workdir, promptFiles, toolNames)
		for _, addition := range opts.AppendSystemPrompt {
			if strings.TrimSpace(addition) != "" {
				cfg.SystemPrompt += "\n\n" + strings.TrimSpace(addition)
			}
		}
		if runner.Loop != nil && cfg.Provider != "" && cfg.Model != "" {
			next, createErr := createLoop(cfg.Provider, cfg.Model)
			if createErr != nil {
				return createErr
			}
			runner.Loop = next
		}
		return nil
	}
	runner.SessionTree = func() string {
		if repository == nil {
			return "Session persistence is disabled."
		}
		return repository.Tree()
	}
	runner.ListSessions = func() string {
		if sessionDir == "" {
			return "Session persistence is disabled."
		}
		items, listErr := session.List(sessionDir)
		if listErr != nil {
			return "Session list failed: " + listErr.Error()
		}
		var b strings.Builder
		for i, item := range items {
			name := item.Name
			if name == "" {
				name = "(unnamed)"
			}
			fmt.Fprintf(&b, "%d. %s  %s  %d messages  %s\n", i+1, item.Path, name, item.Messages, item.Model)
		}
		return strings.TrimRight(b.String(), "\n")
	}
	runner.ExportSession = func(path string) error {
		if repository == nil {
			return fmt.Errorf("session persistence is disabled")
		}
		return repository.Export(resolveSessionPath(path))
	}
	runner.ResumeSession = func(path string) ([]agent.Message, error) {
		next, openErr := session.Open(resolveSessionPath(path))
		if openErr != nil {
			return nil, openErr
		}
		if err := adoptRepository(next); err != nil {
			return nil, err
		}
		return runner.Transcript, nil
	}
	runner.ImportSession = func(path string) ([]agent.Message, error) {
		next, openErr := session.Open(resolveSessionPath(path))
		if openErr != nil {
			return nil, openErr
		}
		if err := adoptRepository(next); err != nil {
			return nil, err
		}
		return runner.Transcript, nil
	}
	runner.ForkSession = func(fork bool) ([]agent.Message, error) {
		if repository == nil {
			return nil, fmt.Errorf("session persistence is disabled")
		}
		next, cloneErr := repository.Clone(sessionDir, fork)
		if cloneErr != nil {
			return nil, cloneErr
		}
		if err := adoptRepository(next); err != nil {
			return nil, err
		}
		return runner.Transcript, nil
	}
	thinkingLevel := ""
	if repository != nil {
		thinkingLevel = repository.Thinking()
	}
	runner.SetThinking = func(level string) error {
		level = strings.ToLower(strings.TrimSpace(level))
		switch level {
		case "off", "minimal", "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("unsupported thinking level %q", level)
		}
		thinkingLevel = level
		cfg.Thinking = level
		if runner.Loop != nil && runner.Loop.Provider != nil {
			if thinkingProvider, ok := runner.Loop.Provider.(provider.ThinkingProvider); ok {
				thinkingProvider.SetThinking(level)
			}
		}
		if repository != nil {
			return repository.AppendThinking(thinkingLevel)
		}
		return nil
	}
	runner.Compact = func() error {
		if runner.Loop == nil || runner.Loop.Provider == nil {
			return fmt.Errorf("no model is configured")
		}
		if len(runner.Transcript) < 3 {
			return fmt.Errorf("conversation is already short")
		}
		prompt := agent.Message{Role: "user", Content: "Summarize the conversation so far for a coding agent. Preserve goals, decisions, constraints, files changed, and unresolved work. Return only the concise summary."}
		input := append([]agent.Message{}, runner.Transcript...)
		input = append(input, prompt)
		summaryReply, summaryErr := runner.Loop.Provider.Complete(context.Background(), input, nil)
		if summaryErr != nil {
			return summaryErr
		}
		summary := strings.TrimSpace(summaryReply.Content)
		if summary == "" {
			return fmt.Errorf("provider returned an empty summary")
		}
		keep := 6
		if len(runner.Transcript) < keep {
			keep = len(runner.Transcript)
		}
		runner.Transcript = append([]agent.Message{{Role: "system", Content: "Previous conversation summary:\n" + summary}}, runner.Transcript[len(runner.Transcript)-keep:]...)
		if repository != nil {
			firstKept := repository.FirstRetainedMessageID(keep)
			return repository.AppendCompactionAt(summary, firstKept, 0)
		}
		return nil
	}
	if opts.Mode == cli.ModeRPC {
		if runner.Loop == nil || runner.Loop.Provider == nil {
			if loopStartupErr != nil {
				fatal(loopStartupErr.Error())
			}
			fatal(startupConfigurationError(cfg))
		}
		var rpcServer *rpc.Server
		rpcServer = &rpc.Server{
			Loop:       runner.Loop,
			Transcript: runner.Transcript,
			Models:     models.Catalog(),
			Persist:    runner.Persist,
			SetModel: func(providerID, modelID string) error {
				if err := runner.SelectModel(providerID, modelID); err != nil {
					return err
				}
				rpcServer.Loop = runner.Loop
				return nil
			},
			SetThinking: runner.SetThinking,
			NewSession:  runner.NewSession,
			SwitchSession: func(path string) ([]agent.Message, error) {
				if runner.SelectSession == nil {
					return nil, fmt.Errorf("session switching is unavailable")
				}
				return runner.SelectSession(path)
			},
			ForkSession: func(entryID string) ([]agent.Message, error) {
				if entryID != "" && repository != nil {
					if err := repository.Branch(strings.TrimSpace(entryID)); err != nil {
						return nil, err
					}
				}
				if runner.ForkSession == nil {
					return nil, fmt.Errorf("session fork is unavailable")
				}
				return runner.ForkSession(true)
			},
			CloneSession: func() ([]agent.Message, error) {
				if runner.ForkSession == nil {
					return nil, fmt.Errorf("session clone is unavailable")
				}
				return runner.ForkSession(false)
			},
			SetName: runner.SetName,
			Compact: func(_ string) error {
				if runner.Compact == nil {
					return fmt.Errorf("context compaction is unavailable")
				}
				return runner.Compact()
			},
			Shell: runner.Shell,
			Tree: func() []rpc.TreeNode {
				if repository == nil {
					return nil
				}
				items := repository.TreeItems()
				out := make([]rpc.TreeNode, 0, len(items))
				for _, item := range items {
					out = append(out, rpc.TreeNode{ID: item.ID, Label: item.Label, Depth: item.Depth, Active: item.Active})
				}
				return out
			},
			Entries: func() any {
				if repository == nil {
					return []session.Entry{}
				}
				return repository.Entries()
			},
			State: func() rpc.State {
				state := rpc.State{Provider: cfg.Provider, Model: cfg.Model, ThinkingLevel: thinkingLevel, MessageCount: len(runner.Transcript)}
				if repository != nil {
					state.SessionFile = repository.Path()
					state.SessionID = repository.Header().ID
					state.SessionName = repository.Name()
				}
				return state
			},
		}
		if err := rpcServer.Serve(context.Background(), os.Stdin, os.Stdout); err != nil {
			fatal(err.Error())
		}
		return
	}
	if err := runner.Run(context.Background()); err != nil {
		fatal(err.Error())
	}
}
func appendFileArguments(workdir, prompt string, files []string) (string, error) {
	var b strings.Builder
	if strings.TrimSpace(prompt) != "" {
		b.WriteString(strings.TrimSpace(prompt))
	}
	for _, name := range files {
		path := resolvePath(workdir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read prompt file %s: %w", name, err)
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		fmt.Fprintf(&b, "<file path=\"%s\">\n%s\n</file>", filepath.ToSlash(name), data)
	}
	return strings.TrimSpace(b.String()), nil
}
func resolvePath(workdir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return workdir
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(workdir, value)
	}
	return filepath.Clean(value)
}
func writeJSONEvent(event agent.Event) {
	payload, err := json.Marshal(map[string]any{"type": event.Type, "detail": event.Detail})
	if err == nil {
		fmt.Println(string(payload))
	}
}

func writeJSONResult(answer, model string) {
	payload, err := json.Marshal(map[string]any{"type": "result", "answer": answer, "model": model})
	if err == nil {
		fmt.Println(string(payload))
	}
}

func normalizeLongOptions(args []string) []string {
	out := append([]string(nil), args...)
	for i, arg := range out {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			out[i] = "-" + strings.TrimPrefix(arg, "--")
		}
	}
	return out
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
		envNames := config.APIKeyEnvironmentNames(cfg.Provider)
		if len(envNames) > 0 {
			return fmt.Sprintf("No API key found for %s. Set %s, use /login %s, or pass -api-key.", cfg.Provider, strings.Join(envNames, " or "), cfg.Provider)
		}
		return fmt.Sprintf("No API key found for %s. Use /login %s or pass -api-key.", cfg.Provider, cfg.Provider)
	}
	return ""
}

func fatal(message string) { fmt.Fprintln(os.Stderr, "piapple:", message); os.Exit(1) }
