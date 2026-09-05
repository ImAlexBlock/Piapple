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

Piapple follows Pi's model-selection behavior: it does not invent a provider or model. Start the TUI directly, then configure a model through the command line for now:

```powershell
.\dist\piapple.exe
.\dist\piapple.exe -provider openai -model gpt-4o-mini
```

API keys can be supplied by the provider environment variable or `-api-key`:

```powershell
$env:OPENAI_API_KEY = "..."
.\dist\piapple.exe -provider openai -model gpt-4o-mini
```

One-shot mode remains available for scripts:

```powershell
.\dist\piapple.exe -provider anthropic -model claude-sonnet-4-5 "Explain this repository"
```

Optional flags: `-provider`, `-model`, `-base-url`, `-api-key`, `-system`, `-max-steps`, `-C`, and `-session`.
When no model is configured, Piapple still opens the TUI and shows the setup state instead of terminating before the interface starts.
## Safety

Like upstream Pi, Piapple executes tools with the permissions of its own process. Run it in a container or sandbox for untrusted repositories.

## Verify

```powershell
go test ./...
go vet ./...
```
