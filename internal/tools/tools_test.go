package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func toolByName(t *testing.T, name string) fileTool {
	t.Helper()
	return fileTool{workdir: t.TempDir(), name: name}
}
func TestEditRequiresExactText(t *testing.T) {
	tool := toolByName(t, "edit")
	path := filepath.Join(tool.workdir, "a.txt")
	if err := os.WriteFile(path, []byte("one two"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := tool.Execute(context.Background(), `{"path":"a.txt","edits":[{"oldText":"missing","newText":"x"}]}`)
	if err == nil {
		t.Fatal("expected exact-match failure")
	}
	result, err := tool.Execute(context.Background(), `{"path":"a.txt","edits":[{"oldText":"two","newText":"three"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal("missing result")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "one three" {
		t.Fatalf("got %q", data)
	}
}
func TestWriteThenReadUsesWorkdir(t *testing.T) {
	tool := toolByName(t, "write")
	_, err := tool.Execute(context.Background(), `{"path":"nested/a.txt","content":"hello"}`)
	if err != nil {
		t.Fatal(err)
	}
	read := fileTool{workdir: tool.workdir, name: "read"}
	got, err := read.Execute(context.Background(), `{"path":"nested/a.txt"}`)
	if err != nil || got != "hello" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}
