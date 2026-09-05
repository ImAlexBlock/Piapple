package tui

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/commands"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Runner struct {
	Loop        *agent.Loop
	In          io.Reader
	Out         io.Writer
	Transcript  []agent.Message
	Persist     func([]agent.Message) error
	Notice      string
	Shell       func(context.Context, string) (string, error)
	Login       func(provider, key string) error
	Logout      func(provider string) error
	SelectModel func(provider, model string) error
}
type resultMsg struct {
	messages []agent.Message
	answer   string
	err      error
}
type model struct {
	runner        *Runner
	ctx           context.Context
	input         string
	lines         []string
	history       []string
	historyPos    int
	busy          bool
	cancel        context.CancelFunc
	scroll        int
	follow        bool
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
)

func (r *Runner) Run(ctx context.Context) error {
	if r.Loop == nil {
		return fmt.Errorf("tui: nil agent loop")
	}
	initialLines := []string{}
	if r.Notice != "" {
		initialLines = append(initialLines, r.Notice)
	}
	_, err := tea.NewProgram(&model{runner: r, ctx: ctx, historyPos: -1, follow: true, lines: initialLines}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
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
	wrapped := strings.Split(wrap(m.input, innerWidth), "\n")
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}
	last := len(wrapped) - 1
	wrapped[last] += "▌"
	box := inputBoxStyle
	if m.busy {
		box = busyBoxStyle
	}
	return box.Width(width - 2).Render(strings.Join(wrapped, "\n"))
}

func (m *model) composerHeight(width int) int {
	boxHeight := lipgloss.Height(m.composerBox(width))
	return boxHeight + 2 // status and shortcut rows below the bordered input
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
func (m *model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy {
		return nil
	}
	m.input = ""
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
			parts := strings.SplitN(strings.TrimSpace(command.Arguments), "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				m.emit("Usage: /model <provider/model>")
				return nil
			}
			if m.runner.SelectModel == nil {
				m.emit("Model selection is unavailable.")
				return nil
			}
			if err := m.runner.SelectModel(parts[0], parts[1]); err != nil {
				m.emit("Model selection failed: " + err.Error())
			} else {
				m.emit("Selected " + parts[0] + "/" + parts[1])
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
	case resultMsg:
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
			m.input += "\n"
		case "backspace":
			if len(m.input) > 0 {
				r := []rune(m.input)
				m.input = string(r[:len(r)-1])
			}
		case "up":
			if len(m.history) > 0 {
				if m.historyPos < 0 {
					m.historyPos = len(m.history)
				}
				if m.historyPos > 0 {
					m.historyPos--
				}
				m.input = m.history[m.historyPos]
			}
		case "down":
			if m.historyPos >= 0 && m.historyPos < len(m.history)-1 {
				m.historyPos++
				m.input = m.history[m.historyPos]
			} else {
				m.historyPos = -1
				m.input = ""
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
				m.input += string(v.Runes)
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
	b.WriteString(m.composerView(width))
	b.WriteByte('\n')
	return lipgloss.NewStyle().Width(width).Height(height).Render(b.String())
}
