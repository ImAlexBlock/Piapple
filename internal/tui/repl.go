package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/commands"
	"github.com/ImAlexBlock/Piapple/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Runner struct {
	Loop          *agent.Loop
	In            io.Reader
	Out           io.Writer
	Transcript    []agent.Message
	Persist       func([]agent.Message) error
	Notice        string
	Shell         func(context.Context, string) (string, error)
	Login         func(provider, key string) error
	Logout        func(provider string) error
	SelectModel   func(provider, model string) error
	ModelOptions  []models.Model
	SessionInfo   func() string
	SetName       func(string) error
	NewSession    func() error
	SessionTree   func() string
	ExportSession func(string) error
	ImportSession func(string) ([]agent.Message, error)
	ResumeSession func(string) ([]agent.Message, error)
	ListSessions  func() string
	ForkSession   func(bool) ([]agent.Message, error)
	SetThinking   func(string) error
	Compact       func() error
}
type resultMsg struct {
	messages []agent.Message
	answer   string
	err      error
}
type eventMsg struct{ event agent.Event }
type model struct {
	runner        *Runner
	ctx           context.Context
	input         string
	cursor        int // rune offset in input
	lines         []string
	history       []string
	historyPos    int
	busy          bool
	cancel        context.CancelFunc
	scroll        int
	follow        bool
	streaming     string
	usage         *agent.Usage
	picker        bool
	pickerIndex   int
	commandIndex  int
	width, height int
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	userStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	dimStyle      = lipgloss.NewStyle().Faint(true)
	toolStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA726"))
	readyStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#66BB6A"))
	keyStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#80CBC4"))
	hintStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#9E9E9E"))
	commandStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#CE93D8"))
	footerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#616161"))
	inputBoxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#5C6BC0")).Padding(0, 1)
	busyBoxStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#FFA726")).Padding(0, 1)
	codeStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#B0BEC5")).Background(lipgloss.Color("#263238"))
)

func (r *Runner) Run(ctx context.Context) error {
	if r.Loop == nil {
		return fmt.Errorf("tui: nil agent loop")
	}
	initialLines := []string{}
	if r.Notice != "" {
		initialLines = append(initialLines, r.Notice)
	}
	m := &model{runner: r, ctx: ctx, historyPos: -1, follow: true, lines: initialLines}
	program := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	previousSink := r.Loop.OnEvent
	r.Loop.OnEvent = func(event agent.Event) {
		if previousSink != nil {
			previousSink(event)
		}
		program.Send(eventMsg{event: event})
	}
	defer func() { r.Loop.OnEvent = previousSink }()
	_, err := program.Run()
	return err
}
func (m *model) Init() tea.Cmd { return nil }
func (m *model) emit(line string) {
	m.lines = append(m.lines, wrap(line, m.contentWidth()))
	m.follow = true
}
func (m *model) contentWidth() int {
	if m.width < 40 {
		return 76
	}
	return m.width - 4
}
func wrap(text string, width int) string {
	if width < 1 {
		return text
	}
	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		if paragraph == "" {
			out = append(out, "")
			continue
		}
		for len([]rune(paragraph)) > width {
			r := []rune(paragraph)
			out = append(out, string(r[:width]))
			paragraph = string(r[width:])
		}
		out = append(out, paragraph)
	}
	return strings.Join(out, "\n")
}
func (m *model) allContentLines() []string {
	var out []string
	for _, line := range m.lines {
		out = append(out, strings.Split(line, "\n")...)
	}
	if m.streaming != "" {
		out = append(out, wrap(userStyle.Render(m.streaming), m.contentWidth()))
	}
	if m.busy {
		out = append(out, toolStyle.Render("● working..."))
	}
	return out
}
func keyHint(key, label string) string {
	return keyStyle.Render(key) + " " + hintStyle.Render(label)
}

