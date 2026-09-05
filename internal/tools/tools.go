package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

const maxOutput = 50 * 1024
const maxLines = 2000

type Builtins struct{ Workdir string }

func (b Builtins) All() []agent.Tool {
	all, _ := b.Select(nil, nil, false)
	return all
}

// Select returns the compiled built-in tools after applying the same include /
// exclude semantics as Pi's --tools and --exclude-tools flags. The order is
// stable so provider requests and snapshots remain deterministic.
func (b Builtins) Select(include, exclude []string, disabled bool) ([]agent.Tool, error) {
	if disabled {
		return nil, nil
	}
	all := []agent.Tool{fileTool{b.Workdir, "read"}, fileTool{b.Workdir, "write"}, fileTool{b.Workdir, "edit"}, fileTool{b.Workdir, "bash"}, fileTool{b.Workdir, "grep"}, fileTool{b.Workdir, "find"}, fileTool{b.Workdir, "ls"}}
	if runtime.GOOS == "windows" {
		all = append(all, fileTool{b.Workdir, "powershell"})
	}
	available := make(map[string]struct{}, len(all))
	for _, tool := range all {
		available[tool.Definition().Name] = struct{}{}
	}
	for _, name := range append(append([]string{}, include...), exclude...) {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, ok := available[name]; !ok {
			return nil, fmt.Errorf("unknown built-in tool %q (available: %s)", name, strings.Join(Names(), ", "))
		}
	}
	included := make(map[string]bool, len(include))
	if len(include) > 0 {
		for _, name := range include {
			included[strings.ToLower(strings.TrimSpace(name))] = true
		}
	}
	excluded := make(map[string]bool, len(exclude))
	for _, name := range exclude {
		excluded[strings.ToLower(strings.TrimSpace(name))] = true
	}
	selected := make([]agent.Tool, 0, len(all))
	for _, tool := range all {
		name := tool.Definition().Name
		if len(included) > 0 && !included[name] {
			continue
		}
		if excluded[name] {
			continue
		}
		selected = append(selected, tool)
	}
	return selected, nil
}

func Names() []string {
	names := []string{"read", "write", "edit", "bash", "grep", "find", "ls"}
	if runtime.GOOS == "windows" {
		names = append(names, "powershell")
	}
	return names
}

func NamesOf(items []agent.Tool) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item != nil {
			out = append(out, item.Definition().Name)
		}
	}
	return out
}

type fileTool struct{ workdir, name string }

