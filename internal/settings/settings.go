// Package settings owns Piapple's persistent user and project configuration.
// Its precedence follows Pi: CLI > project .pi/settings.json > user config.
package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type ModelRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}
type Settings struct {
	DefaultModel         *ModelRef         `json:"-"`
	EnabledModels        []ModelRef        `json:"-"`
	DefaultThinkingLevel string            `json:"defaultThinkingLevel,omitempty"`
	ModelThinkingLevels  map[string]string `json:"modelThinkingLevels,omitempty"`
	TUIMode              string            `json:"tuiMode,omitempty"`
	extra                map[string]json.RawMessage
}

func UserPath(home string) string { return filepath.Join(home, ".pi", "agent", "settings.json") }
func LegacyUserPath(home string) string {
	return filepath.Join(home, ".piapple", "agent", "settings.json")
}
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

// UnmarshalJSON accepts both Pi's native settings shape and Piapple's older
// object form. Pi stores the default model as a string next to
// "defaultProvider": "openai", while early Piapple builds wrote
// {"defaultModel":{"provider":"openai","id":"gpt-4o"}}. Keeping the
// raw fields also prevents /model from deleting unrelated Pi settings such as
// theme, tuiMode, or terminal preferences when it saves a new default.
func (s *Settings) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*s = Settings{extra: cloneRaw(raw)}
	if value, ok := raw["defaultThinkingLevel"]; ok {
		_ = json.Unmarshal(value, &s.DefaultThinkingLevel)
	}
	if value, ok := raw["modelThinkingLevels"]; ok {
		_ = json.Unmarshal(value, &s.ModelThinkingLevels)
	}
	if value, ok := raw["tuiMode"]; ok {
		_ = json.Unmarshal(value, &s.TUIMode)
	}
	provider := ""
	if value, ok := raw["defaultProvider"]; ok {
		_ = json.Unmarshal(value, &provider)
	}
	if value, ok := raw["defaultModel"]; ok {
		if model, ok := decodeModelRef(value, provider); ok {
			s.DefaultModel = &model
		}
	}
	if value, ok := raw["enabledModels"]; ok {
		var values []json.RawMessage
		if json.Unmarshal(value, &values) == nil {
			for _, item := range values {
				if model, ok := decodeModelRef(item, provider); ok {
					s.EnabledModels = append(s.EnabledModels, model)
				}
			}
		}
	}
	return nil
}

// MarshalJSON writes Pi's portable string/provider form while retaining
// fields unknown to Piapple. That makes the Go client a safe co-user of an
// existing Pi configuration file instead of replacing it with a reduced one.
func (s Settings) MarshalJSON() ([]byte, error) {
	raw := cloneRaw(s.extra)
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	if s.DefaultModel == nil {
		delete(raw, "defaultModel")
		delete(raw, "defaultProvider")
	} else {
		raw["defaultProvider"] = mustJSON(strings.ToLower(strings.TrimSpace(s.DefaultModel.Provider)))
		raw["defaultModel"] = mustJSON(s.DefaultModel.ID)
	}
	if s.DefaultThinkingLevel != "" {
		raw["defaultThinkingLevel"] = mustJSON(s.DefaultThinkingLevel)
	}
	if s.ModelThinkingLevels != nil {
		raw["modelThinkingLevels"] = mustJSON(s.ModelThinkingLevels)
	}
	if s.TUIMode != "" {
		raw["tuiMode"] = mustJSON(s.TUIMode)
	}
	if s.EnabledModels != nil {
		values := make([]string, 0, len(s.EnabledModels))
		for _, model := range s.EnabledModels {
			if model.Provider == "" || model.ID == "" {
				continue
			}
			values = append(values, strings.ToLower(strings.TrimSpace(model.Provider))+"/"+model.ID)
		}
		raw["enabledModels"] = mustJSON(values)
	}
	return json.Marshal(raw)
}

func decodeModelRef(data json.RawMessage, defaultProvider string) (ModelRef, bool) {
	var object ModelRef
	if json.Unmarshal(data, &object) == nil {
		object.Provider = strings.ToLower(strings.TrimSpace(object.Provider))
		if object.Provider == "" {
			object.Provider = strings.ToLower(strings.TrimSpace(defaultProvider))
		}
		object.ID = strings.TrimSpace(object.ID)
		return object, object.Provider != "" && object.ID != ""
	}
	var value string
	if json.Unmarshal(data, &value) != nil {
		return ModelRef{}, false
	}
	parts := strings.SplitN(strings.TrimSpace(value), "/", 2)
	provider := strings.ToLower(strings.TrimSpace(defaultProvider))
	id := strings.TrimSpace(value)
	if len(parts) == 2 {
		provider, id = strings.ToLower(strings.TrimSpace(parts[0])), strings.TrimSpace(parts[1])
	}
	return ModelRef{Provider: provider, ID: id}, provider != "" && id != ""
}

func cloneRaw(source map[string]json.RawMessage) map[string]json.RawMessage {
	if source == nil {
		return nil
	}
	out := make(map[string]json.RawMessage, len(source))
	for key, value := range source {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
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
