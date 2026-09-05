package session

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"io"
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
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	ParentID         *string        `json:"parentId"`
	Timestamp        string         `json:"timestamp"`
	Message          *agent.Message `json:"message,omitempty"`
	Provider         string         `json:"provider,omitempty"`
	ModelID          string         `json:"modelId,omitempty"`
	Name             string         `json:"name,omitempty"`
	Thinking         string         `json:"thinkingLevel,omitempty"`
	Summary          string         `json:"summary,omitempty"`
	FirstKeptEntryID string         `json:"firstKeptEntryId,omitempty"`
	TokensBefore     int            `json:"tokensBefore,omitempty"`
	FromID           string         `json:"fromId,omitempty"`
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
	clean := strings.TrimLeft(filepath.Clean(cwd), `/\`)
	clean = strings.NewReplacer("\\", "-", "/", "-", ":", "-").Replace(clean)
	return filepath.Join(home, ".pi", "agent", "sessions", "--"+clean+"--")
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
	decoder := json.NewDecoder(f)
	var h Header
	if err = decoder.Decode(&h); err != nil {
		return nil, err
	}
	if h.Type != "session" {
		return nil, fmt.Errorf("session header missing")
	}
	r := &Repository{path: path, header: h}
	for {
		var entry Entry
		err = decoder.Decode(&entry)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if entry.Type == "session" {
			continue
		}
		if entry.ID == "" {
			entry.ID = newID()
		}
		r.entries = append(r.entries, entry)
		r.leaf = &r.entries[len(r.entries)-1].ID
	}
	r.migrate()
	return r, nil
}
func (r *Repository) migrate() {
	version := r.header.Version
	if version == 0 {
		version = 1
	}
	if version < 2 {
		var previous *string
		for i := range r.entries {
			if r.entries[i].ID == "" {
				r.entries[i].ID = newID()
			}
			r.entries[i].ParentID = previous
			previous = &r.entries[i].ID
		}
	}
	if version < 3 {
		for i := range r.entries {
			if r.entries[i].Message != nil && r.entries[i].Message.Role == "hookMessage" {
				r.entries[i].Message.Role = "custom"
			}
		}
	}
	if len(r.entries) > 0 {
		r.leaf = &r.entries[len(r.entries)-1].ID
	}
	if r.header.Version < CurrentVersion {
		r.header.Version = CurrentVersion
	}
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
	path := r.BranchEntries()
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].Type == "thinking_level_change" && path[i].Thinking != "" {
			return path[i].Thinking
		}
	}
	return ""
}

// AppendCompaction marks a context boundary. Context() starts from this
// summary when rebuilding a resumed transcript.
// AppendCompaction records a context boundary without retaining entries
// before it. Use AppendCompactionAt when a caller knows the first entry that
// should remain visible after compaction.
func (r *Repository) AppendCompaction(summary string) error {
	return r.AppendCompactionAt(summary, "", 0)
}

func (r *Repository) AppendCompactionAt(summary, firstKeptEntryID string, tokensBefore int) error {
	e := Entry{Type: "compaction", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), Summary: summary, FirstKeptEntryID: firstKeptEntryID, TokensBefore: tokensBefore}
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
	path := r.BranchEntries()
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].Type == "session_info" && path[i].Name != "" {
			return path[i].Name
		}
	}
	return ""
}

// Model returns the most recently selected model in the session, if one has
// been recorded. Entries are walked backwards because the session is an
// append-only log and later changes supersede earlier ones.
func (r *Repository) Model() (provider, modelID string, ok bool) {
	path := r.BranchEntries()
	for i := len(path) - 1; i >= 0; i-- {
		entry := path[i]
		if entry.Type == "model_change" && entry.Provider != "" && entry.ModelID != "" {
			return entry.Provider, entry.ModelID, true
		}
		if entry.Type == "message" && entry.Message != nil && entry.Message.Role == "assistant" && entry.Message.Provider != "" && entry.Message.Model != "" {
			return entry.Message.Provider, entry.Message.Model, true
		}
	}
	return "", "", false
}

// LeafID returns the entry currently selected for appends and context.
func (r *Repository) LeafID() string {
	if r.leaf == nil {
		return ""
	}
	return *r.leaf
}

// Branch selects an existing entry as the active leaf. Existing entries remain
// immutable; the next append creates a child of the selected entry.
func (r *Repository) Branch(entryID string) error {
	if entryID == "" {
		return fmt.Errorf("entry id is empty")
	}
	for i := range r.entries {
		if r.entries[i].ID == entryID {
			r.leaf = &r.entries[i].ID
			return nil
		}
	}
	return fmt.Errorf("entry %q not found", entryID)
}

// ResetLeaf starts a new root branch. This is used when navigating before the
// first message, and mirrors Pi's resetLeaf behavior.
func (r *Repository) ResetLeaf() { r.leaf = nil }

// GetEntry returns a defensive copy of an entry by id.
func (r *Repository) GetEntry(entryID string) (Entry, bool) {
	for _, entry := range r.entries {
		if entry.ID == entryID {
			return entry, true
		}
	}
	return Entry{}, false
}

// Children returns direct children in append order.
func (r *Repository) Children(parentID string) []Entry {
	out := make([]Entry, 0)
	for _, entry := range r.entries {
		if entry.ParentID != nil && *entry.ParentID == parentID {
			out = append(out, entry)
		}
	}
	return out
}

// BranchEntries walks from the selected leaf to the root and returns the path
// in prompt order. If entryID is supplied, that entry is used without changing
// the active leaf.
func (r *Repository) BranchEntries(entryID ...string) []Entry {
	leafID := r.LeafID()
	if len(entryID) > 0 {
		leafID = entryID[0]
	}
	if leafID == "" {
		return nil
	}
	byID := make(map[string]Entry, len(r.entries))
	for _, entry := range r.entries {
		byID[entry.ID] = entry
	}
	path := make([]Entry, 0)
	current, ok := byID[leafID]
	for ok {
		path = append(path, current)
		if current.ParentID == nil {
			break
		}
		current, ok = byID[*current.ParentID]
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// FirstRetainedMessageID returns the entry id of the message at the start of
// the last count active message entries. It lets compaction persist the same
// retained tail that the runtime keeps in memory.
func (r *Repository) FirstRetainedMessageID(count int) string {
	if count < 1 {
		return ""
	}
	path := r.BranchEntries()
	messages := make([]Entry, 0)
	for _, entry := range path {
		if entry.Type == "message" && entry.Message != nil {
			messages = append(messages, entry)
		}
	}
	if len(messages) == 0 {
		return ""
	}
	index := len(messages) - count
	if index < 0 {
		index = 0
	}
	return messages[index].ID
}

// AppendBranchSummary records a Pi-compatible branch summary under a selected
// historical node and makes the new summary the active leaf.
func (r *Repository) AppendBranchSummary(fromID, summary string) error {
	if fromID != "" {
		if _, ok := r.GetEntry(fromID); !ok {
			return fmt.Errorf("entry %q not found", fromID)
		}
		if err := r.Branch(fromID); err != nil {
			return err
		}
	}
	entry := Entry{Type: "branch_summary", ID: newID(), ParentID: r.leaf, Timestamp: time.Now().UTC().Format(time.RFC3339Nano), FromID: fromID, Summary: summary}
	if err := r.write(entry); err != nil {
		return err
	}
	r.entries = append(r.entries, entry)
	r.leaf = &r.entries[len(r.entries)-1].ID
	return nil
}

func (r *Repository) Context() []agent.Message {
	path := r.BranchEntries()
	if len(path) == 0 {
		return nil
	}
	latestCompaction := -1
	for i, entry := range path {
		if entry.Type == "compaction" {
			latestCompaction = i
		}
	}
	if latestCompaction >= 0 {
		compaction := path[latestCompaction]
		start := latestCompaction
		if compaction.FirstKeptEntryID != "" {
			for i := 0; i < latestCompaction; i++ {
				if path[i].ID == compaction.FirstKeptEntryID {
					start = i
					break
				}
			}
		}
		kept := append([]Entry(nil), path[start:latestCompaction]...)
		path = append([]Entry{{Type: "compaction", Summary: compaction.Summary}}, kept...)
		path = append(path, r.BranchEntries()[latestCompaction+1:]...)
	}
	out := make([]agent.Message, 0)
	for _, entry := range path {
		switch entry.Type {
		case "message":
			if entry.Message != nil {
				out = append(out, *entry.Message)
			}
		case "compaction":
			out = append(out, agent.Message{Role: "system", Content: "Previous conversation summary:\n" + entry.Summary})
		case "branch_summary":
			if entry.Summary != "" {
				out = append(out, agent.Message{Role: "user", Content: "The following is a summary of a branch that this conversation came back from:\n\n<summary>\n" + entry.Summary + "\n</summary>"})
			}
		}
	}
	return out
}

func (r *Repository) Path() string     { return r.path }
func (r *Repository) Header() Header   { return r.header }
func (r *Repository) Entries() []Entry { return append([]Entry(nil), r.entries...) }

// TreeItem is a flattened view of the session tree for a terminal picker.
type TreeItem struct {
	ID     string
	Label  string
	Depth  int
	Active bool
}

// TreeItems returns entries in deterministic depth-first order. It includes
// state entries (model changes, names, compactions) because Pi's tree picker
// navigates the complete append-only tree rather than only messages.
func (r *Repository) TreeItems() []TreeItem {
	byParent := make(map[string][]Entry)
	roots := make([]Entry, 0)
	for _, entry := range r.entries {
		if entry.ParentID == nil {
			roots = append(roots, entry)
		} else {
			byParent[*entry.ParentID] = append(byParent[*entry.ParentID], entry)
		}
	}
	label := func(entry Entry) string {
		name := entry.Type
		switch entry.Type {
		case "message":
			if entry.Message != nil {
				name = entry.Message.Role
				if entry.Message.Content != "" {
					name += ": " + strings.TrimSpace(strings.ReplaceAll(entry.Message.Content, "\n", " "))
				}
			}
		case "model_change":
			name = "model " + entry.Provider + "/" + entry.ModelID
		case "session_info":
			name = "name " + entry.Name
		case "branch_summary":
			name = "branch summary: " + entry.Summary
		case "compaction":
			name = "compaction: " + entry.Summary
		}
		return name
	}
	items := make([]TreeItem, 0, len(r.entries))
	var visit func([]Entry, int)
	visit = func(entries []Entry, depth int) {
		for _, entry := range entries {
			items = append(items, TreeItem{ID: entry.ID, Label: label(entry), Depth: depth, Active: entry.ID == r.LeafID()})
			visit(byParent[entry.ID], depth+1)
		}
	}
	visit(roots, 0)
	return items
}

// Tree returns a compact human-readable representation of the current
// session's append-only entry tree. It is intentionally derived from entries,
// so it also works for sessions imported from another Pi-compatible client.
func (r *Repository) Tree() string {
	if len(r.entries) == 0 {
		return ""
	}
	byParent := make(map[string][]Entry)
	roots := make([]Entry, 0)
	for _, entry := range r.entries {
		if entry.ParentID == nil {
			roots = append(roots, entry)
		} else {
			byParent[*entry.ParentID] = append(byParent[*entry.ParentID], entry)
		}
	}
	var b strings.Builder
	var visit func([]Entry, int)
	visit = func(entries []Entry, depth int) {
		for _, entry := range entries {
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
			case "branch_summary":
				label = "branch summary"
			case "compaction":
				label = "compaction " + entry.Summary
			}
			marker := " "
			if entry.ID == r.LeafID() {
				marker = ">"
			}
			fmt.Fprintf(&b, "%s%s %s  %s\n", strings.Repeat("  ", depth), marker, entry.ID, label)
			visit(byParent[entry.ID], depth+1)
		}
	}
	visit(roots, 0)
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
	pathEntries := r.BranchEntries()
	cloneEntries := make([]Entry, 0, len(pathEntries))
	idMap := make(map[string]string, len(pathEntries))
	for _, source := range pathEntries {
		idMap[source.ID] = newID()
	}
	for _, source := range pathEntries {
		entry := source
		entry.ID = idMap[source.ID]
		entry.ParentID = nil
		if source.ParentID != nil {
			if parent, ok := idMap[*source.ParentID]; ok {
				parentCopy := parent
				entry.ParentID = &parentCopy
			}
		}
		if source.Message != nil {
			message := *source.Message
			message.ToolCalls = append([]agent.ToolCall(nil), source.Message.ToolCalls...)
			entry.Message = &message
		}
		cloneEntries = append(cloneEntries, entry)
	}
	clone := &Repository{path: path, header: header, entries: cloneEntries}
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

// FindByID resolves an exact or unambiguous prefix session id in dir. Pi's
// command line accepts partial UUIDs, while exact paths remain supported by
// Open; keeping resolution here makes TUI, RPC, and CLI behavior consistent.
func FindByID(dir, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", fmt.Errorf("session id is empty")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if err != nil {
		return "", err
	}
	matches := make([]string, 0, 1)
	for _, path := range files {
		f, openErr := os.Open(path)
		if openErr != nil {
			continue
		}
		var h Header
		decodeErr := json.NewDecoder(f).Decode(&h)
		_ = f.Close()
		if decodeErr == nil && (h.ID == id || strings.HasPrefix(h.ID, id)) {
			matches = append(matches, path)
		}
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("session %q not found", id)
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("session id %q is ambiguous", id)
	}
	return matches[0], nil
}

func OpenByID(dir, id string) (*Repository, error) {
	path, err := FindByID(dir, id)
	if err != nil {
		return nil, err
	}
	return Open(path)
}
func sessionTimestamp(path string) time.Time {
	f, err := os.Open(path)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()
	var h Header
	if err := json.NewDecoder(f).Decode(&h); err != nil {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, h.Timestamp)
	if err != nil {
		return time.Time{}
	}
	return parsed
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
		ai := sessionTimestamp(files[i])
		aj := sessionTimestamp(files[j])
		if !ai.IsZero() && !aj.IsZero() && !ai.Equal(aj) {
			return ai.After(aj)
		}
		mi, _ := os.Stat(files[i])
		mj, _ := os.Stat(files[j])
		if mi != nil && mj != nil && !mi.ModTime().Equal(mj.ModTime()) {
			return mi.ModTime().After(mj.ModTime())
		}
		return files[i] > files[j]
	})
	return Open(files[0])
}
