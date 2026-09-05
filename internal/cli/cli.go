// Package cli contains the flag grammar shared by Piapple's executable modes.
// It intentionally does not know how a session or provider is created; this
// keeps parsing deterministic and makes it possible to test CLI behavior
// without starting a terminal or making a network request.
package cli

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/config"
)

type Mode string

const (
	ModeInteractive Mode = "interactive"
	ModeText        Mode = "text"
	ModeJSON        Mode = "json"
	ModeRPC         Mode = "rpc"
)

type Diagnostic struct {
	Kind    string
	Message string
}

type Options struct {
	Config config.Config

	Mode        Mode
	Help        bool
	Version     bool
	ListModels  bool
	ModelFilter string
	Continue    bool
	Resume      bool
	NoSession   bool
	SessionDir  string
	SessionID   string
	Fork        string
	Export      string
	Name        string
	ModelRefs   []string
	Print       bool
	JSON        bool
	Verbose     bool
	TUI         string

	AppendSystemPrompt []string
	Tools              []string
	ExcludeTools       []string
	NoTools            bool
	NoBuiltinTools     bool
	NoContextFiles     bool

	Messages []string
	FileArgs []string
}

// Parse parses Pi-compatible command line options. Unlike flag.FlagSet it
// accepts options in any position, supports --name=value, and stops parsing
// after -- so prompts beginning with a dash remain usable.
func Parse(args []string) (Options, error) {
	out := Options{Mode: ModeInteractive, Config: config.Config{Workdir: ".", MaxSteps: 12}}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			out.appendPositional(args[i+1:])
			break
		}
		if arg == "" {
			continue
		}
		// Pi treats @path as a prompt attachment regardless of where it appears
		// in the command line. Keep it out of Messages so a positional file is
		// not accidentally sent as literal text.
		if strings.HasPrefix(arg, "@") && len(arg) > 1 {
			out.FileArgs = append(out.FileArgs, arg[1:])
			continue
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			out.Messages = append(out.Messages, arg)
			continue
		}

		name, inline, hasInline := splitOption(arg)
		switch name {
		case "help", "h":
			if hasInline {
				return out, fmt.Errorf("--%s does not take a value", name)
			}
			out.Help = true
		case "version", "v":
			if hasInline {
				return out, fmt.Errorf("--%s does not take a value", name)
			}
			out.Version = true
		case "provider":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.Provider = strings.ToLower(strings.TrimSpace(value))
		case "model":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.Model = strings.TrimSpace(value)
		case "api-key":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.APIKey = value
		case "base-url":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.BaseURL = value
		case "system", "system-prompt":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.SystemPrompt = value
		case "append-system-prompt":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.AppendSystemPrompt = append(out.AppendSystemPrompt, value)
		case "thinking":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.Thinking = strings.ToLower(strings.TrimSpace(value))
		case "max-steps":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			steps, parseErr := strconv.Atoi(value)
			if parseErr != nil || steps < 1 {
				return out, fmt.Errorf("--max-steps must be a positive integer")
			}
			out.Config.MaxSteps = steps
		case "C", "cwd":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.Workdir = value
		case "session":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Config.SessionPath = value
		case "session-dir":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.SessionDir = value
		case "session-id":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.SessionID = value
		case "fork":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Fork = value
		case "name", "n":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Name = value
		case "continue", "c":
			if hasInline {
				return out, fmt.Errorf("--%s does not take a value", name)
			}
			out.Continue = true
		case "resume", "r":
			if hasInline {
				return out, fmt.Errorf("--%s does not take a value", name)
			}
			out.Resume = true
		case "no-session":
			if hasInline {
				return out, fmt.Errorf("--%s does not take a value", name)
			}
			out.NoSession = true
		case "print", "p":
			if hasInline {
				// Pi treats -p=prompt as a convenient one-shot form.
				out.Messages = append(out.Messages, inline)
			} else {
				out.Print = true
			}
		case "json":
			if hasInline {
				return out, fmt.Errorf("--json does not take a value")
			}
			out.JSON, out.Print, out.Mode = true, true, ModeJSON
		case "mode":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "text":
				out.Mode = ModeText
				out.Print = true
			case "json":
				out.Mode = ModeJSON
				out.JSON, out.Print = true, true
			case "rpc":
				out.Mode = ModeRPC
			default:
				return out, fmt.Errorf("--mode must be text, json, or rpc")
			}
		case "list-models":
			if hasInline {
				out.ModelFilter = inline
			} else if i+1 < len(args) && !isOption(args[i+1]) && !strings.HasPrefix(args[i+1], "@") {
				i++
				out.ModelFilter = args[i]
			}
			out.ListModels = true
		case "tools", "t":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Tools = splitList(value)
		case "exclude-tools", "xt":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.ExcludeTools = splitList(value)
		case "no-tools", "nt":
			if hasInline {
				return out, fmt.Errorf("--no-tools does not take a value")
			}
			out.NoTools = true
		case "no-builtin-tools", "nbt":
			if hasInline {
				return out, fmt.Errorf("--no-builtin-tools does not take a value")
			}
			out.NoBuiltinTools = true
		case "no-context-files", "nc":
			if hasInline {
				return out, fmt.Errorf("--no-context-files does not take a value")
			}
			out.NoContextFiles = true
		case "verbose":
			if hasInline {
				return out, fmt.Errorf("--verbose does not take a value")
			}
			out.Verbose = true
		case "tui-mode":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			value = strings.ToLower(strings.TrimSpace(value))
			if value != "regular" && value != "fullscreen" {
				return out, fmt.Errorf("--tui-mode must be regular or fullscreen")
			}
			out.TUI = value
		case "export":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.Print = true
			out.Export = value
		case "models":
			value, next, err := optionValue(args, i, name, inline, hasInline)
			if err != nil {
				return out, err
			}
			i = next
			out.ModelRefs = splitList(value)
		default:
			return out, fmt.Errorf("unknown option %q", arg)
		}
	}
	if out.Mode == ModeRPC && out.Print {
		// The RPC protocol owns stdin/stdout; a one-shot prompt is ambiguous.
		return out, fmt.Errorf("--mode rpc cannot be combined with --print or --json")
	}
	return out, nil
}

