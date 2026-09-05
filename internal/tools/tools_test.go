package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestReadOnlyDiscoveryTools(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\nfunc Demo() {}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "b.txt"), []byte("needle"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, raw, want string }{{"ls", `{}`, "a.go"}, {"find", `{"pattern":"*.txt"}`, "nested/b.txt"}, {"grep", `{"pattern":"needle"}`, "nested/b.txt:1:needle"}} {
		tool := fileTool{workdir: dir, name: test.name}
		got, err := tool.Execute(context.Background(), test.raw)
		if err != nil || !strings.Contains(got, test.want) {
			t.Fatalf("%s: got=%q err=%v", test.name, got, err)
		}
	}
}
