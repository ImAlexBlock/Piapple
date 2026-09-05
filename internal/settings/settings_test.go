package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePrecedence(t *testing.T) {
	user := Settings{DefaultModel: &ModelRef{Provider: "u", ID: "u"}}
	project := Settings{DefaultModel: &ModelRef{Provider: "p", ID: "p"}}
	if got := Resolve(nil, project, user); got.ID != "p" {
		t.Fatalf("project=%#v", got)
	}
	cli := &ModelRef{Provider: "c", ID: "c"}
	if got := Resolve(cli, project, user); got.ID != "c" {
		t.Fatalf("cli=%#v", got)
	}
}

func TestPiCompatiblePaths(t *testing.T) {
	if got := UserPath("C:/home"); got != filepath.Join("C:/home", ".pi", "agent", "settings.json") {
		t.Fatalf("user path=%q", got)
	}
	if got := ProjectPath("C:/repo"); got != filepath.Join("C:/repo", ".pi", "settings.json") {
		t.Fatalf("project path=%q", got)
	}
}

func TestLoadAcceptsPiStringModelAndPreservesUnknownSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	contents := `{"defaultProvider":"openai","defaultModel":"gpt-4o","theme":"dark","tuiMode":"regular"}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DefaultModel == nil || settings.DefaultModel.Provider != "openai" || settings.DefaultModel.ID != "gpt-4o" {
		t.Fatalf("default model=%#v", settings.DefaultModel)
	}
	if settings.TUIMode != "regular" {
		t.Fatalf("tui mode=%q", settings.TUIMode)
	}
	settings.DefaultModel = &ModelRef{Provider: "anthropic", ID: "claude-sonnet-4-5"}
	if err := Save(path, settings); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"theme": "dark"`) || !strings.Contains(string(data), `"defaultProvider": "anthropic"`) || !strings.Contains(string(data), `"defaultModel": "claude-sonnet-4-5"`) {
		t.Fatalf("saved settings=%s", data)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestLoadAcceptsLegacyObjectModelAndModelList(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	contents := `{"defaultModel":{"provider":"google","id":"gemini-2.5-flash"},"enabledModels":["openai/gpt-4o",{"provider":"anthropic","id":"claude-sonnet-4-5"}]}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if settings.DefaultModel == nil || settings.DefaultModel.Provider != "google" {
		t.Fatalf("default model=%#v", settings.DefaultModel)
	}
	if len(settings.EnabledModels) != 2 || settings.EnabledModels[0].Provider+"/"+settings.EnabledModels[0].ID != "openai/gpt-4o" {
		t.Fatalf("enabled models=%#v", settings.EnabledModels)
	}
}
