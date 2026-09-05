package systemprompt

import (
	"strings"
	"testing"
)

func TestBuildIncludesStableEnvironmentAndInstructions(t *testing.T) {
	prompt := Build("base", "C:/repo", []ContextFile{{Path: "AGENTS.md", Content: "test rule"}}, []string{"read", "bash"})
	for _, want := range []string{"base", "Operating contract", "C:/repo", "Available tools: read, bash", "test rule"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q: %s", want, prompt)
		}
	}
}

func TestBuildUsesPiStyleDefaultPromptAndToolSnippets(t *testing.T) {
	prompt := Build("", "/repo", nil, []string{"read", "bash", "unknown"})
	for _, want := range []string{
		"expert coding assistant",
		"# Available tools",
		"- read: read a text file",
		"- bash: run a shell command",
		"# Guidelines",
		"Working directory: /repo",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("default prompt missing %q: %s", want, prompt)
		}
	}
}
