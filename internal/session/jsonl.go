package session

import (
	"bufio"
	"encoding/json"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"os"
	"path/filepath"
)

func Load(path string) ([]agent.Message, error) {
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []agent.Message
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		var m agent.Message
		if err := json.Unmarshal(scanner.Bytes(), &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, scanner.Err()
}
func Append(path string, messages []agent.Message) error {
	if path == "" || len(messages) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, m := range messages {
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if _, err = f.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}
