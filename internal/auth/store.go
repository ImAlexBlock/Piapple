// Package auth stores provider credentials separately from settings, following
// Pi's auth.json model. API keys are never placed in settings.json.
package auth

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type APIKeyCredential struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}
type File map[string]APIKeyCredential

func Path(home string) string       { return filepath.Join(home, ".pi", "agent", "auth.json") }
func LegacyPath(home string) string { return filepath.Join(home, ".piapple", "agent", "auth.json") }
func Load(path string) (File, error) {
	var file File
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return File{}, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	return file, nil
}
func Save(path string, file File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
func (f File) Get(provider string) string {
	credential, ok := f[normalizeProvider(provider)]
	if !ok || credential.Type != "api_key" {
		return ""
	}
	return credential.Key
}

func (f File) Set(provider, key string) {
	f[normalizeProvider(provider)] = APIKeyCredential{Type: "api_key", Key: key}
}

func (f File) Delete(provider string) { delete(f, normalizeProvider(provider)) }

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "gemini":
		return "google"
	case "kimi":
		return "moonshot"
	case "dashscope":
		return "qwen"
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}
