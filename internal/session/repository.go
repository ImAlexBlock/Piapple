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
	Thinking  string         `json:"thinkingLevel,omitempty"`
	Summary   string         `json:"summary,omitempty"`
}
type Repository struct {
	path    string
	header  Header
	entries []Entry
	leaf    *string
}

type Summary struct {
	Path     string
	ID       string
	Name     string
	CWD      string
	Modified time.Time
	Messages int
	Model    string
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

// AppendThinking records the active reasoning level in the session tree.
func (r *Repository) AppendThinking(level string) error {
	e := Entry{Type: "thinking_level_change", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Thinking: strings.TrimSpace(level)}
	if err := r.write(e); err != nil {
		return err
	}
	r.entries = append(r.entries, e)
	r.leaf = &r.entries[len(r.entries)-1].ID
	return nil
}

func (r *Repository) Thinking() string {
	for i := len(r.entries) - 1; i >= 0; i-- {
		if r.entries[i].Type == "thinking_level_change" && r.entries[i].Thinking != "" {
			return r.entries[i].Thinking
		}
	}
	return ""
}

// AppendCompaction marks a context boundary. Context() starts from this
// summary when rebuilding a resumed transcript.
func (r *Repository) AppendCompaction(summary string) error {
	e := Entry{Type: "compaction", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Summary: summary}
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
		switch e.Type {
		case "message":
			if e.Message != nil {
				out = append(out, *e.Message)
			}
		case "compaction":
			out = []agent.Message{{Role: "system", Content: "Previous conversation summary:\n" + e.Summary}}
		}
	}
	return out
}

func (r *Repository) Path() string     { return r.path }
func (r *Repository) Header() Header   { return r.header }
func (r *Repository) Entries() []Entry { return append([]Entry(nil), r.entries...) }

// Tree returns a compact human-readable representation of the current
// session's append-only entry tree. It is intentionally derived from entries,
// so it also works for sessions imported from another Pi-compatible client.
func (r *Repository) Tree() string {
	var b strings.Builder
	for _, entry := range r.entries {
		label := entry.Type
		switch entry.Type {
		case "message":
			if entry.Message != nil {
				label = "message/" + entry.Message.Role
			}
		case "model_change":
			label = "model " + entry.Provider + "/" + entry.ModelID
		case "session_info":
			label = "name " + entry.Name
		}
		fmt.Fprintf(&b, "%s  %s\n", entry.ID, label)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Export writes an exact JSONL copy of the session to path.
func (r *Repository) Export(path string) error {
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Clone creates a new session file containing the current entries. A fork
// records the source session ID in the new header; a clone is independent.
func (r *Repository) Clone(dir string, fork bool) (*Repository, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	id := newID()
	now := time.Now().UTC()
	path := filepath.Join(dir, now.Format("20060102_150405.000000000")+"_"+id+".jsonl")
	header := Header{Type: "session", Version: CurrentVersion, ID: id, Timestamp: now.Format(time.RFC3339Nano), CWD: r.header.CWD}
	if fork {
		header.ParentSession = r.header.ID
	}
	clone := &Repository{path: path, header: header, entries: append([]Entry(nil), r.entries...)}
	if len(clone.entries) > 0 {
		clone.leaf = &clone.entries[len(clone.entries)-1].ID
	}
	if err := clone.write(header); err != nil {
		return nil, err
	}
	for _, entry := range clone.entries {
		if err := clone.write(entry); err != nil {
			return nil, err
		}
	}
	return clone, nil
}

func List(dir string) ([]Summary, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return nil, err
	}
	out := make([]Summary, 0, len(files))
	for _, path := range files {
		r, openErr := Open(path)
		if openErr != nil {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			continue
		}
		provider, modelID, ok := r.Model()
		modelText := ""
		if ok {
			modelText = provider + "/" + modelID
		}
		out = append(out, Summary{Path: path, ID: r.header.ID, Name: r.Name(), CWD: r.header.CWD, Modified: info.ModTime(), Messages: len(r.Context()), Model: modelText})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out, nil
}

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
