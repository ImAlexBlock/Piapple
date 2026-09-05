package session

import (
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"os"
	"path/filepath"
	"strings"
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

func TestRepositoryTreeExportAndClone(t *testing.T) {
	dir := t.TempDir()
	r, err := Create(dir, "/project")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.AppendMessage(agent.Message{Role: "user", Content: "hello"})
	_ = r.AppendName("demo")
	if tree := r.Tree(); tree == "" || !strings.Contains(tree, "message/user") || !strings.Contains(tree, "name demo") {
		t.Fatalf("tree=%q", tree)
	}
	exported := filepath.Join(t.TempDir(), "export.jsonl")
	if err := r.Export(exported); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exported); err != nil {
		t.Fatal(err)
	}
	fork, err := r.Clone(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if fork.Header().ParentSession != r.Header().ID || len(fork.Context()) != 1 {
		t.Fatalf("header=%#v context=%v", fork.Header(), fork.Context())
	}
	items, err := List(dir)
	if err != nil || len(items) != 2 {
		t.Fatalf("items=%#v err=%v", items, err)
	}
}

func TestCompactionStartsResumedContextAtSummary(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.AppendMessage(agent.Message{Role: "user", Content: "old"})
	_ = r.AppendMessage(agent.Message{Role: "assistant", Content: "old answer"})
	if err := r.AppendCompaction("keep the goal"); err != nil {
		t.Fatal(err)
	}
	_ = r.AppendMessage(agent.Message{Role: "user", Content: "new"})
	context := r.Context()
	if len(context) != 2 || context[0].Role != "system" || !strings.Contains(context[0].Content, "keep the goal") || context[1].Content != "new" {
		t.Fatalf("context=%#v", context)
	}
}

func TestRepositoryBranchesContextFromSelectedLeaf(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if err = r.AppendMessage(agent.Message{Role: "user", Content: "root"}); err != nil {
		t.Fatal(err)
	}
	rootID := r.Entries()[0].ID
	if err = r.AppendMessage(agent.Message{Role: "assistant", Content: "first"}); err != nil {
		t.Fatal(err)
	}
	assistantID := r.Entries()[1].ID
	if err = r.Branch(rootID); err != nil {
		t.Fatal(err)
	}
	if err = r.AppendMessage(agent.Message{Role: "assistant", Content: "second"}); err != nil {
		t.Fatal(err)
	}
	ctx := r.Context()
	if len(ctx) != 2 || ctx[0].Content != "root" || ctx[1].Content != "second" {
		t.Fatalf("ctx=%#v", ctx)
	}
	children := r.Children(rootID)
	if len(children) != 2 || children[0].ID != assistantID {
		t.Fatalf("children=%#v", children)
	}
	if !strings.Contains(r.Tree(), rootID) || !strings.Contains(r.Tree(), "message/assistant") {
		t.Fatalf("tree=%q", r.Tree())
	}
}

func TestRepositoryCompactionRetainsExplicitFirstEntry(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	_ = r.AppendMessage(agent.Message{Role: "user", Content: "old"})
	_ = r.AppendMessage(agent.Message{Role: "assistant", Content: "keep"})
	keepID := r.Entries()[1].ID
	_ = r.AppendMessage(agent.Message{Role: "user", Content: "latest"})
	if err := r.AppendCompactionAt("summary", keepID, 99); err != nil {
		t.Fatal(err)
	}
	ctx := r.Context()
	if len(ctx) != 3 || ctx[0].Role != "system" || ctx[1].Content != "keep" || ctx[2].Content != "latest" {
		t.Fatalf("ctx=%#v", ctx)
	}
	entry := r.Entries()[len(r.Entries())-1]
	if entry.FirstKeptEntryID != keepID || entry.TokensBefore != 99 {
		t.Fatalf("entry=%#v", entry)
	}
}

func TestRepositoryStateFollowsActiveBranch(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AppendModelChange("openai", "gpt-root"); err != nil {
		t.Fatal(err)
	}
	root := r.Entries()[0].ID
	if err := r.AppendName("root"); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendThinking("low"); err != nil {
		t.Fatal(err)
	}
	if err := r.Branch(root); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendModelChange("anthropic", "claude-branch"); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendName("branch"); err != nil {
		t.Fatal(err)
	}
	if err := r.AppendThinking("high"); err != nil {
		t.Fatal(err)
	}
	if provider, model, ok := r.Model(); !ok || provider != "anthropic" || model != "claude-branch" {
		t.Fatalf("branch model=%q/%q ok=%v", provider, model, ok)
	}
	if r.Name() != "branch" || r.Thinking() != "high" {
		t.Fatalf("branch state name=%q thinking=%q", r.Name(), r.Thinking())
	}
	if err := r.Branch(root); err != nil {
		t.Fatal(err)
	}
	if provider, model, ok := r.Model(); !ok || provider != "openai" || model != "gpt-root" {
		t.Fatalf("root model=%q/%q ok=%v", provider, model, ok)
	}
	if r.Name() != "" || r.Thinking() != "" {
		t.Fatalf("inactive branch leaked name=%q thinking=%q", r.Name(), r.Thinking())
	}
}

func TestFindByIDAcceptsUniquePrefix(t *testing.T) {
	dir := t.TempDir()
	first, err := Create(dir, "/project")
	if err != nil {
		t.Fatal(err)
	}
	id := first.Header().ID
	path, err := FindByID(dir, id[:8])
	if err != nil || path != first.Path() {
		t.Fatalf("path=%q err=%v", path, err)
	}
	if _, err := OpenByID(dir, "missing"); err == nil {
		t.Fatal("missing session was accepted")
	}
}
