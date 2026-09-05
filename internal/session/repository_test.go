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
