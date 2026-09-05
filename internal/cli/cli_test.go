package cli

import (
	"reflect"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/config"
)

func TestParseSupportsPiStyleModesAndPositionalPrompt(t *testing.T) {
	got, err := Parse([]string{"--provider", "openai", "--model=openai/gpt-4o", "--mode", "json", "fix", "the", "bug"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Provider != "openai" || got.Config.Model != "openai/gpt-4o" || got.Mode != ModeJSON || !got.Print || !got.JSON {
		t.Fatalf("options=%+v", got)
	}
	if !reflect.DeepEqual(got.Messages, []string{"fix", "the", "bug"}) {
		t.Fatalf("messages=%#v", got.Messages)
	}
}

func TestParseStopsAtDoubleDashAndSupportsFileArguments(t *testing.T) {
	got, err := Parse([]string{"--print", "--", "--literal", "@README.md", "-not-an-option"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Messages, []string{"--literal", "-not-an-option"}) || !reflect.DeepEqual(got.FileArgs, []string{"README.md"}) {
		t.Fatalf("messages=%#v files=%#v", got.Messages, got.FileArgs)
	}
}

func TestParseReportsUnknownAndMissingOptions(t *testing.T) {
	for _, args := range [][]string{{"--unknown"}, {"--model"}, {"--max-steps", "0"}, {"--mode", "yaml"}} {
		if _, err := Parse(args); err == nil {
			t.Fatalf("Parse(%#v) unexpectedly succeeded", args)
		}
	}
}

func TestParseToolAndModelOptions(t *testing.T) {
	got, err := Parse([]string{"-t", "read,grep", "-xt", "grep", "-nc", "--no-session", "-C", "repo", "--append-system-prompt", "one", "--append-system-prompt=two", "-n", "demo", "--models", "openai/gpt-4o,google/gemini-2.5-flash"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Tools, []string{"read", "grep"}) || !reflect.DeepEqual(got.ExcludeTools, []string{"grep"}) || !got.NoContextFiles || !got.NoSession || got.Config.Workdir != "repo" || got.Name != "demo" {
		t.Fatalf("options=%+v", got)
	}
	if !reflect.DeepEqual(got.AppendSystemPrompt, []string{"one", "two"}) {
		t.Fatalf("append prompts=%#v", got.AppendSystemPrompt)
	}
	if !reflect.DeepEqual(got.ModelRefs, []string{"openai/gpt-4o", "google/gemini-2.5-flash"}) {
		t.Fatalf("model refs=%#v", got.ModelRefs)
	}
}

func TestParseSessionPathAndAtFileAnywhere(t *testing.T) {
	got, err := Parse([]string{"hello", "@one.txt", "--session", "session.jsonl", "@two.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.SessionPath != "session.jsonl" {
		t.Fatalf("session path=%q", got.Config.SessionPath)
	}
	if !reflect.DeepEqual(got.Messages, []string{"hello"}) || !reflect.DeepEqual(got.FileArgs, []string{"one.txt", "two.txt"}) {
		t.Fatalf("messages=%#v files=%#v", got.Messages, got.FileArgs)
	}
}

func TestParseKeepsDefaults(t *testing.T) {
	got, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Mode != ModeInteractive || got.Config.Workdir != "." || got.Config.MaxSteps != 12 || got.Config != (config.Config{Workdir: ".", MaxSteps: 12}) {
		t.Fatalf("options=%+v", got)
	}
}
