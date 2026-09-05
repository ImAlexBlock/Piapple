package tui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/commands"
	"github.com/ImAlexBlock/Piapple/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type SessionChoice struct {
	Path   string
	Label  string
	Detail string
}

type TreeChoice struct {
	ID     string
	Label  string
	Depth  int
	Active bool
}

type Runner struct {
	Loop              *agent.Loop
	In                io.Reader
	Out               io.Writer
	Transcript        []agent.Message
	Persist           func([]agent.Message) error
	Notice            string
	Shell             func(context.Context, string) (string, error)
	Login             func(provider, key string) error
	Logout            func(provider string) error
	SelectModel       func(provider, model string) error
	ModelOptions      []models.Model
	SessionInfo       func() string
	SetName           func(string) error
	NewSession        func() error
	SessionTree       func() string
	ExportSession     func(string) error
	ImportSession     func(string) ([]agent.Message, error)
	ResumeSession     func(string) ([]agent.Message, error)
	ListSessions      func() string
	SessionOptions    func() []SessionChoice
	SelectSession     func(string) ([]agent.Message, error)
	TreeOptions       func() []TreeChoice
	SelectTreeEntry   func(string) ([]agent.Message, error)
	CopyText          func(string) error
	ForkSession       func(bool) ([]agent.Message, error)
	SetThinking       func(string) error
	Compact           func() error
	SettingsView      func() string
	Reload            func() error
	OpenSessionPicker bool
	CurrentModel      func() string
	CurrentThinking   func() string
	Fullscreen        bool

	// bindLoop is installed for the lifetime of Run. Model/session commands can
	// replace Runner.Loop; keeping the event sink attached to the active loop is
	// what makes streaming continue after /model, /resume, /tree, or /reload.
	bindLoop func(*agent.Loop)
}
type resultMsg struct {
	messages       []agent.Message
	answer         string
	err            error
	renderFrom     int
	renderAgent    bool
	renderedAnswer bool
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
	reasoning     string
	usage         *agent.Usage
	picker        bool
	pickerIndex   int
	sessionPicker bool
	sessionIndex  int
	treePicker    bool
	treeIndex     int
	commandIndex  int
	loginProvider string
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
	initialLines = append(initialLines, renderTranscript(r.Transcript)...)
	m := &model{runner: r, ctx: ctx, historyPos: -1, follow: true, lines: initialLines}
	if r.OpenSessionPicker && r.SessionOptions != nil && len(r.SessionOptions()) > 0 {
		m.sessionPicker = true
	}
	programOptions := []tea.ProgramOption{tea.WithMouseCellMotion()}
	if r.Fullscreen {
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	program := tea.NewProgram(m, programOptions...)
	var restoreSink func()
	r.bindLoop = func(loop *agent.Loop) {
		if restoreSink != nil {
			restoreSink()
			restoreSink = nil
		}
		if loop != nil {
			restoreSink = loop.SetEventSink(func(event agent.Event) {
				program.Send(eventMsg{event: event})
			})
		}
	}
	r.bindLoop(r.Loop)
	defer func() {
		r.bindLoop = nil
		if restoreSink != nil {
			restoreSink()
		}
	}()
	_, err := program.Run()
	return err
}

func (r *Runner) syncLoop() {
	if r != nil && r.bindLoop != nil {
		r.bindLoop(r.Loop)
	}
}
func renderTranscript(messages []agent.Message) []string {
	lines := make([]string, 0, len(messages)*2)
	for _, message := range messages {
		switch message.Role {
		case "user":
			lines = append(lines, userStyle.Render("you")+"\n"+wrap(message.Content, 76))
		case "assistant":
			if message.Reasoning != "" {
				lines = append(lines, dimStyle.Render("thinking")+"\n"+wrap(message.Reasoning, 76))
			}
			if message.Content != "" {
				lines = append(lines, titleStyle.Render("assistant")+"\n"+wrap(message.Content, 76))
			}
		case "tool", "toolResult":
			lines = append(lines, toolStyle.Render("tool result")+"\n"+wrap(message.Content, 76))
		case "bashExecution":
			lines = append(lines, toolStyle.Render("$ "+message.Command)+"\n"+wrap(message.Content, 76))
		case "system":
			lines = append(lines, dimStyle.Render(message.Content))
		}
	}
	return lines
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
	// Hardwrap understands ANSI escape sequences emitted by lipgloss. Counting
	// those bytes as runes made a resized transcript overflow into the footer.
	return ansi.Hardwrap(text, width, false)
}
func (m *model) allContentLines() []string {
	var out []string
	for _, line := range m.lines {
		out = append(out, strings.Split(wrap(line, m.contentWidth()), "\n")...)
	}
	if m.reasoning != "" {
		out = append(out, wrap(dimStyle.Render("thinking: "+m.reasoning), m.contentWidth()))
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
	start, end := pickerWindow(m.commandIndex, len(items))
	var b strings.Builder
	for i := start; i < end; i++ {
		item := items[i]
		line := "  /" + item.Name
		if item.ArgumentHint != "" {
			line += " " + hintStyle.Render(item.ArgumentHint)
		}
		line += "  " + dimStyle.Render(item.Description)
		if i == m.commandIndex {
			line = commandStyle.Render("›") + line[1:]
		}
		b.WriteString(line)
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	if start > 0 {
		b.WriteString("\n" + hintStyle.Render("… more commands above"))
	}
	if end < len(items) {
		b.WriteString("\n" + hintStyle.Render("… more commands below"))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7E57C2")).Padding(0, 1).Width(width - 2).Render(b.String())
}
func (m *model) commandSuggestionHeight(width int) int {
	if len(m.commandSuggestions()) == 0 {
		return 0
	}
	return lipgloss.Height(m.commandSuggestionView(width)) + 1
}

const pickerVisibleRows = 8

func pickerWindow(index, total int) (start, end int) {
	if total <= 0 {
		return 0, 0
	}
	if index < 0 {
		index = 0
	}
	if index >= total {
		index = total - 1
	}
	start = (index / pickerVisibleRows) * pickerVisibleRows
	end = start + pickerVisibleRows
	if end > total {
		end = total
	}
	return start, end
}

func (m *model) pickerHeight() int {
	if !m.picker {
		return 0
	}
	total := len(m.runner.ModelOptions)
	if total > pickerVisibleRows {
		total = pickerVisibleRows
	}
	return total + 2
}

func (m *model) pickerView(width int) string {
	if !m.picker {
		return ""
	}
	var b strings.Builder
	b.WriteString(commandStyle.Render("Select model") + " " + hintStyle.Render("↑↓ navigate  Enter select  Esc cancel") + "\n")
	start, end := pickerWindow(m.pickerIndex, len(m.runner.ModelOptions))
	for i := start; i < end; i++ {
		item := m.runner.ModelOptions[i]
		line := "  " + item.Ref()
		if item.Name != "" && item.Name != item.ID {
			line += "  " + dimStyle.Render(item.Name)
		}
		if i == m.pickerIndex {
			line = commandStyle.Render("›") + " " + line[2:]
		}
		b.WriteString(line)
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	if start > 0 {
		b.WriteString("\n" + hintStyle.Render("… more models above"))
	}
	if end < len(m.runner.ModelOptions) {
		b.WriteString("\n" + hintStyle.Render("… more models below"))
	}
	return b.String()
}

func (m *model) sessionPickerView(width int) string {
	if !m.sessionPicker || m.runner.SessionOptions == nil {
		return ""
	}
	items := m.runner.SessionOptions()
	if len(items) == 0 {
		return hintStyle.Render("No sessions found")
	}
	if m.sessionIndex >= len(items) {
		m.sessionIndex = len(items) - 1
	}
	var b strings.Builder
	b.WriteString(commandStyle.Render("Resume session") + " " + hintStyle.Render("↑↓ navigate  Enter select  Esc cancel") + "\n")
	start, end := pickerWindow(m.sessionIndex, len(items))
	for i := start; i < end; i++ {
		item := items[i]
		label := item.Label
		if label == "" {
			label = item.Path
		}
		line := "  " + label
		if item.Detail != "" {
			line += "  " + dimStyle.Render(item.Detail)
		}
		if i == m.sessionIndex {
			line = commandStyle.Render("›") + " " + line[2:]
		}
		b.WriteString(line)
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	if start > 0 {
		b.WriteString("\n" + hintStyle.Render("… more sessions above"))
	}
	if end < len(items) {
		b.WriteString("\n" + hintStyle.Render("… more sessions below"))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7E57C2")).Padding(0, 1).Width(width - 2).Render(b.String())
}

func (m *model) treePickerView(width int) string {
	if !m.treePicker || m.runner.TreeOptions == nil {
		return ""
	}
	items := m.runner.TreeOptions()
	if len(items) == 0 {
		return hintStyle.Render("Session tree is empty")
	}
	if m.treeIndex >= len(items) {
		m.treeIndex = len(items) - 1
	}
	var b strings.Builder
	b.WriteString(commandStyle.Render("Session tree") + " " + hintStyle.Render("↑↓ navigate  Enter switch  Esc cancel") + "\n")
	start, end := pickerWindow(m.treeIndex, len(items))
	for i := start; i < end; i++ {
		item := items[i]
		indent := strings.Repeat("  ", item.Depth)
		line := indent + "  " + item.Label
		if item.Active {
			line += "  " + readyStyle.Render("active")
		}
		if i == m.treeIndex {
			line = indent + commandStyle.Render("›") + " " + strings.TrimSpace(strings.TrimPrefix(line, indent))
		}
		b.WriteString(line)
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	if start > 0 {
		b.WriteString("\n" + hintStyle.Render("… more entries above"))
	}
	if end < len(items) {
		b.WriteString("\n" + hintStyle.Render("… more entries below"))
	}
	return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#7E57C2")).Padding(0, 1).Width(width - 2).Render(b.String())
}

func (m *model) sessionPickerHeight(width int) int {
	if !m.sessionPicker {
		return 0
	}
	return lipgloss.Height(m.sessionPickerView(width)) + 1
}

func (m *model) treePickerHeight(width int) int {
	if !m.treePicker {
		return 0
	}
	return lipgloss.Height(m.treePickerView(width)) + 1
}
func (m *model) composerHeight(width int) int {
	boxHeight := lipgloss.Height(m.composerBox(width))
	return boxHeight + 2 + m.pickerHeight() + m.sessionPickerHeight(width) + m.treePickerHeight(width) + m.commandSuggestionHeight(width) // overlays, status and shortcut rows
}

func (m *model) composerView(width int) string {
	status := readyStyle.Render("● ready")
	if m.busy {
		status = toolStyle.Render("● working") + hintStyle.Render("  model and tools are running")
	}
	meta := []string{status}
	if m.runner.CurrentModel != nil {
		if value := strings.TrimSpace(m.runner.CurrentModel()); value != "" {
			meta = append(meta, titleStyle.Render(value))
		}
	}
	if m.runner.CurrentThinking != nil {
		if value := strings.TrimSpace(m.runner.CurrentThinking()); value != "" {
			meta = append(meta, hintStyle.Render("thinking: "+value))
		}
	}
	if m.usage != nil && (m.usage.InputTokens > 0 || m.usage.OutputTokens > 0 || m.usage.TotalTokens > 0) {
		meta = append(meta, footerStyle.Render(fmt.Sprintf("tokens in:%d out:%d", m.usage.InputTokens, m.usage.OutputTokens)))
	}
	shortcuts := strings.Join([]string{
		keyHint("Enter", "send"), keyHint("Alt+Enter", "newline"), keyHint("↑↓", "history"),
		keyHint("PgUp/PgDn", "scroll"), keyHint("Ctrl+P", "cycle model"), keyHint("Ctrl+C", "cancel / exit"), keyHint("Ctrl+L", "clear view"),
	}, "  ")
	return m.composerBox(width) + "\n" + strings.Join(meta, "  ") + "\n" + footerStyle.Render(shortcuts+"  "+commandStyle.Render("/help"))
}

func (m *model) cycleModel(delta int) {
	if m.busy || len(m.runner.ModelOptions) == 0 {
		return
	}
	current := ""
	if m.runner.CurrentModel != nil {
		current = strings.TrimSpace(m.runner.CurrentModel())
	}
	index := -1
	for i, item := range m.runner.ModelOptions {
		if item.Ref() == current {
			index = i
			break
		}
	}
	if delta == 0 {
		delta = 1
	}
	index = (index + delta) % len(m.runner.ModelOptions)
	if index < 0 {
		index += len(m.runner.ModelOptions)
	}
	item := m.runner.ModelOptions[index]
	if err := m.selectModel(item.Provider, item.ID); err != nil {
		m.emit("Model selection failed: " + err.Error())
		return
	}
	m.emit("Selected " + item.Ref())
}
func (m *model) selectModel(providerID, modelID string) error {
	if m.runner.SelectModel == nil {
		return fmt.Errorf("model selection is unavailable")
	}
	if err := m.runner.SelectModel(providerID, modelID); err != nil {
		return err
	}
	m.runner.syncLoop()
	return nil
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

func (m *model) selectSession() tea.Cmd {
	if !m.sessionPicker || m.runner.SelectSession == nil {
		return nil
	}
	items := m.runner.SessionOptions()
	if len(items) == 0 {
		m.sessionPicker = false
		return nil
	}
	if m.sessionIndex >= len(items) {
		m.sessionIndex = len(items) - 1
	}
	path := items[m.sessionIndex].Path
	m.sessionPicker = false
	messages, err := m.runner.SelectSession(path)
	if err != nil {
		m.emit("Resume failed: " + err.Error())
	} else {
		m.runner.syncLoop()
		m.runner.Transcript = messages
		m.lines = renderTranscript(messages)
		m.scroll = 0
		m.emit(fmt.Sprintf("Resumed %d messages.", len(messages)))
	}
	return nil
}

func (m *model) selectTreeEntry() tea.Cmd {
	if !m.treePicker || m.runner.SelectTreeEntry == nil {
		return nil
	}
	items := m.runner.TreeOptions()
	if len(items) == 0 {
		m.treePicker = false
		return nil
	}
	if m.treeIndex >= len(items) {
		m.treeIndex = len(items) - 1
	}
	id := items[m.treeIndex].ID
	m.treePicker = false
	messages, err := m.runner.SelectTreeEntry(id)
	if err != nil {
		m.emit("Tree selection failed: " + err.Error())
	} else {
		m.runner.syncLoop()
		m.runner.Transcript = messages
		m.lines = renderTranscript(messages)
		m.scroll = 0
		m.emit(fmt.Sprintf("Switched to %d messages.", len(messages)))
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
	if m.loginProvider != "" {
		masked := []rune(strings.Repeat("•", len([]rune(m.input))))
		pos := len(masked)
		if m.cursor < pos {
			pos = m.cursor
		}
		return string(masked[:pos]) + "▌" + string(masked[pos:])
	}
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
	if m.loginProvider != "" {
		providerID := m.loginProvider
		m.loginProvider = ""
		m.setInput("")
		if m.runner.Login == nil {
			m.emit("Credential storage is unavailable.")
		} else if err := m.runner.Login(providerID, text); err != nil {
			m.emit("Login failed: " + err.Error())
		} else {
			m.emit("Saved API key for " + providerID + ".")
		}
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
		before := len(m.runner.Transcript)
		return func() tea.Msg {
			output, err := m.runner.Shell(m.ctx, command)
			var exitCode *int
			if err == nil {
				code := 0
				exitCode = &code
			} else {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					code := exitErr.ExitCode()
					exitCode = &code
				}
			}
			message := agent.Message{Role: "bashExecution", Command: command, Content: output, ExitCode: exitCode, Cancelled: errors.Is(m.ctx.Err(), context.Canceled), ExcludeFromContext: excluded, Timestamp: time.Now().UnixMilli()}
			m.runner.Transcript = append(m.runner.Transcript, message)
			if m.runner.Persist != nil {
				if persistErr := m.runner.Persist([]agent.Message{message}); persistErr != nil {
					return resultMsg{messages: m.runner.Transcript, answer: output, err: persistErr, renderFrom: before}
				}
			}
			return resultMsg{messages: m.runner.Transcript, answer: output, err: err, renderFrom: before}
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
		case "quit", "exit":
			return tea.Quit
		case "hotkeys":
			m.emit("Enter send | Alt+Enter newline | ↑↓ history | PgUp/PgDn scroll | Ctrl+C cancel/exit | Ctrl+L clear view | /help commands")
			return nil
		case "changelog":
			m.emit("Piapple follows Pi's core agent loop, provider streaming, session tree, tools, and fixed-composer TUI. Plugin runtime is intentionally excluded.")
			return nil
		case "scoped-models":
			m.emit("Model cycling scope is the built-in catalog; use /model <provider/model> for any compatible model.")
			return nil
		case "settings":
			if m.runner.SettingsView == nil {
				m.emit("Settings are unavailable.")
			} else {
				m.emit(m.runner.SettingsView())
			}
			return nil
		case "reload":
			if m.runner.Reload == nil {
				m.emit("Reload is unavailable.")
			} else if err := m.runner.Reload(); err != nil {
				m.emit("Reload failed: " + err.Error())
			} else {
				m.runner.syncLoop()
				m.emit("Reloaded settings and project instructions.")
			}
			return nil
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
			if strings.TrimSpace(command.Arguments) != "" && m.runner.SelectTreeEntry != nil {
				messages, err := m.runner.SelectTreeEntry(strings.TrimSpace(command.Arguments))
				if err != nil {
					m.emit("Tree selection failed: " + err.Error())
				} else {
					m.runner.syncLoop()
					m.runner.Transcript = messages
					m.lines = renderTranscript(messages)
					m.scroll = 0
					m.emit(fmt.Sprintf("Switched to %d messages.", len(messages)))
				}
				return nil
			}
			if m.runner.TreeOptions != nil {
				if items := m.runner.TreeOptions(); len(items) > 0 {
					m.treePicker, m.treeIndex = true, 0
					return nil
				}
			}
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
				m.runner.syncLoop()
				m.runner.Transcript = messages
				m.lines = renderTranscript(messages)
				m.emit(fmt.Sprintf("Imported %d messages.", len(messages)))
			}
			return nil
		case "resume":
			path := strings.TrimSpace(command.Arguments)
			if path == "" {
				if m.runner.SessionOptions != nil && m.runner.SelectSession != nil {
					if items := m.runner.SessionOptions(); len(items) > 0 {
						m.sessionPicker, m.sessionIndex = true, 0
						return nil
					}
				}
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
				m.runner.syncLoop()
				m.runner.Transcript = messages
				m.lines = renderTranscript(messages)
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
				m.runner.syncLoop()
				m.runner.Transcript = messages
				m.lines = renderTranscript(messages)
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
			if len(parts) == 0 {
				m.emit("Usage: /login <provider> [api-key]. Supported: openai, anthropic, google, xai, groq, mistral, deepseek, openrouter")
				return nil
			}
			if len(parts) == 1 {
				if m.runner.Login == nil {
					m.emit("Credential storage is unavailable.")
					return nil
				}
				m.loginProvider = parts[0]
				m.setInput("")
				m.emit("Enter API key for " + parts[0] + ". The input is masked; press Enter to save.")
				return nil
			}
			if m.runner.Login == nil {
				m.emit("Credential storage is unavailable.")
				return nil
			}
			if err := m.runner.Login(parts[0], strings.Join(parts[1:], " ")); err != nil {
				m.emit("Login failed: " + err.Error())
			} else {
				m.emit("Saved API key for " + parts[0] + ".")
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
		case "copy":
			var content string
			for i := len(m.runner.Transcript) - 1; i >= 0; i-- {
				if m.runner.Transcript[i].Role == "assistant" && m.runner.Transcript[i].Content != "" {
					content = m.runner.Transcript[i].Content
					break
				}
			}
			if content == "" {
				m.emit("No assistant message to copy.")
				return nil
			}
			if m.runner.CopyText == nil {
				m.emit("Clipboard is unavailable.")
			} else if err := m.runner.CopyText(content); err != nil {
				m.emit("Copy failed: " + err.Error())
			} else {
				m.emit("Copied the last assistant message.")
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
		return resultMsg{messages: next, answer: answer, err: err, renderFrom: before, renderAgent: true}
	}
}
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case eventMsg:
		if v.event.Type == "reasoning_delta" {
			m.reasoning += v.event.Detail
			m.follow = true
		} else if v.event.Type == "model_delta" {
			m.streaming += v.event.Detail
			m.follow = true
		} else if v.event.Type == "tool_start" {
			m.emit(toolStyle.Render("▶ ") + v.event.Detail)
		} else if v.event.Type == "tool_end" {
			m.emit(toolStyle.Render("✓ ") + v.event.Detail)
		}
	case resultMsg:
		m.streaming = ""
		m.reasoning = ""
		m.busy = false
		m.cancel = nil
		m.runner.Transcript = v.messages
		if v.renderAgent {
			start := v.renderFrom
			if start < 0 {
				start = 0
			}
			if start > len(v.messages) {
				start = len(v.messages)
			}
			for _, message := range v.messages[start:] {
				// The user message is rendered synchronously when submit() is
				// called. Assistant/tool messages are appended here so tool
				// results remain visible after the streaming status disappears.
				if message.Role == "user" || message.Role == "system" {
					continue
				}
				if rendered := renderTranscript([]agent.Message{message}); len(rendered) > 0 {
					m.lines = append(m.lines, rendered...)
				}
				if message.Role == "assistant" && (message.Content != "" || message.Reasoning != "") {
					v.renderedAnswer = true
				}
			}
			m.follow = true
		}

		for i := len(v.messages) - 1; i >= 0; i-- {
			if v.messages[i].Role == "assistant" && v.messages[i].Usage != nil {
				m.usage = v.messages[i].Usage
				break
			}
		}
		if v.err != nil {
			m.emit("error: " + v.err.Error())
		}
		if v.answer != "" && !v.renderedAnswer {
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
		if m.sessionPicker {
			switch v.String() {
			case "esc", "ctrl+c":
				m.sessionPicker = false
			case "up":
				if m.sessionIndex > 0 {
					m.sessionIndex--
				}
			case "down":
				if m.runner.SessionOptions != nil {
					if n := len(m.runner.SessionOptions()); m.sessionIndex < n-1 {
						m.sessionIndex++
					}
				}
			case "enter":
				return m, m.selectSession()
			}
			return m, nil
		}
		if m.treePicker {
			switch v.String() {
			case "esc", "ctrl+c":
				m.treePicker = false
			case "up":
				if m.treeIndex > 0 {
					m.treeIndex--
				}
			case "down":
				if m.runner.TreeOptions != nil {
					if n := len(m.runner.TreeOptions()); m.treeIndex < n-1 {
						m.treeIndex++
					}
				}
			case "enter":
				return m, m.selectTreeEntry()
			}
			return m, nil
		}
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
		case "ctrl+p":
			m.cycleModel(1)
		case "ctrl+c":
			if m.loginProvider != "" {
				m.loginProvider = ""
				m.setInput("")
				return m, nil
			}
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
	if suggestions := m.commandSuggestionView(width); suggestions != "" && !m.picker && !m.sessionPicker && !m.treePicker {
		b.WriteString(suggestions)
		b.WriteByte('\n')
	}
	if m.sessionPicker {
		b.WriteString(m.sessionPickerView(width))
		b.WriteByte('\n')
	}
	if m.treePicker {
		b.WriteString(m.treePickerView(width))
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
