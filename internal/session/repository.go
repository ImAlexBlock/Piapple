package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const CurrentVersion = 3

type Header struct {
	Type          string `json:"type"`
	Version       int    `json:"version"`
	ID            string `json:"id"`
	Timestamp     string `json:"timestamp"`
	CWD           string `json:"cwd"`
	ParentSession string `json:"parentSession,omitempty"`
}
type Entry struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	ParentID  *string        `json:"parentId"`
	Timestamp string         `json:"timestamp"`
	Message   *agent.Message `json:"message,omitempty"`
	Provider  string         `json:"provider,omitempty"`
	ModelID   string         `json:"modelId,omitempty"`
	Name      string         `json:"name,omitempty"`
}
type Repository struct {
	path    string
	header  Header
	entries []Entry
	leaf    *string
}

func newID() string { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }
func DefaultDirectory(home, cwd string) string {
	clean := strings.ReplaceAll(filepath.Clean(cwd), ":", "")
	clean = strings.NewReplacer("\\", "-", "/", "-").Replace(clean)
	return filepath.Join(home, ".piapple", "agent", "sessions", "--"+clean+"--")
}
func Create(dir, cwd string) (*Repository, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	id := newID()
	now := time.Now().UTC()
	// Include nanoseconds so session ordering remains deterministic even when
	// the filesystem reports coarse modification times.
	path := filepath.Join(dir, now.Format("20060102_150405.000000000")+"_"+id+".jsonl")
	h := Header{Type: "session", Version: CurrentVersion, ID: id, Timestamp: now.Format(time.RFC3339Nano), CWD: cwd}
	r := &Repository{path: path, header: h}
	if err := r.write(h); err != nil {
		return nil, err
	}
	return r, nil
}
func Open(path string) (*Repository, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var h Header
	if err = dec.Decode(&h); err != nil {
		return nil, err
	}
	if h.Type != "session" {
		return nil, fmt.Errorf("session header missing")
	}
	r := &Repository{path: path, header: h}
	for dec.More() {
		var e Entry
		if err = dec.Decode(&e); err != nil {
			return nil, err
		}
		r.entries = append(r.entries, e)
		r.leaf = &r.entries[len(r.entries)-1].ID
	}
	return r, nil
}
func (r *Repository) write(value any) error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = f.Write(append(raw, '\n'))
	return err
}
func (r *Repository) AppendMessage(m agent.Message) error {
	e := Entry{Type: "message", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Message: &m}
	if err := r.write(e); err != nil {
		return err
	}
	r.entries = append(r.entries, e)
	r.leaf = &r.entries[len(r.entries)-1].ID
	return nil
}

// AppendModelChange records the model selected for this session. Pi keeps
// model changes in the session tree rather than only in process memory, so a
// resumed session can restore the last model selected in that session.
func (r *Repository) AppendModelChange(provider, modelID string) error {
	e := Entry{Type: "model_change", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Provider: provider, ModelID: modelID}
	if err := r.write(e); err != nil {
		return err
	}
	r.entries = append(r.entries, e)
	r.leaf = &r.entries[len(r.entries)-1].ID
	return nil
}

// AppendName records a display name without rewriting earlier session entries.
func (r *Repository) AppendName(name string) error {
	e := Entry{Type: "session_info", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Name: strings.TrimSpace(name)}
	if err := r.write(e); err != nil {
		return err
	}
	r.entries = append(r.entries, e)
	r.leaf = &r.entries[len(r.entries)-1].ID
	return nil
}

func (r *Repository) Name() string {
	for i := len(r.entries) - 1; i >= 0; i-- {
		if r.entries[i].Type == "session_info" && r.entries[i].Name != "" {
			return r.entries[i].Name
		}
	}
	return ""
}

// Model returns the most recently selected model in the session, if one has
// been recorded. Entries are walked backwards because the session is an
// append-only log and later changes supersede earlier ones.
func (r *Repository) Model() (provider, modelID string, ok bool) {
	for i := len(r.entries) - 1; i >= 0; i-- {
		entry := r.entries[i]
		if entry.Type == "model_change" && entry.Provider != "" && entry.ModelID != "" {
			return entry.Provider, entry.ModelID, true
		}
	}
	return "", "", false
}
func (r *Repository) Context() []agent.Message {
	out := []agent.Message{}
	for _, e := range r.entries {
		if e.Type == "message" && e.Message != nil {
			out = append(out, *e.Message)
		}
	}
	return out
}
func (r *Repository) Path() string     { return r.path }
func (r *Repository) Header() Header   { return r.header }
func (r *Repository) Entries() []Entry { return append([]Entry(nil), r.entries...) }
func Continue(dir string) (*Repository, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, os.ErrNotExist
	}
	sort.Slice(files, func(i, j int) bool {
		a, _ := os.Stat(files[i])
		b, _ := os.Stat(files[j])
		if a.ModTime().Equal(b.ModTime()) {
			// Some filesystems expose coarse mtime precision. Create() uses a
			// sortable timestamp prefix, so use the path as a deterministic tie
			// breaker instead of returning an arbitrary session.
			return files[i] > files[j]
		}
		return a.ModTime().After(b.ModTime())
	})
	return Open(files[0])
}