func (m *model) composerBox(width int) string {
	innerWidth := width - 6
	if innerWidth < 16 {
		innerWidth = 16
	}
	wrapped := strings.Split(wrap(m.inputWithCursor(), innerWidth), "\n")
	if len(wrapped) == 0 {
		wrapped = []string{"▌"}
	}
	box := inputBoxStyle
	if m.busy {
		box = busyBoxStyle
	}
	return box.Width(width - 2).Render(strings.Join(wrapped, "\n"))
}

func (m *model) commandSuggestions() []commands.Definition {
	trimmed := strings.TrimSpace(m.input)
	if !strings.HasPrefix(trimmed, "/") || strings.ContainsAny(trimmed, " \t\n") {
		return nil
	}
	prefix := strings.TrimPrefix(trimmed, "/")
	defs := append([]commands.Definition{{Name: "help", Description: "Show commands"}, {Name: "clear", Description: "Clear conversation view"}}, commands.Builtins...)
	out := make([]commands.Definition, 0, len(defs))
	for _, definition := range defs {
		if prefix == "" || strings.HasPrefix(definition.Name, prefix) {
			out = append(out, definition)
		}
	}
	return out
}

func (m *model) commandSuggestionView(width int) string {
	items := m.commandSuggestions()
	if len(items) == 0 {
		return ""
	}
	if m.commandIndex >= len(items) {
		m.commandIndex = len(items) - 1
	}
	if m.commandIndex < 0 {
		m.commandIndex = 0
	}
	var b strings.Builder
	for i, item := range items {
		line := "  /" + item.Name
		if item.ArgumentHint != "" {
			line += " " + hintStyle.Render(item.ArgumentHint)
		}
		line += "  " + dimStyle.Render(item.Description)
		if i == m.commandIndex {
			line = commandStyle.Render("›") + line[1:]
		}
		b.WriteString(line)
		if i+1 < len(items) {
			b.WriteByte('\n')
		}
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7E57C2")).Padding(0, 1).Width(width - 2).Render(b.String())
}

func (m *model) commandSuggestionHeight(width int) int {
	if len(m.commandSuggestions()) == 0 {
		return 0
	}
	return lipgloss.Height(m.commandSuggestionView(width)) + 1
}

func (m *model) pickerHeight() int {
	if !m.picker {
		return 0
	}
	return len(m.runner.ModelOptions) + 2
}

func (m *model) pickerView(width int) string {
	if !m.picker {
		return ""
	}
	var b strings.Builder
	b.WriteString(commandStyle.Render("Select model") + " " + hintStyle.Render("↑↓ navigate  Enter select  Esc cancel") + "\n")
	for i, item := range m.runner.ModelOptions {
		line := "  " + item.Ref()
		if item.Name != "" && item.Name != item.ID {
			line += "  " + dimStyle.Render(item.Name)
		}
		if i == m.pickerIndex {
			line = commandStyle.Render("›") + " " + line[2:]
		}
		b.WriteString(line)
		if i+1 < len(m.runner.ModelOptions) {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func (m *model) composerHeight(width int) int {
	boxHeight := lipgloss.Height(m.composerBox(width))
	return boxHeight + 2 + m.pickerHeight() + m.commandSuggestionHeight(width) // picker, suggestions, status and shortcut rows
}

func (m *model) composerView(width int) string {
	status := readyStyle.Render("● ready")
	if m.busy {
		status = toolStyle.Render("● working") + hintStyle.Render("  model and tools are running")
	}
	shortcuts := strings.Join([]string{
		keyHint("Enter", "send"), keyHint("Alt+Enter", "newline"), keyHint("↑↓", "history"),
		keyHint("PgUp/PgDn", "scroll"), keyHint("Ctrl+C", "cancel / exit"), keyHint("Ctrl+L", "clear view"),
	}, "  ")
	return m.composerBox(width) + "\n" + status + "\n" + footerStyle.Render(shortcuts+"  "+commandStyle.Render("/help"))
}
func (m *model) selectModel(providerID, modelID string) error {
	if m.runner.SelectModel == nil {
		return fmt.Errorf("model selection is unavailable")
	}
	return m.runner.SelectModel(providerID, modelID)
}

func (m *model) selectPickedModel() tea.Cmd {
	if !m.picker || len(m.runner.ModelOptions) == 0 {
		return nil
	}
	item := m.runner.ModelOptions[m.pickerIndex]
	m.picker = false
	if err := m.selectModel(item.Provider, item.ID); err != nil {
		m.emit("Model selection failed: " + err.Error())
	} else {
		m.emit("Selected " + item.Ref())
	}
	return nil
}

func (m *model) setInput(value string) {
	m.input = value
	m.cursor = len([]rune(value))
}

func (m *model) insertInput(value string) {
	runes := []rune(m.input)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	runes = append(runes[:m.cursor], append([]rune(value), runes[m.cursor:]...)...)
	m.input = string(runes)
	m.cursor += len([]rune(value))
}

func (m *model) deleteBeforeCursor() {
	runes := []rune(m.input)
	if m.cursor <= 0 || len(runes) == 0 {
		return
	}
	runes = append(runes[:m.cursor-1], runes[m.cursor:]...)
	m.input = string(runes)
	m.cursor--
}

func (m *model) deleteAtCursor() {
	runes := []rune(m.input)
	if m.cursor < 0 || m.cursor >= len(runes) {
		return
	}
	runes = append(runes[:m.cursor], runes[m.cursor+1:]...)
	m.input = string(runes)
}

func (m *model) moveCursor(delta int) {
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len([]rune(m.input)) {
		m.cursor = len([]rune(m.input))
	}
}

func (m *model) inputWithCursor() string {
	runes := []rune(m.input)
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > len(runes) {
		m.cursor = len(runes)
	}
	return string(runes[:m.cursor]) + "▌" + string(runes[m.cursor:])
}

func (m *model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy {
		return nil
	}
	m.input = ""
	m.cursor = 0
	m.history = append(m.history, text)
	m.historyPos = -1
	if strings.HasPrefix(text, "!") {
		excluded := strings.HasPrefix(text, "!!")
		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(text, "!"), "!"))
		if command == "" {
			m.emit("Shell command is empty.")
			return nil
		}
		if m.runner.Shell == nil {
			m.emit("Shell execution is unavailable.")
			return nil
		}
		m.emit(toolStyle.Render("$ ") + command)
		m.busy = true
		return func() tea.Msg {
			output, err := m.runner.Shell(m.ctx, command)
			if err != nil {
				return resultMsg{err: err}
			}
			if !excluded {
				m.runner.Transcript = append(m.runner.Transcript, agent.Message{Role: "user", Content: "Shell command: " + command + "\n" + output})
			}
			return resultMsg{messages: m.runner.Transcript, answer: output}
		}
	}
	if command, ok := commands.Parse(text); ok {
		switch command.Name {
		case "help":
			m.emit(commands.Help())
			return nil
		case "clear":
			m.lines = nil
			m.runner.Transcript = nil
			m.scroll = 0
			return nil
		case "quit":
			return tea.Quit
		case "model":
			if strings.TrimSpace(command.Arguments) == "" {
				if len(m.runner.ModelOptions) == 0 {
					m.emit("No models are available. Use /model <provider/model>.")
					return nil
				}
				m.picker, m.pickerIndex = true, 0
				return nil
			}
			providerID, modelID, err := models.ParseRef(command.Arguments)
			if err != nil {
				m.emit("Usage: /model <provider/model>")
				return nil
			}
			if err := m.selectModel(providerID, modelID); err != nil {
				m.emit("Model selection failed: " + err.Error())
			} else {
				m.emit("Selected " + providerID + "/" + modelID)
			}
			return nil
		case "tree":
			if m.runner.SessionTree == nil {
				m.emit("Session tree is unavailable.")
			} else {
				tree := m.runner.SessionTree()
				if tree == "" {
					tree = "(empty session)"
				}
				m.emit(tree)
			}
			return nil
		case "export":
			path := strings.TrimSpace(command.Arguments)
			if path == "" {
				m.emit("Usage: /export <path>")
				return nil
			}
			if m.runner.ExportSession == nil {
				m.emit("Session export is unavailable.")
				return nil
			}
			if err := m.runner.ExportSession(path); err != nil {
				m.emit("Export failed: " + err.Error())
			} else {
				m.emit("Exported session to " + path)
			}
			return nil
		case "import":
			path := strings.TrimSpace(command.Arguments)
			if path == "" {
				m.emit("Usage: /import <path>")
				return nil
			}
			if m.runner.ImportSession == nil {
				m.emit("Session import is unavailable.")
				return nil
			}
			messages, err := m.runner.ImportSession(path)
			if err != nil {
				m.emit("Import failed: " + err.Error())
			} else {
				m.runner.Transcript = messages
				m.emit(fmt.Sprintf("Imported %d messages.", len(messages)))
			}
			return nil
		case "resume":
			path := strings.TrimSpace(command.Arguments)
			if path == "" {
				if m.runner.ListSessions == nil {
					m.emit("Usage: /resume <session.jsonl>")
				} else {
					list := m.runner.ListSessions()
					if list == "" {
						list = "No sessions found."
					}
					m.emit(list)
				}
				return nil
			}
			if m.runner.ResumeSession == nil {
				m.emit("Session resume is unavailable.")
				return nil
			}
			messages, err := m.runner.ResumeSession(path)
			if err != nil {
				m.emit("Resume failed: " + err.Error())
			} else {
				m.runner.Transcript = messages
				m.lines = nil
				m.scroll = 0
				m.emit(fmt.Sprintf("Resumed %d messages.", len(messages)))
			}
			return nil
		case "fork", "clone":
			if m.runner.ForkSession == nil {
				m.emit("Session branching is unavailable.")
				return nil
			}
			messages, err := m.runner.ForkSession(command.Name == "fork")
			if err != nil {
				m.emit("Session branch failed: " + err.Error())
			} else {
				m.runner.Transcript = messages
				m.lines = nil
				m.scroll = 0
				m.emit(fmt.Sprintf("Created %s session.", command.Name))
			}
			return nil
		case "thinking":
			level := strings.TrimSpace(command.Arguments)
			if level == "" {
				m.emit("Usage: /thinking <off|minimal|low|medium|high>")
				return nil
			}
			if m.runner.SetThinking == nil {
				m.emit("Thinking level control is unavailable.")
				return nil
			}
			if err := m.runner.SetThinking(level); err != nil {
				m.emit("Thinking level failed: " + err.Error())
			} else {
				m.emit("Thinking level: " + level)
			}
			return nil
		case "compact":
			if m.runner.Compact == nil {
				m.emit("Context compaction is unavailable.")
				return nil
			}
			if err := m.runner.Compact(); err != nil {
				m.emit("Compaction failed: " + err.Error())
			} else {
				m.emit("Context compacted.")
			}
			return nil
		case "login":
			parts := strings.Fields(command.Arguments)
			if len(parts) != 2 {
				m.emit("Usage: /login <provider> <api-key>")
				return nil
			}
			if m.runner.Login == nil {
				m.emit("Credential storage is unavailable.")
				return nil
			}
			if err := m.runner.Login(parts[0], parts[1]); err != nil {
				m.emit("Login failed: " + err.Error())
			} else {
				m.emit("Saved API key for " + parts[0] + ". Use /model or restart with -provider " + parts[0] + " -model <model-id>.")
			}
			return nil
		case "new":
			if m.runner.NewSession == nil {
				m.emit("Creating a new session is unavailable.")
				return nil
			}
			if err := m.runner.NewSession(); err != nil {
				m.emit("New session failed: " + err.Error())
			} else {
				m.runner.Transcript = nil
				m.lines = nil
				m.scroll = 0
				m.emit("Started a new session.")
			}
			return nil
		case "session":
			if m.runner.SessionInfo == nil {
				m.emit("Session information is unavailable.")
			} else {
				m.emit(m.runner.SessionInfo())
			}
			return nil
		case "name":
			name := strings.TrimSpace(command.Arguments)
			if name == "" {
				m.emit("Usage: /name <session-name>")
				return nil
			}
			if m.runner.SetName == nil {
				m.emit("Session naming is unavailable.")
				return nil
			}
			if err := m.runner.SetName(name); err != nil {
				m.emit("Session name failed: " + err.Error())
			} else {
				m.emit("Session name: " + name)
			}
			return nil
		case "logout":
			provider := strings.TrimSpace(command.Arguments)
			if provider == "" {
				m.emit("Usage: /logout <provider>")
				return nil
			}
			if m.runner.Logout == nil {
				m.emit("Credential storage is unavailable.")
				return nil
			}
			if err := m.runner.Logout(provider); err != nil {
				m.emit("Logout failed: " + err.Error())
			} else {
				m.emit("Removed API key for " + provider + ".")
			}
			return nil
		default:
			m.emit("/" + command.Name + " is recognized but has not been migrated yet.")
			return nil
		}
	}
	switch text {
	case "/clear":
		m.lines = nil
		m.runner.Transcript = nil
		m.scroll = 0
		return nil
	case "/exit":
		return tea.Quit
	}
	if m.runner.Loop == nil || m.runner.Loop.Provider == nil {
		m.emit("No model is configured. Restart with -model <model-id> and the provider API key.")
		return nil
	}
	m.emit(userStyle.Render("> ") + text)
	m.busy = true
	before := len(m.runner.Transcript)
	m.runner.Transcript = append(m.runner.Transcript, agent.Message{Role: "user", Content: text})
	requestCtx, cancel := context.WithCancel(m.ctx)
	m.cancel = cancel
	return func() tea.Msg {
		defer cancel()
		next, answer, err := m.runner.Loop.Run(requestCtx, m.runner.Transcript)
		if err == nil && m.runner.Persist != nil {
			err = m.runner.Persist(next[before:])
		}
		return resultMsg{messages: next, answer: answer, err: err}
	}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case eventMsg:
		if v.event.Type == "model_delta" {
			m.streaming += v.event.Detail
			m.follow = true
		} else if v.event.Type == "tool_start" {
			m.emit(toolStyle.Render("▶ ") + v.event.Detail)
		} else if v.event.Type == "tool_end" {
			m.emit(toolStyle.Render("✓ ") + v.event.Detail)
		}
	case resultMsg:
		m.streaming = ""
		m.busy = false
		m.cancel = nil
		m.runner.Transcript = v.messages
		if v.err != nil {
			m.emit("error: " + v.err.Error())
		} else {
			m.emit(v.answer)
		}
	case tea.MouseMsg:
		if v.Button == tea.MouseButtonWheelUp {
			m.follow = false
			m.scroll++
		}
		if v.Button == tea.MouseButtonWheelDown {
			m.scroll--
			if m.scroll <= 0 {
				m.scroll = 0
				m.follow = true
			}
		}
	case tea.KeyMsg:
		if m.picker {
			switch v.String() {
			case "esc", "ctrl+c":
				m.picker = false
			case "up":
				if m.pickerIndex > 0 {
					m.pickerIndex--
				}
			case "down":
				if m.pickerIndex < len(m.runner.ModelOptions)-1 {
					m.pickerIndex++
				}
			case "enter":
				return m, m.selectPickedModel()
			}
			return m, nil
		}
		if suggestions := m.commandSuggestions(); len(suggestions) > 0 {
			switch v.String() {
			case "tab":
				item := suggestions[m.commandIndex]
				m.setInput("/" + item.Name + " ")
				return m, nil
			case "up":
				if m.commandIndex > 0 {
					m.commandIndex--
				}
				return m, nil
			case "down":
				if m.commandIndex < len(suggestions)-1 {
					m.commandIndex++
				}
				return m, nil
			}
		}
		switch v.String() {
		case "ctrl+c":
			if m.busy {
				if m.cancel != nil {
					m.cancel()
				}
				m.emit("cancel requested")
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			return m, m.submit()
		case "alt+enter", "ctrl+j":
			m.insertInput("\n")
		case "left":
			m.moveCursor(-1)
		case "right":
			m.moveCursor(1)
		case "backspace":
			m.deleteBeforeCursor()
		case "delete":
			m.deleteAtCursor()
		case "ctrl+a":
			m.cursor = 0
		case "ctrl+e":
			m.cursor = len([]rune(m.input))
		case "ctrl+w":
			runes := []rune(m.input)
			if m.cursor > 0 {
				start := m.cursor
				for start > 0 && runes[start-1] == ' ' {
					start--
				}
				for start > 0 && runes[start-1] != ' ' {
					start--
				}
				runes = append(runes[:start], runes[m.cursor:]...)
				m.input = string(runes)
				m.cursor = start
			}
		case "up":
			if len(m.history) > 0 {
				if m.historyPos < 0 {
					m.historyPos = len(m.history)
				}
				if m.historyPos > 0 {
					m.historyPos--
				}
				m.setInput(m.history[m.historyPos])
			}
		case "down":
			if m.historyPos >= 0 && m.historyPos < len(m.history)-1 {
				m.historyPos++
				m.setInput(m.history[m.historyPos])
			} else {
				m.historyPos = -1
				m.setInput("")
			}
		case "pgup", "ctrl+u":
			m.follow = false
			m.scroll += m.viewportRows(m.layoutWidth()) / 2
		case "pgdown", "ctrl+d":
			m.scroll -= m.viewportRows(m.layoutWidth()) / 2
			if m.scroll <= 0 {
				m.scroll = 0
				m.follow = true
			}
		case "home":
			m.follow = false
			m.scroll = len(m.allContentLines())
		case "end":
			m.scroll = 0
			m.follow = true
		case "ctrl+l":
			m.lines = nil
		default:
			if v.Type == tea.KeyRunes {
				m.insertInput(string(v.Runes))
			}
		}
	}
	return m, nil
}
func (m *model) layoutWidth() int {
	if m.width < 40 {
		return 80
	}
	return m.width
}

func (m *model) viewportRows(width int) int {
	h := m.height
	if h < 8 {
		h = 24
	}
	rows := h - 3 - m.composerHeight(width) // header, divider, and composer
	if rows < 1 {
		return 1
	}
	return rows
}
func (m *model) View() string {
	width := m.width
	if width < 40 {
		width = 80
	}
	height := m.height
	if height < 8 {
		height = 24
	}
	content := m.allContentLines()
	rows := m.viewportRows(width)
	maxScroll := len(content) - rows
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.follow {
		m.scroll = 0
	}
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	start := maxScroll - m.scroll
	if start < 0 {
		start = 0
	}
	end := start + rows
	if end > len(content) {
		end = len(content)
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("Piapple"))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render("coding agent"))
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("─", width))
	b.WriteByte('\n')
	for _, line := range content[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for i := end - start; i < rows; i++ {
		b.WriteByte('\n')
	}
	b.WriteString(strings.Repeat("─", width))
	b.WriteByte('\n')
	if suggestions := m.commandSuggestionView(width); suggestions != "" && !m.picker {
		b.WriteString(suggestions)
		b.WriteByte('\n')
	}
	if m.picker {
		b.WriteString(m.pickerView(width))
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("─", width))
		b.WriteByte('\n')
	}
	b.WriteString(m.composerView(width))
	b.WriteByte('\n')
	return lipgloss.NewStyle().Width(width).Height(height).Render(b.String())
}
