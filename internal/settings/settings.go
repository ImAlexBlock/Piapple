// Package settings owns Piapple's persistent user and project configuration.
// Its precedence follows Pi: CLI > project .pi/settings.json > user config.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type ModelRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}
type Settings struct {
	DefaultModel  *ModelRef  `json:"defaultModel,omitempty"`
	EnabledModels []ModelRef `json:"enabledModels,omitempty"`
}

func UserPath(home string) string   { return filepath.Join(home, ".piapple", "agent", "settings.json") }
func ProjectPath(cwd string) string { return filepath.Join(cwd, ".pi", "settings.json") }
func Load(path string) (Settings, error) {
	var s Settings
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err = json.Unmarshal(data, &s); err != nil {
		return s, err
	}
	return s, nil
}
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
func Resolve(cli *ModelRef, project, user Settings) *ModelRef {
	if cli != nil {
		return cli
	}
	if project.DefaultModel != nil {
		return project.DefaultModel
	}
	return user.DefaultModel
}
