// Package systemprompt builds the stable coding-agent prompt sent to every
// provider. Keeping this outside the TUI makes print mode and interactive mode
// use identical instructions, as in Pi's model runtime.
package systemprompt

import (
	"fmt"
	"runtime"
	"strings"
)

type ContextFile struct{ Path, Content string }

func Build(base, workdir string, files []ContextFile, toolNames []string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "You are Piapple, a concise expert coding assistant."
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n# Operating contract\n")
	b.WriteString("You are operating as a coding agent. Inspect relevant files before editing, make focused changes, and explain what changed and how it was verified. Preserve unrelated user changes.\n")
	b.WriteString("Use tools for repository facts instead of guessing. Keep tool calls small and report failures honestly.\n")
	b.WriteString("\n# Environment\n")
	fmt.Fprintf(&b, "- OS: %s\n- Working directory: %s\n", runtime.GOOS, workdir)
	if len(toolNames) > 0 {
		b.WriteString("- Available tools: ")
		b.WriteString(strings.Join(toolNames, ", "))
		b.WriteByte('\n')
	}
	if len(files) > 0 {
		b.WriteString("\n# Project instructions\n")
		for _, file := range files {
			fmt.Fprintf(&b, "<project-instructions path=%q>\n%s\n</project-instructions>\n", file.Path, file.Content)
		}
	}
	return strings.TrimSpace(b.String())
}
