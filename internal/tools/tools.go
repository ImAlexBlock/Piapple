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
	return []agent.Tool{fileTool{b.Workdir, "read"}, fileTool{b.Workdir, "write"}, fileTool{b.Workdir, "edit"}, fileTool{b.Workdir, "bash"}, fileTool{b.Workdir, "grep"}, fileTool{b.Workdir, "find"}, fileTool{b.Workdir, "ls"}}
}

func Names() []string { return []string{"read", "write", "edit", "bash", "grep", "find", "ls"} }

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
		return def("grep", "Search file contents using a regular expression.", map[string]any{"pattern": str(), "path": str(), "limit": map[string]any{"type": "integer"}})
	case "find":
		return def("find", "Find files by glob pattern.", map[string]any{"pattern": str(), "path": str(), "limit": map[string]any{"type": "integer"}})
	case "ls":
		return def("ls", "List directory contents.", map[string]any{"path": str(), "limit": map[string]any{"type": "integer"}})
	default:
		return def("bash", "Execute a shell command in the working directory.", map[string]any{"command": str(), "timeout": map[string]any{"type": "integer", "description": "seconds"}})
	}
}
func str() map[string]any { return map[string]any{"type": "string"} }
func def(name, description string, props map[string]any) agent.ToolDefinition {
	required := []string{}
	switch name {
	case "read", "ls":
		required = []string{"path"}
	case "write":
		required = []string{"path", "content"}
	case "edit":
		required = []string{"path", "edits"}
	case "grep", "find":
		required = []string{"pattern"}
	case "bash":
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
		return "[output truncated]\n" + value[len(value)-maxOutput:]
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
	out := []string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		matched, _ := filepath.Match(pattern, filepath.Base(path))
		if !matched {
			matched, _ = filepath.Match(pattern, filepath.ToSlash(relative))
		}
		if matched {
			out = append(out, filepath.ToSlash(relative))
			if len(out) >= max {
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(out)
	return limited(strings.Join(out, "\n")), nil
}
func (t fileTool) grep(args map[string]json.RawMessage) (string, error) {
	pattern, err := arg(args, "pattern")
	if err != nil {
		return "", err
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	root := optionalPath(args, t.workdir)
	max := limit(args, 100)
	out := []string{}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		relative, _ := filepath.Rel(root, path)
		for lineNo, line := range strings.Split(string(data), "\n") {
			if re.MatchString(line) {
				out = append(out, fmt.Sprintf("%s:%d:%s", filepath.ToSlash(relative), lineNo+1, line))
				if len(out) >= max {
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return limited(strings.Join(out, "\n")), nil
}
