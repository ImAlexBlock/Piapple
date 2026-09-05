package tools

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

func TestBuiltinSchemasDeclareRequiredArguments(t *testing.T) {
	builtins := Builtins{Workdir: t.TempDir()}.All()
	want := map[string][]string{"read": {"path"}, "write": {"path", "content"}, "edit": {"path", "edits"}, "bash": {"command"}, "grep": {"pattern"}, "find": {"pattern"}}
	for _, tool := range builtins {
		definition := tool.Definition()
		if expected, ok := want[definition.Name]; ok {
			got, ok := definition.Parameters["required"].([]string)
			if !ok || len(got) != len(expected) {
				t.Fatalf("%s required=%#v", definition.Name, definition.Parameters["required"])
			}
		}
	}
}

func TestDiscoveryToolsRespectGitignoreAndGlobOptions(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored/\n*.secret\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "ignored"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored", "bad.go"), []byte("needle"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "visible.secret"), []byte("needle"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "good.go"), []byte("Needle\nsecond"), 0644); err != nil {
		t.Fatal(err)
	}
	find := fileTool{workdir: dir, name: "find"}
	got, err := find.Execute(context.Background(), `{"pattern":"**/*.go"}`)
	if err != nil || !strings.Contains(got, "src/good.go") || strings.Contains(got, "ignored") {
		t.Fatalf("find=%q err=%v", got, err)
	}
	grep := fileTool{workdir: dir, name: "grep"}
	got, err = grep.Execute(context.Background(), `{"pattern":"needle","ignoreCase":true,"glob":"*.go"}`)
	if err != nil || !strings.Contains(got, "src/good.go:1") {
		t.Fatalf("grep=%q err=%v", got, err)
	}
	if strings.Contains(got, "visible.secret") {
		t.Fatalf("glob filter ignored: %q", got)
	}
}

func TestBuiltinOptionalLsPathAndPowerShellSchema(t *testing.T) {
	ls := fileTool{workdir: t.TempDir(), name: "ls"}.Definition()
	if _, ok := ls.Parameters["required"]; ok {
		t.Fatalf("ls path should be optional: %#v", ls.Parameters)
	}
	ps := fileTool{workdir: t.TempDir(), name: "powershell"}.Definition()
	required, ok := ps.Parameters["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "command" {
		t.Fatalf("powershell required=%#v", ps.Parameters["required"])
	}
}

func TestBuiltinsSelectHonorsIncludeExcludeAndDisable(t *testing.T) {
	builtins := Builtins{Workdir: t.TempDir()}
	selected, err := builtins.Select([]string{"read", "grep"}, []string{"grep"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := NamesOf(selected); !reflect.DeepEqual(got, []string{"read"}) {
		t.Fatalf("selected=%#v", got)
	}
	disabled, err := builtins.Select(nil, nil, true)
	if err != nil || len(disabled) != 0 {
		t.Fatalf("disabled=%#v err=%v", disabled, err)
	}
	if _, err := builtins.Select([]string{"missing"}, nil, false); err == nil {
		t.Fatal("unknown tool was accepted")
	}
}