func (t fileTool) Definition() agent.ToolDefinition {
	switch t.name {
	case "read":
		return def("read", "Read text file contents; use offset and limit for large files.", map[string]any{"path": str(), "offset": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}})
	case "write":
		return def("write", "Create or overwrite a file.", map[string]any{"path": str(), "content": str()})
	case "edit":
		return def("edit", "Make exact text replacements in a file. oldText must match exactly.", map[string]any{"path": str(), "edits": map[string]any{"type": "array", "items": map[string]any{"type": "object", "properties": map[string]any{"oldText": str(), "newText": str()}, "required": []string{"oldText", "newText"}}}})
	case "grep":
		return def("grep", "Search file contents for a regex or literal pattern. Respects .gitignore.", map[string]any{"pattern": str(), "path": map[string]any{"type": "string", "description": "Directory or file to search (default: current directory)"}, "glob": map[string]any{"type": "string", "description": "Filter files by glob"}, "ignoreCase": map[string]any{"type": "boolean"}, "literal": map[string]any{"type": "boolean"}, "context": map[string]any{"type": "integer"}, "limit": map[string]any{"type": "integer"}})
	case "find":
		return def("find", "Find files by glob pattern.", map[string]any{"pattern": str(), "path": str(), "limit": map[string]any{"type": "integer"}})
	case "ls":
		return def("ls", "List directory contents. Includes dotfiles and sorts entries alphabetically.", map[string]any{"path": map[string]any{"type": "string", "description": "Directory to list (default: current directory)"}, "limit": map[string]any{"type": "integer"}})
	case "powershell":
		return def("powershell", "Execute a PowerShell command in the working directory.", map[string]any{"command": str(), "timeout": map[string]any{"type": "integer", "description": "seconds"}})
	default:
		return def("bash", "Execute a shell command in the working directory.", map[string]any{"command": str(), "timeout": map[string]any{"type": "integer", "description": "seconds"}})
	}
}
func str() map[string]any { return map[string]any{"type": "string"} }
func def(name, description string, props map[string]any) agent.ToolDefinition {
	required := []string{}
	switch name {
	case "read":
		required = []string{"path"}
	case "write":
		required = []string{"path", "content"}
	case "edit":
		required = []string{"path", "edits"}
	case "grep", "find":
		required = []string{"pattern"}
	case "bash", "powershell":
		required = []string{"command"}
	}
	parameters := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		parameters["required"] = required
	}
	return agent.ToolDefinition{Name: name, Description: description, Parameters: parameters}
}
func (t fileTool) Execute(ctx context.Context, raw string) (string, error) {
	var args map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		return "", err
	}
	switch t.name {
	case "read":
		return t.read(args)
	case "write":
		return t.write(args)
	case "edit":
		return t.edit(args)
	case "grep":
		return t.grep(args)
	case "find":
		return t.find(args)
	case "ls":
		return t.ls(args)
	case "powershell":
		return t.powershell(ctx, args)
	default:
		return t.bash(ctx, args)
	}
}
func arg(args map[string]json.RawMessage, name string) (string, error) {
	var value string
	if raw, ok := args[name]; !ok {
		return "", fmt.Errorf("missing %s", name)
	} else if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmt.Errorf("%s must be a string", name)
	}
	return value, nil
}
func (t fileTool) path(args map[string]json.RawMessage) (string, error) {
	p, err := arg(args, "path")
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(t.workdir, p)
	}
	return filepath.Clean(p), nil
}
func limited(value string) string {
	if len(value) > maxOutput {
		return value[:maxOutput] + "\n[output truncated]"
	}
	return value
}
func (t fileTool) read(args map[string]json.RawMessage) (string, error) {
	p, err := t.path(args)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	offset := 0
	limit := maxLines
	_ = json.Unmarshal(args["offset"], &offset)
	_ = json.Unmarshal(args["limit"], &limit)
	if offset > 0 {
		offset--
	}
	if offset < 0 {
		offset = 0
	}
	if limit < 1 {
		limit = maxLines
	}
	end := offset + limit
	if end > len(lines) {
		end = len(lines)
	}
	if offset > len(lines) {
		return "", nil
	}
	return limited(strings.Join(lines[offset:end], "\n")), nil
}
func (t fileTool) write(args map[string]json.RawMessage) (string, error) {
	p, err := t.path(args)
	if err != nil {
		return "", err
	}
	content, err := arg(args, "content")
	if err != nil {
		return "", err
	}
	if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return "", err
	}
	if err = os.WriteFile(p, []byte(content), 0644); err != nil {
		return "", err
	}
	return "wrote " + p, nil
}
func (t fileTool) edit(args map[string]json.RawMessage) (string, error) {
	p, err := t.path(args)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	var edits []struct {
		OldText string `json:"oldText"`
		NewText string `json:"newText"`
	}
	if err = json.Unmarshal(args["edits"], &edits); err != nil {
		return "", err
	}
	if len(edits) == 0 {
		return "", fmt.Errorf("edits must not be empty")
	}
	text := string(data)
	for _, e := range edits {
		if e.OldText == "" {
			return "", fmt.Errorf("oldText must not be empty")
		}
		if !strings.Contains(text, e.OldText) {
			return "", fmt.Errorf("oldText not found")
		}
		text = strings.Replace(text, e.OldText, e.NewText, 1)
	}
	if err = os.WriteFile(p, []byte(text), 0644); err != nil {
		return "", err
	}
	return "edited " + p, nil
}
func RunShell(ctx context.Context, workdir, command string) (string, error) {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-lc", command)
	}
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return limited(string(out)), fmt.Errorf("%w", err)
	}
	return limited(string(out)), nil
}

func (t fileTool) powershell(ctx context.Context, args map[string]json.RawMessage) (string, error) {
	command, err := arg(args, "command")
	if err != nil {
		return "", err
	}
	seconds := 0
	_ = json.Unmarshal(args["timeout"], &seconds)
	if seconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command)
	cmd.Dir = t.workdir
	out, runErr := cmd.CombinedOutput()
	output := limited(string(out))
	if runErr != nil {
		return output, runErr
	}
	return output, nil
}

func (t fileTool) bash(ctx context.Context, args map[string]json.RawMessage) (string, error) {
	command, err := arg(args, "command")
	if err != nil {
		return "", err
	}
	seconds := 0
	_ = json.Unmarshal(args["timeout"], &seconds)
	if seconds > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
		defer cancel()
	}
	return RunShell(ctx, t.workdir, command)
}

