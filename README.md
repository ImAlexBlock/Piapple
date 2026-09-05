# Piapple

Piapple is a small, cross-platform Go coding agent inspired by [Pi](https://github.com/earendil-works/pi). It keeps the essential execution model: a model receives a system prompt, transcript, and typed tools; tool results are fed back until the model returns a final answer.

## Included

- Full-screen interactive terminal TUI built on Bubble Tea
- Persistent conversation view, input editor, command handling, history navigation, resize support, alternate screen, and Ctrl+C cancellation
- Interactive commands: `/help`, `/clear`, `/exit`; one-shot CLI mode for scripts
- Tool loop with bounded turns and typed JSON tool arguments
- Built-in `read`, `write`, `edit`, and `bash` tools
- OpenAI-compatible Chat Completions, Anthropic Messages, and Google Gemini provider adapters
- JSONL transcript persistence
- No plugin runtime and no built-in permission layer

## Quick start

```powershell
$env:OPENAI_API_KEY = "..."
go run ./cmd/piapple -provider openai -model gpt-4o-mini
# One-shot mode (useful in scripts)
go run ./cmd/piapple -provider anthropic -model claude-sonnet-4-5 "Explain this repository"
```

Optional flags: `-base-url`, `-api-key`, `-system`, `-max-steps`, `-C`, and `-session`.

## Safety

Like upstream Pi, Piapple executes tools with the permissions of its own process. Run it in a container or sandbox for untrusted repositories.

## Verify

```powershell
go test ./...
go vet ./...
```
