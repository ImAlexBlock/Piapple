package tui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Runner struct {
	Loop       *agent.Loop
	In         io.Reader
	Out        io.Writer
	Transcript []agent.Message
	Persist    func([]agent.Message) error
}

type resultMsg struct {
	messages  []agent.Message
	answer    string
	err       error
	persisted []agent.Message
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
	width, height int
	mu            sync.Mutex
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	userStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	dimStyle   = lipgloss.NewStyle().Faint(true)
	toolStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFA726"))
)

func (r *Runner) Run(ctx context.Context) error {
	if r.Loop == nil {
		return fmt.Errorf("tui: nil agent loop")
	}
	p := tea.NewProgram(&model{runner: r, ctx: ctx, historyPos: -1}, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}
func (m *model) Init() tea.Cmd    { return nil }
func (m *model) emit(line string) { m.lines = append(m.lines, line) }
func (m *model) submit() tea.Cmd {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy {
		return nil
	}
	m.input = ""
	m.history = append(m.history, text)
	m.historyPos = -1
	switch text {
	case "/help":
		m.emit(dimStyle.Render("/help  /clear  /model  /exit\nEnter sends a prompt. Ctrl+C cancels/exits."))
		return nil
	case "/clear":
		m.lines = nil
		m.runner.Transcript = nil
		return nil
	case "/exit", "/quit":
		return tea.Quit
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
		persisted := next[before:]
		if err == nil && m.runner.Persist != nil {
			err = m.runner.Persist(persisted)
		}
		return resultMsg{messages: next, answer: answer, err: err, persisted: persisted}
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
	case tea.KeyMsg:
		switch v.String() {
		case "ctrl+c":
			if m.busy {
				if m.cancel != nil {
					m.cancel()
				}
				m.emit(dimStyle.Render("cancel requested"))
				return m, nil
			}
			return m, tea.Quit
		case "enter":
			return m, m.submit()
		case "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
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
		case "ctrl+l":
			m.lines = nil
		default:
			if len(v.Runes) > 0 && v.Type == tea.KeyRunes {
				m.input += string(v.Runes)
			}
		}
	}
	return m, nil
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
	var b strings.Builder
	b.WriteString(titleStyle.Render("Piapple"))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render("coding agent"))
	b.WriteString("\n\n")
	available := height - 5
	start := 0
	if len(m.lines) > available {
		start = len(m.lines) - available
	}
	for _, line := range m.lines[start:] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if m.busy {
		b.WriteString(toolStyle.Render("● working..."))
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	prompt := "piapple> "
	if m.busy {
		prompt = "        "
	}
	b.WriteString(userStyle.Render(prompt))
	b.WriteString(m.input)
	b.WriteString("▌\n")
	b.WriteString(dimStyle.Render("Ctrl+C cancel/exit • /help commands"))
	return lipgloss.NewStyle().Width(width).Render(b.String())
}
