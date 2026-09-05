package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/models"
	tea "github.com/charmbracelet/bubbletea"
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
