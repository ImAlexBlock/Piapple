package projectcontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadFollowsAncestorOrderAndOverride(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "AGENTS.md"), []byte("root"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "AGENTS.md"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "AGENTS.override.md"), []byte("override"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(child)
	if err != nil || len(got) != 2 {
		t.Fatalf("%#v %v", got, err)
	}
	if got[0].Content != "root" || got[1].Content != "override" {
		t.Fatalf("%#v", got)
	}
	if !strings.Contains(Format(got), "project_context") {
		t.Fatal("missing context wrapper")
	}
}

func TestLoadWithHomeIncludesPiUserInstructionsFirst(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(cwd, 0755); err != nil {
		t.Fatal(err)
	}
	userDir := filepath.Join(home, ".pi", "agent")
	if err := os.MkdirAll(userDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(userDir, "AGENTS.md"), []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWithHome(cwd, home)
	if err != nil || len(got) != 1 || got[0].Content != "user" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}
