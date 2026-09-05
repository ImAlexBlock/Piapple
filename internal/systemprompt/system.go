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
		base = "You are an expert coding assistant operating inside piapple, a Go coding-agent harness. Help the user by inspecting files, executing commands, editing code, and writing new files."
	}
	var b strings.Builder
	b.WriteString(base)
	b.WriteString("\n\n# Available tools\n")
	if len(toolNames) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, name := range toolNames {
			if description := toolDescription(name); description != "" {
				fmt.Fprintf(&b, "- %s: %s\n", name, description)
			} else {
				fmt.Fprintf(&b, "- %s\n", name)
			}
		}
	}
	b.WriteString("\n# Operating contract\n")
	b.WriteString("The user message, this prompt, and the typed tool schemas are the source of truth for the current task.\n")
	b.WriteString("\n# Guidelines\n")
	b.WriteString("- Be concise in your responses.\n")
	b.WriteString("- Inspect relevant files before editing and make focused changes.\n")
	b.WriteString("- Use tools for repository facts instead of guessing.\n")
	b.WriteString("- Explain what changed and how it was verified.\n")
	b.WriteString("- Preserve unrelated user changes and report failures honestly.\n")
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

func toolDescription(name string) string {
	switch name {
	case "read":
		return "read a text file or a bounded slice of lines"
	case "write":
		return "write or replace a file"
	case "edit":
		return "apply an exact text replacement to a file"
	case "bash":
		return "run a shell command on Unix-like systems"
	case "powershell":
		return "run a PowerShell command on Windows"
	case "grep":
		return "search file contents"
	case "find":
		return "find files by glob"
	case "ls":
		return "list directory entries"
	default:
		return ""
	}
}
