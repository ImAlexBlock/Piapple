# Piapple

Piapple is a cross-platform Go port of the core [Pi coding agent](https://github.com/earendil-works/pi) runtime. It keeps the parts that make Pi useful from a terminal: the agent/tool loop, streaming model responses, JSONL sessions and branches, slash commands, print/RPC modes, and an interactive TUI with a fixed composer. Runtime plugins, extensions, skills, prompt templates, and theme loading are intentionally **not** included.

The project is designed to be a compatible implementation rather than a new agent product:

```text
CLI / TUI / RPC
       |
  session + command state
       |
  agent loop (stream -> tool call -> tool result -> ...)
       |
  compiled provider channels
       |
 OpenAI-compatible / Anthropic / Gemini HTTP APIs
```

## Build

Requires Go 1.26 or newer.

```powershell
go build -trimpath -o .\\dist\\piapple.exe .\\cmd\\piapple
```

The same source builds on Windows, Linux, and macOS. `bash` is used on Unix-like systems and `powershell` is used for the Windows-native shell tool.

## Start the interactive TUI

Piapple deliberately does **not** select a default model or provider. Starting without a model still opens the TUI, so the model can be configured from `/model` or `/login` instead of terminating at startup:

```powershell
.\\dist\\piapple.exe
```

Choose a model in the picker with `/model`, or configure it explicitly:

```powershell
$env:OPENAI_API_KEY = "..."
.\\dist\\piapple.exe --provider openai --model gpt-4o-mini

# provider/model notation is also accepted
.\\dist\\piapple.exe --model anthropic/claude-sonnet-4-5
```

Keys are read from provider environment variables first and then from Pi-compatible auth storage at `$HOME/.pi/agent/auth.json`. In the TUI, `/login openai` opens a masked key editor and `/logout openai` removes the stored key.

Supported compiled channels:

```text
openai anthropic google xai groq mistral deepseek openrouter together
fireworks perplexity moonshot kimi zai minimax siliconflow qwen dashscope github
```

`google`, `gemini`; `moonshot`, `kimi`; and `qwen`, `dashscope` are accepted aliases. All channels are compiled into the binary—there is no runtime provider/plugin loader.

## CLI modes

```text
piapple [options] [prompt...]

-p, --print                 one-shot text output
--mode text                 one-shot text protocol
--mode json, --json         JSON event lines followed by a result object
--mode rpc                  newline-delimited JSON control protocol
--provider NAME             provider channel; no implicit default
--model ID                  model ID or PROVIDER/ID
--api-key KEY               override environment/auth.json
--base-url URL              override the provider endpoint
--thinking LEVEL            off, minimal, low, medium, high, xhigh, max
--models a,b               models available to Ctrl+P cycling
--list-models [FILTER]      print the built-in catalog
```

Examples:

```powershell
.\\dist\\piapple.exe --print --provider deepseek --model deepseek-chat "Explain this repository"
Get-Content prompt.txt | .\\dist\\piapple.exe --provider openai --model gpt-4o -p
.\\dist\\piapple.exe --json --provider openai --model gpt-4o "run the tests"
.\\dist\\piapple.exe --list-models deepseek
```

`@file` arguments are inserted into the initial user message using a `<file path="...">...</file>` block. `--` stops option parsing, which allows prompts beginning with `-`.

Session options:

```text
-c, --continue              continue the newest project session
-r, --resume                open the TUI session picker
--session PATH               open an explicit JSONL session
--session-dir PATH          override the project session directory
--session-id ID             open a session by full or unique ID prefix
--fork PATH|ID              clone a session into a new branch/session
--export PATH               export the current JSONL session and exit
--no-session                disable persistence
-n, --name NAME             name the new session
```

Tool/context options:

```text
-t, --tools a,b             allow only selected built-in tools
-xt, --exclude-tools a,b    disable selected built-in tools
-nt, --no-tools             disable all model tool calls
-nbt, --no-builtin-tools    disable all compiled tools
-nc, --no-context-files     skip AGENTS.md and PI.md project instructions
-C, --cwd PATH              run against another working directory
--system-prompt TEXT        replace the base system prompt
--append-system-prompt T    append an instruction; repeatable
```

## TUI interaction

The default TUI is an alternate-screen layout inspired by Pi/OpenCode:

- the transcript has its own viewport and PgUp/PgDn/mouse-wheel scrolling;
- the bottom composer is a fixed rounded rectangle and does not move when output scrolls;
- Enter sends, Alt+Enter or Ctrl+J inserts a newline, and arrow keys edit/history;
- Ctrl+C cancels a running request and exits when idle; Ctrl+L clears only the view;
- Ctrl+P cycles the configured model list;
- `/` commands have completion with Tab and a bounded picker so small terminals retain the composer.

Built-in slash commands:

```text
/help       /settings     /model       /tree        /thinking
/scoped-models /export    /import      /copy        /name
/session    /changelog    /hotkeys     /fork        /clone
/login      /logout       /new         /compact     /resume
/reload     /quit        /exit
```

Shell escapes follow Pi's convention: `!command` runs in the shell and includes its output in context; `!!command` runs it but excludes the output from subsequent model context.

## Sessions

Sessions are append-only JSONL files under the Pi-compatible project directory `$HOME/.pi/agent/sessions/--<cwd>--`. The repository preserves v1/v2/v3 headers, parent IDs, active leaves, model/thinking changes, compaction boundaries, branch summaries, tool calls/results, and first-class bash execution messages. Existing Pi content-block messages (`text`, `thinking`, `toolCall`, `toolResult`, and `bashExecution`) can be read; legacy Piapple message records remain readable as well.

`/tree`, `/fork`, `/clone`, `/resume`, `/import`, `/export`, and `/session` all operate on that same repository abstraction. No database or background service is required.

## RPC

`--mode rpc` reads one JSON object per line from stdin and writes responses/events to stdout. The basic protocol includes:

```text
prompt, abort, get_state, get_available_models, set_model, cycle_model
get_available_thinking_levels, set_thinking_level, cycle_thinking_level
new_session, switch_session, fork, clone, set_session_name, compact, bash
get_messages, get_entries, get_tree, get_last_assistant_text, get_commands
```

Prompt execution is asynchronous, so an `abort` request can cancel an in-flight provider request or tool execution. The TUI, text, JSON, and RPC front ends all use the same `internal/agent.Loop`.

## Built-in tools

The compiled tool registry provides `read`, `write`, `edit`, `bash`, `powershell` (Windows), `grep`, `find`, and `ls`. Tools validate JSON object arguments, required fields, timeouts, output limits, basic `.gitignore` filtering, glob/literal search modes, and context cancellation. Piapple executes with the permissions of its own process; use a container or sandbox for untrusted code.

## Verification

```powershell
$env:GOCACHE = (Join-Path (Get-Location) '.gocache')
$env:GOMODCACHE = (Join-Path (Get-Location) '.gomodcache')
gofmt -w cmd internal
go test ./...
go vet ./...
go build -trimpath -o .\\dist\\piapple.exe .\\cmd\\piapple
git diff --check
```

The tests cover CLI grammar, provider request/stream parsing, retry behavior, agent tool rounds and cancellation, session wire compatibility/tree semantics, tool contracts, RPC dispatch, and TUI layout/input behavior.
