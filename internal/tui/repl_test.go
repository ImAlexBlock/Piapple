package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	tea "github.com/charmbracelet/bubbletea"
)

func TestViewportScrollKeepsComposerAtBottom(t *testing.T) {
	m := &model{ctx: context.Background(), historyPos: -1, follow: true, width: 60, height: 12, runner: &Runner{Loop: &agent.Loop{}}}
	for i := 0; i < 40; i++ {
		m.emit("line " + string(rune('A'+i%26)))
	}
	m.input = "draft"
	before := m.View()
	if !strings.Contains(before, "draft") {
		t.Fatal("composer is missing")
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
