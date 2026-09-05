package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/models"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestViewportScrollKeepsComposerAtBottom(t *testing.T) {
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, width: 60, height: 12, runner: &Runner{Loop: &agent.Loop{}}}
	for i := 0; i < 40; i++ {
		m.emit("line " + string(rune('A'+i%26)))
	}
	empty := m.View()
	if strings.Contains(empty, "Type a message...") || strings.Contains(empty, "piapple>") {
		t.Fatal("empty composer should not render placeholder text")
	}
	m.input = "draft"
	before := m.View()
	if !strings.Contains(before, "draft") {
		t.Fatal("composer is missing")
	}
	if strings.Contains(before, "piapple>") {
		t.Fatal("composer should not render a prompt prefix")
	}
	if !strings.Contains(before, "╭") || !strings.Contains(before, "╰") {
		t.Fatal("composer is not rendered as a bordered box")
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyPgUp}); cmd != nil {
		t.Fatal("scroll should not submit a command")
	}
	after := m.View()
	if !strings.Contains(after, "draft") {
		t.Fatal("composer moved or disappeared after scrolling")
	}
	if before == after {
		t.Fatal("page up did not change viewport")
	}
}

func TestModelCommandOpensAndAppliesPicker(t *testing.T) {
	selected := ""
	r := &Runner{Loop: &agent.Loop{}, ModelOptions: []models.Model{{Provider: "openai", ID: "gpt-4o", Name: "GPT-4o"}, {Provider: "google", ID: "gemini-2.5-flash"}}, SelectModel: func(provider, model string) error {
		selected = provider + "/" + model
		return nil
	}}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r}
	m.input = "/model"
	if cmd := m.submit(); cmd != nil || !m.picker {
		t.Fatalf("picker=%v cmd=%v", m.picker, cmd)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("picker selection should be synchronous")
	}
	if selected != "google/gemini-2.5-flash" || m.picker {
		t.Fatalf("selected=%q picker=%v", selected, m.picker)
	}
}

func TestNewCommandResetsTranscript(t *testing.T) {
	called := false
	r := &Runner{Loop: &agent.Loop{}, Transcript: []agent.Message{{Role: "user", Content: "old"}}, NewSession: func() error { called = true; return nil }}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r, lines: []string{"old output"}}
	m.input = "/new"
	if cmd := m.submit(); cmd != nil {
		t.Fatal("new command should not run asynchronously")
	}
	if !called || len(m.runner.Transcript) != 0 || len(m.lines) != 1 || !strings.Contains(m.lines[0], "Started") {
		t.Fatalf("called=%v transcript=%v lines=%v", called, m.runner.Transcript, m.lines)
	}
}

func TestComposerSupportsCursorEditing(t *testing.T) {
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: &Runner{Loop: &agent.Loop{}}, input: "ac", cursor: 1}
	m.insertInput("b")
	if m.input != "abc" || m.cursor != 2 {
		t.Fatalf("insert input=%q cursor=%d", m.input, m.cursor)
	}
	m.deleteBeforeCursor()
	if m.input != "ac" || m.cursor != 1 {
		t.Fatalf("backspace input=%q cursor=%d", m.input, m.cursor)
	}
	m.setInput("你好")
	m.moveCursor(-1)
	m.insertInput("!")
	if m.input != "你!好" {
		t.Fatalf("unicode input=%q", m.input)
	}
}

func TestCommandAutocompleteUsesTab(t *testing.T) {
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: &Runner{Loop: &agent.Loop{}}, input: "/mod", cursor: 4}
	if len(m.commandSuggestions()) == 0 {
		t.Fatal("expected model suggestion")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.input != "/model " || m.cursor != len([]rune(m.input)) {
		t.Fatalf("input=%q cursor=%d", m.input, m.cursor)
	}
}

func TestInteractiveLoginMasksAndStoresKey(t *testing.T) {
	var gotProvider, gotKey string
	r := &Runner{Loop: &agent.Loop{}, Login: func(provider, key string) error { gotProvider, gotKey = provider, key; return nil }}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r}
	m.input = "/login openai"
	m.cursor = len([]rune(m.input))
	m.submit()
	if m.loginProvider != "openai" {
		t.Fatalf("provider=%q", m.loginProvider)
	}
	m.setInput("secret")
	if strings.Contains(m.inputWithCursor(), "secret") {
		t.Fatal("key leaked in composer")
	}
	m.submit()
	if gotProvider != "openai" || gotKey != "secret" || m.loginProvider != "" {
		t.Fatalf("provider=%q key=%q mode=%q", gotProvider, gotKey, m.loginProvider)
	}
}

