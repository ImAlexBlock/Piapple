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