func optionalPath(args map[string]json.RawMessage, workdir string) string {
	p, err := arg(args, "path")
	if err != nil || p == "" {
		return workdir
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(workdir, p)
}
func limit(args map[string]json.RawMessage, fallback int) int {
	value := fallback
	_ = json.Unmarshal(args["limit"], &value)
	if value < 1 {
		return fallback
	}
	return value
}
func (t fileTool) ls(args map[string]json.RawMessage) (string, error) {
	entries, err := os.ReadDir(optionalPath(args, t.workdir))
	if err != nil {
		return "", err
	}
	max := limit(args, 500)
	out := []string{}
	for _, entry := range entries {
		suffix := ""
		if entry.IsDir() {
			suffix = "/"
		}
		out = append(out, entry.Name()+suffix)
		if len(out) >= max {
			break
		}
	}
	sort.Strings(out)
	return limited(strings.Join(out, "\n")), nil
}
func (t fileTool) find(args map[string]json.RawMessage) (string, error) {
	pattern, err := arg(args, "pattern")
	if err != nil {
		return "", err
	}
	root := optionalPath(args, t.workdir)
	max := limit(args, 1000)
	rules := loadIgnoreRules(root, t.workdir)
	out := make([]string, 0, max)
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ignoredByRules(root, path, d.IsDir(), rules) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		rel := relativeForRoot(root, path)
		if globMatch(pattern, rel) || globMatch(pattern, filepath.Base(path)) {
			out = append(out, rel)
			if len(out) >= max {
				return fs.SkipAll
			}
		}
		return nil
	})
	if walkErr != nil {
		return "", walkErr
	}
	sort.Strings(out)
	return limited(strings.Join(out, "\n")), nil
}

func (t fileTool) grep(args map[string]json.RawMessage) (string, error) {
	pattern, err := arg(args, "pattern")
	if err != nil {
		return "", err
	}
	var ignoreCase, literal bool
	_ = json.Unmarshal(args["ignoreCase"], &ignoreCase)
	_ = json.Unmarshal(args["literal"], &literal)
	if literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	root := optionalPath(args, t.workdir)
	max := limit(args, 100)
	contextLines := 0
	_ = json.Unmarshal(args["context"], &contextLines)
	if contextLines < 0 {
		contextLines = 0
	}
	glob := ""
	_ = json.Unmarshal(args["glob"], &glob)
	rules := loadIgnoreRules(root, t.workdir)
	out := make([]string, 0, max)
	walkErr := walkTextFiles(root, func(path string, data []byte) bool {
		if isBinary(data) {
			return true
		}
		if glob != "" {
			rel := relativeForRoot(root, path)
			if !globMatch(glob, rel) && !globMatch(glob, filepath.Base(path)) {
				return true
			}
		}
		lines := strings.Split(string(data), "\n")
		emitted := map[int]bool{}
		for lineNo, line := range lines {
			if !re.MatchString(line) {
				continue
			}
			from := lineNo - contextLines
			if from < 0 {
				from = 0
			}
			to := lineNo + contextLines
			if to >= len(lines) {
				to = len(lines) - 1
			}
			for i := from; i <= to; i++ {
				if emitted[i] {
					continue
				}
				emitted[i] = true
				marker := ""
				if i != lineNo {
					marker = "-"
				} else {
					marker = ":"
				}
				out = append(out, fmt.Sprintf("%s:%d%s%s", relativeForRoot(root, path), i+1, marker, lines[i]))
				if len(out) >= max {
					return false
				}
			}
		}
		return len(out) < max
	}, rules)
	if walkErr != nil {
		return "", walkErr
	}
	return limited(strings.Join(out, "\n")), nil
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

type ignoreRule struct {
	pattern string
	negated bool
	dirOnly bool
}

func loadIgnoreRules(root, workdir string) []ignoreRule {
	base := root
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		base = filepath.Dir(root)
	}
	if base == "" {
		base = workdir
	}
	data, err := os.ReadFile(filepath.Join(base, ".gitignore"))
	if err != nil {
		return nil
	}
	rules := []ignoreRule{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		r := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			r.negated = true
			line = line[1:]
		}
		r.dirOnly = strings.HasSuffix(line, "/")
		line = strings.TrimSuffix(line, "/")
		r.pattern = strings.TrimPrefix(filepath.ToSlash(line), "/")
		if r.pattern != "" {
			rules = append(rules, r)
		}
	}
	return rules
}
func ignoredByRules(root, path string, isDir bool, rules []ignoreRule) bool {
	if filepath.Base(path) == ".git" && isDir {
		return true
	}
	if len(rules) == 0 {
		return false
	}
	rel := relativeForRoot(root, path)
	ignored := false
	for _, rule := range rules {
		if rule.dirOnly && !isDir {
			continue
		}
		matched := globMatch(rule.pattern, rel) || (!strings.Contains(rule.pattern, "/") && globMatch(rule.pattern, filepath.Base(rel)))
		if matched {
			ignored = !rule.negated
		}
	}
	return ignored
}
func relativeForRoot(root, path string) string {
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		return filepath.ToSlash(filepath.Base(path))
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}
func globMatch(pattern, value string) bool {
	pattern = filepath.ToSlash(pattern)
	value = filepath.ToSlash(value)
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			b.WriteByte('\\')
			b.WriteByte(pattern[i])
		default:
			b.WriteByte(pattern[i])
		}
	}
	b.WriteString("$")
	matched, _ := regexp.MatchString(b.String(), value)
	return matched
}

func walkTextFiles(root string, visit func(string, []byte) bool, rules []ignoreRule) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ignoredByRules(root, path, d.IsDir(), rules) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if !visit(path, data) {
			return fs.SkipAll
		}
		return nil
	})
}
