// Package commands contains Piapple's built-in slash-command grammar. It is
// deliberately separate from the TUI so print/RPC modes use the same parser.
package commands

import "strings"

type Command struct{ Name, Arguments string }

func Parse(input string) (Command, bool) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return Command{}, false
	}
	fields := strings.Fields(input)
	if len(fields) == 0 {
		return Command{}, false
	}
	name := strings.TrimPrefix(fields[0], "/")
	args := ""
	if len(fields) > 1 {
		args = strings.TrimSpace(strings.TrimPrefix(input, fields[0]))
	}
	return Command{Name: name, Arguments: args}, true
}

type Definition struct{ Name, Description, ArgumentHint string }

var Builtins = []Definition{{"settings", "Open settings menu", ""}, {"model", "Select model", "<provider/model>"}, {"tree", "Navigate session tree", ""}, {"thinking", "Set thinking level", "<level>"}, {"export", "Export session", "[path]"}, {"import", "Import a JSONL session", "<path>"}, {"copy", "Copy last agent message", ""}, {"name", "Set session display name", "<name>"}, {"session", "Show session info and stats", ""}, {"fork", "Create a session fork", ""}, {"clone", "Clone the current session", ""}, {"login", "Configure provider authentication", "<provider> [api-key]"}, {"logout", "Remove provider authentication", "<provider>"}, {"new", "Start a new session", ""}, {"compact", "Compact session context", ""}, {"resume", "Resume a different session", ""}, {"reload", "Reload context and settings", ""}, {"quit", "Quit Piapple", ""}, {"exit", "Quit Piapple", ""}}

func Help() string {
	lines := make([]string, 0, len(Builtins))
	for _, d := range Builtins {
		suffix := ""
		if d.ArgumentHint != "" {
			suffix = " " + d.ArgumentHint
		}
		lines = append(lines, "/"+d.Name+suffix+" — "+d.Description)
	}
	return strings.Join(lines, "\n")
}
