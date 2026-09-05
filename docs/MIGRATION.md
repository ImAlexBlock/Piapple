# Piapple migration plan

Piapple is a Go migration of Pi's coding-agent core, excluding extension, skill, prompt-package, theme-package and plugin runtimes. Compatibility work is driven by upstream Pi behavior, not a new command design.

## Migration order

1. Persistent settings, auth, model catalog/resolution and slash-command grammar.
2. Pi-compatible JSONL session repository: create, continue, resume, tree, fork and clone.
3. Native commands: login/logout, model, thinking, session lifecycle, compact, shell commands and context reload.
4. Streaming multi-provider runtime and built-in tool parity.
5. CLI print/JSON/RPC modes plus TUI selectors, overlays, autocomplete and tool renderers.

The source compatibility reference is `earendil-works/pi`, `packages/coding-agent`, at the upstream revision recorded in project history.