func (o *Options) appendPositional(args []string) {
	for _, arg := range args {
		if strings.HasPrefix(arg, "@") && len(arg) > 1 {
			o.FileArgs = append(o.FileArgs, arg[1:])
		} else {
			o.Messages = append(o.Messages, arg)
		}
	}
}

func splitOption(arg string) (name, inline string, hasInline bool) {
	name = strings.TrimPrefix(arg, "-")
	name = strings.TrimPrefix(name, "-")
	if index := strings.IndexByte(name, '='); index >= 0 {
		return name[:index], name[index+1:], true
	}
	return name, "", false
}

func optionValue(args []string, index int, name, inline string, hasInline bool) (string, int, error) {
	if hasInline {
		if inline == "" {
			return "", index, fmt.Errorf("--%s requires a value", name)
		}
		return inline, index, nil
	}
	if index+1 >= len(args) {
		return "", index, fmt.Errorf("--%s requires a value", name)
	}
	return args[index+1], index + 1, nil
}

func isOption(value string) bool { return strings.HasPrefix(value, "-") && value != "-" }
func splitList(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// Usage is intentionally concise; the executable can print it without
// importing the provider, session, or TUI packages.
func Usage() string {
	return `Usage: piapple [options] [prompt...]

Modes:
  (default)                 Full-screen interactive TUI
  -p, --print               Print one answer and exit
  --mode text|json|rpc      Select a headless protocol
  --json                    Print JSON events and the final result

Model:
  --provider NAME           Provider channel (no implicit default)
  --model ID                Model ID, or PROVIDER/ID
  --api-key KEY             API key (otherwise environment/auth.json)
  --base-url URL            Override provider endpoint
  --thinking LEVEL          off, minimal, low, medium, high, xhigh, max
  --models a,b              Models allowed in the Ctrl+P cycle
  --list-models [FILTER]    List the built-in model catalog

Session:
  -c, --continue            Continue the newest project session
  -r, --resume              Open the session picker on startup
  --session PATH             Use an explicit JSONL session
  --session-dir PATH         Override the project session directory
  --no-session               Disable session persistence
  --fork PATH                Fork an existing session
  --export PATH              Export a session JSONL file and exit

Tools/context:
  -t, --tools a,b            Restrict built-in tools
  -xt, --exclude-tools a,b   Disable selected built-in tools
  -nt, --no-tools            Disable all tool calls
  -nbt, --no-builtin-tools   Disable all built-in tools
  -nc, --no-context-files    Do not load AGENTS.md/PI.md context files

Other:
  -C, --cwd PATH             Working directory
  --system-prompt TEXT       Replace the base system prompt
  --append-system-prompt T   Append an instruction (repeatable)
  -n, --name NAME            Name a new session
  --help, -h                 Show this help
  --version, -v              Show the version`
}
