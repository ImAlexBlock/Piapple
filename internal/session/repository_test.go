package session

import (
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"testing"
)

func TestRepositoryWritesPiStyleHeaderAndLinkedEntries(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if r.Header().Version != 3 {
		t.Fatal(r.Header())
	}
	if err = r.AppendMessage(agent.Message{Role: "user", Content: "one"}); err != nil {
		t.Fatal(err)
	}
	if err = r.AppendMessage(agent.Message{Role: "assistant", Content: "two"}); err != nil {
		t.Fatal(err)
	}
	entries := r.Entries()
	if entries[1].ParentID == nil || *entries[1].ParentID != entries[0].ID {
		t.Fatalf("entries are not linked: %#v", entries)
	}
	loaded, err := Open(r.Path())
	if err != nil || len(loaded.Context()) != 2 {
		t.Fatalf("load=%#v err=%v", loaded, err)
	}
}
func TestContinueUsesMostRecentlyModifiedSession(t *testing.T) {
	dir := t.TempDir()
	first, _ := Create(dir, "a")
	second, _ := Create(dir, "a")
	got, err := Continue(dir)
	if err != nil || got.Path() != second.Path() {
		t.Fatalf("got=%v second=%v err=%v", got.Path(), second.Path(), err)
	}
	_ = first
}

func TestRepositoryPersistsAndRestoresModelChange(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AppendModelChange("anthropic", "claude-sonnet"); err != nil {
		t.Fatal(err)
	}
	if provider, model, ok := r.Model(); !ok || provider != "anthropic" || model != "claude-sonnet" {
		t.Fatalf("model=%q/%q ok=%v", provider, model, ok)
	}
	loaded, err := Open(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if provider, model, ok := loaded.Model(); !ok || provider != "anthropic" || model != "claude-sonnet" {
		t.Fatalf("loaded model=%q/%q ok=%v", provider, model, ok)
	}
}

func TestRepositoryModelReturnsLatestChange(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.AppendModelChange("openai", "gpt-old")
	_ = r.AppendModelChange("openai", "gpt-new")
	provider, model, ok := r.Model()
	if !ok || provider != "openai" || model != "gpt-new" {
		t.Fatalf("model=%q/%q ok=%v", provider, model, ok)
	}
}

func TestRepositoryPersistsSessionName(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AppendName("release work"); err != nil {
		t.Fatal(err)
	}
	loaded, err := Open(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Name() != "release work" {
		t.Fatalf("name=%q", loaded.Name())
	}
}