func TestResumeCommandOpensInteractiveSessionPicker(t *testing.T) {
	selected := ""
	r := &Runner{Loop: &agent.Loop{}, SessionOptions: func() []SessionChoice {
		return []SessionChoice{{Path: "one.jsonl", Label: "one", Detail: "1 message"}, {Path: "two.jsonl", Label: "two", Detail: "2 messages"}}
	}, SelectSession: func(path string) ([]agent.Message, error) {
		selected = path
		return []agent.Message{{Role: "user", Content: path}}, nil
	}}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r, input: "/resume"}
	if cmd := m.submit(); cmd != nil || !m.sessionPicker {
		t.Fatalf("picker=%v cmd=%v", m.sessionPicker, cmd)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected != "two.jsonl" || m.sessionPicker || len(m.runner.Transcript) != 1 {
		t.Fatalf("selected=%q picker=%v transcript=%#v", selected, m.sessionPicker, m.runner.Transcript)
	}
}

func TestTreeCommandSwitchesActiveBranch(t *testing.T) {
	selected := ""
	r := &Runner{Loop: &agent.Loop{}, TreeOptions: func() []TreeChoice {
		return []TreeChoice{{ID: "root", Label: "user: root"}, {ID: "child", Label: "assistant: child", Depth: 1}}
	}, SelectTreeEntry: func(id string) ([]agent.Message, error) {
		selected = id
		return []agent.Message{{Role: "user", Content: id}}, nil
	}}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r, input: "/tree"}
	m.submit()
	if !m.treePicker {
		t.Fatal("tree picker did not open")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if selected != "child" || m.treePicker || m.runner.Transcript[0].Content != "child" {
		t.Fatalf("selected=%q picker=%v transcript=%#v", selected, m.treePicker, m.runner.Transcript)
	}
}

func TestCopyCommandUsesLastAssistantMessage(t *testing.T) {
	var copied string
	r := &Runner{Loop: &agent.Loop{}, Transcript: []agent.Message{{Role: "user", Content: "question"}, {Role: "assistant", Content: "answer"}}, CopyText: func(text string) error { copied = text; return nil }}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r, input: "/copy"}
	m.submit()
	if copied != "answer" {
		t.Fatalf("copied=%q", copied)
	}
}

func TestCtrlPCyclesModelWithoutMovingComposer(t *testing.T) {
	selected := ""
	r := &Runner{Loop: &agent.Loop{}, CurrentModel: func() string { return "openai/one" }, ModelOptions: []models.Model{{Provider: "openai", ID: "one"}, {Provider: "openai", ID: "two"}}, SelectModel: func(provider, model string) error {
		selected = provider + "/" + model
		return nil
	}}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, width: 72, height: 12, runner: r}
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if selected != "openai/two" || !strings.Contains(m.View(), "openai/one") {
		t.Fatalf("selected=%q view=%q", selected, m.View())
	}
}

func TestTranscriptANSIWrappingUsesTerminalWidth(t *testing.T) {
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, width: 48, height: 12, runner: &Runner{Loop: &agent.Loop{}}}
	m.lines = []string{userStyle.Render(strings.Repeat("x", 100))}
	content := m.allContentLines()
	for _, line := range content {
		if lipgloss.Width(line) > m.contentWidth() {
			t.Fatalf("line width=%d max=%d line=%q", lipgloss.Width(line), m.contentWidth(), line)
		}
	}
}

func TestResultRendersToolResultsAndAssistantOnce(t *testing.T) {
	r := &Runner{Loop: &agent.Loop{}}
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, runner: r, lines: []string{"sent"}}
	m.Update(resultMsg{
		messages: []agent.Message{
			{Role: "user", Content: "read README"},
			{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "1", Name: "read", Arguments: `{"path":"README.md"}`}}},
			{Role: "tool", ToolName: "read", Content: "file contents"},
			{Role: "assistant", Content: "Done."},
		},
		answer:      "Done.",
		renderFrom:  0,
		renderAgent: true,
	})
	view := strings.Join(m.lines, "\n")
	if !strings.Contains(view, "file contents") || !strings.Contains(view, "Done.") {
		t.Fatalf("rendered lines=%q", view)
	}
	if strings.Count(view, "Done.") != 1 {
		t.Fatalf("assistant answer rendered more than once: %q", view)
	}
}
