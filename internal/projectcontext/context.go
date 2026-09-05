// Package projectcontext implements Pi's project instruction discovery.
package projectcontext

import (
	"os"
	"path/filepath"
	"strings"
)

type File struct {
	Path    string
	Content string
}

// Load walks from filesystem root to cwd. At each directory, AGENTS.override.md
// replaces AGENTS.md/CLAUDE.md, matching Pi's nearest-directory rule.
func Load(cwd string) ([]File, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return LoadWithHome(cwd, home)
}

func LoadWithHome(cwd, home string) ([]File, error) {
	absolute, err := filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	out := []File{}
	if home != "" {
		for _, name := range []string{"AGENTS.override.md", "AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(home, ".pi", "agent", name)
			data, readErr := os.ReadFile(path)
			if os.IsNotExist(readErr) {
				continue
			}
			if readErr != nil {
				return nil, readErr
			}
			out = append(out, File{Path: path, Content: string(data)})
			break
		}
	}
	dirs := []string{}
	for dir := absolute; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		for _, name := range []string{"AGENTS.override.md", "AGENTS.md", "CLAUDE.md"} {
			path := filepath.Join(dir, name)
			data, readErr := os.ReadFile(path)
			if os.IsNotExist(readErr) {
				continue
			}
			if readErr != nil {
				return nil, readErr
			}
			out = append(out, File{Path: path, Content: string(data)})
			break
		}
	}
	return out, nil
}

func Format(files []File) string {
	if len(files) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n<project_context>\nProject-specific instructions:\n")
	for _, file := range files {
		b.WriteString("<project_instructions path=\"")
		b.WriteString(file.Path)
		b.WriteString("\">\n")
		b.WriteString(file.Content)
		b.WriteString("\n</project_instructions>\n")
	}
	b.WriteString("</project_context>")
	return b.String()
}
