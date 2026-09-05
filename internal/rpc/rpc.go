// Package rpc implements Pi's newline-delimited headless control protocol.
// It deliberately exposes only compiled Piapple capabilities: there is no
// extension/plugin dispatch in this package.
package rpc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/commands"
	"github.com/ImAlexBlock/Piapple/internal/models"
)

type Request struct {
	ID                 string `json:"id,omitempty"`
	Type               string `json:"type"`
	Message            string `json:"message,omitempty"`
	Provider           string `json:"provider,omitempty"`
	ModelID            string `json:"modelId,omitempty"`
	Level              string `json:"level,omitempty"`
	Command            string `json:"command,omitempty"`
	SessionPath        string `json:"sessionPath,omitempty"`
	EntryID            string `json:"entryId,omitempty"`
	Name               string `json:"name,omitempty"`
	ExcludeFromContext bool   `json:"excludeFromContext,omitempty"`
}

type State struct {
	Provider            string `json:"provider,omitempty"`
	Model               string `json:"model,omitempty"`
	ThinkingLevel       string `json:"thinkingLevel,omitempty"`
	IsStreaming         bool   `json:"isStreaming"`
	SessionFile         string `json:"sessionFile,omitempty"`
	SessionID           string `json:"sessionId,omitempty"`
	SessionName         string `json:"sessionName,omitempty"`
	MessageCount        int    `json:"messageCount"`
	PendingMessageCount int    `json:"pendingMessageCount"`
}

type TreeNode struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Depth  int    `json:"depth"`
	Active bool   `json:"active"`
}

type response struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Command string `json:"command"`
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

type event struct {
	Type   string `json:"type"`
	Event  string `json:"event"`
	Detail string `json:"detail,omitempty"`
}

// Server is a small stateful RPC adapter around the same agent loop used by
// the TUI and print modes. Callbacks keep session/model storage in the main
// package without introducing a second implementation of those features.
type Server struct {
	Loop          *agent.Loop
	Transcript    []agent.Message
	Models        []models.Model
	Commands      []commands.Definition
	State         func() State
	Persist       func([]agent.Message) error
	SetModel      func(provider, model string) error
	SetThinking   func(string) error
	NewSession    func() error
	SwitchSession func(string) ([]agent.Message, error)
	ForkSession   func(string) ([]agent.Message, error)
	CloneSession  func() ([]agent.Message, error)
	SetName       func(string) error
	Compact       func(string) error
	Shell         func(context.Context, string) (string, error)
	Tree          func() []TreeNode
	Entries       func() any

	mu     sync.Mutex
	busy   bool
	cancel context.CancelFunc
	write  sync.Mutex
	out    io.Writer
}

// Serve consumes JSON objects until EOF. Prompt execution is asynchronous so
// an abort request can arrive while a model or tool call is running.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	if in == nil || out == nil {
		return fmt.Errorf("rpc input and output are required")
	}
	s.out = out
	s.Commands = append([]commands.Definition(nil), s.Commands...)
	if len(s.Commands) == 0 {
		s.Commands = append([]commands.Definition(nil), commands.Builtins...)
	}
	var workers sync.WaitGroup
	scanner := bufio.NewScanner(in)
	// Prompts and tool output can contain large JSON strings. Keep the scanner
	// bounded but well above a normal terminal message.
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var request Request
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			s.writeResponse(response{Type: "response", Command: "parse", Success: false, Error: err.Error()})
			continue
		}
		if request.Type == "prompt" {
			workers.Add(1)
			go func(req Request) {
				defer workers.Done()
				s.handlePrompt(ctx, req)
			}(request)
			continue
		}
		s.handle(ctx, request)
	}
	workers.Wait()
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func (s *Server) handlePrompt(parent context.Context, request Request) {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: false, Error: "agent is already running"})
		return
	}
	if s.Loop == nil || s.Loop.Provider == nil {
		s.mu.Unlock()
		s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: false, Error: "no model provider is configured"})
		return
	}
	before := len(s.Transcript)
	transcript := append([]agent.Message(nil), s.Transcript...)
	transcript = append(transcript, agent.Message{Role: "user", Content: request.Message, Timestamp: time.Now().UnixMilli()})
	loop := s.Loop
	promptCtx, cancel := context.WithCancel(parent)
	s.busy, s.cancel = true, cancel
	restoreSink := loop.SetEventSink(func(e agent.Event) {
		s.writeEvent(e)
	})
	s.mu.Unlock()

	updated, answer, err := loop.Run(promptCtx, transcript)
	cancel()

	s.mu.Lock()
	restoreSink()
	s.busy, s.cancel = false, nil
	if err == nil {
		s.Transcript = updated
	}
	s.mu.Unlock()
	if err != nil {
		s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: false, Error: err.Error()})
		return
	}
	if s.Persist != nil {
		if persistErr := s.Persist(updated[before:]); persistErr != nil {
			s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: false, Error: persistErr.Error()})
			return
		}
	}
	s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: true, Data: map[string]any{"answer": answer}})
}

func (s *Server) handle(ctx context.Context, request Request) {
	switch request.Type {
	case "abort":
		s.mu.Lock()
		if s.cancel != nil {
			s.cancel()
		}
		s.mu.Unlock()
		s.success(request, nil)
	case "clear_queue":
		s.success(request, nil)
	case "get_state":
		state := s.state()
		s.success(request, state)
	case "get_available_models":
		s.success(request, map[string]any{"models": s.Models})
	case "set_model":
		if s.SetModel == nil {
			s.failure(request, "model selection is unavailable")
			return
		}
		if err := s.SetModel(request.Provider, request.ModelID); err != nil {
			s.failure(request, err.Error())
			return
		}
		s.success(request, map[string]any{"provider": request.Provider, "modelId": request.ModelID})
	case "cycle_model":
		s.cycleModel(request)
	case "get_available_thinking_levels":
		s.success(request, map[string]any{"levels": []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}})
	case "set_thinking_level":
		if s.SetThinking == nil {
			s.failure(request, "thinking level control is unavailable")
			return
		}
		if err := s.SetThinking(request.Level); err != nil {
			s.failure(request, err.Error())
			return
		}
		s.success(request, nil)
	case "cycle_thinking_level":
		s.cycleThinking(request)
	case "new_session":
		if s.NewSession == nil {
			s.failure(request, "new session is unavailable")
			return
		}
		if err := s.NewSession(); err != nil {
			s.failure(request, err.Error())
			return
		}
		s.mu.Lock()
		s.Transcript = nil
		s.mu.Unlock()
		s.success(request, nil)
	case "switch_session":
		if s.SwitchSession == nil {
			s.failure(request, "session switching is unavailable")
			return
		}
		messages, err := s.SwitchSession(request.SessionPath)
		if err != nil {
			s.failure(request, err.Error())
			return
		}
		s.mu.Lock()
		s.Transcript = messages
		s.mu.Unlock()
		s.success(request, map[string]any{"cancelled": false})
	case "fork":
		if s.ForkSession == nil {
			s.failure(request, "session fork is unavailable")
			return
		}
		messages, err := s.ForkSession(request.EntryID)
		if err != nil {
			s.failure(request, err.Error())
			return
		}
		s.mu.Lock()
		s.Transcript = messages
		s.mu.Unlock()
		s.success(request, map[string]any{"cancelled": false})
	case "clone":
		if s.CloneSession == nil {
			s.failure(request, "session clone is unavailable")
			return
		}
		messages, err := s.CloneSession()
		if err != nil {
			s.failure(request, err.Error())
			return
		}
		s.mu.Lock()
		s.Transcript = messages
		s.mu.Unlock()
		s.success(request, map[string]any{"cancelled": false})
	case "set_session_name":
		if s.SetName == nil {
			s.failure(request, "session naming is unavailable")
			return
		}
		if err := s.SetName(request.Name); err != nil {
			s.failure(request, err.Error())
			return
		}
		s.success(request, nil)
	case "compact":
		if s.Compact == nil {
			s.failure(request, "context compaction is unavailable")
			return
		}
		if err := s.Compact(request.Message); err != nil {
			s.failure(request, err.Error())
			return
		}
		s.success(request, nil)
	case "bash":
		s.handleBash(ctx, request)
	case "get_messages":
		s.mu.Lock()
		messages := append([]agent.Message(nil), s.Transcript...)
		s.mu.Unlock()
		s.success(request, map[string]any{"messages": messages})
	case "get_entries":
		if s.Entries == nil {
			s.success(request, map[string]any{"entries": []any{}})
			return
		}
		s.success(request, map[string]any{"entries": s.Entries()})
	case "get_tree":
		if s.Tree == nil {
			s.success(request, map[string]any{"tree": []TreeNode{}})
			return
		}
		s.success(request, map[string]any{"tree": s.Tree()})
	case "get_last_assistant_text":
		s.mu.Lock()
		text := ""
		for i := len(s.Transcript) - 1; i >= 0; i-- {
			if s.Transcript[i].Role == "assistant" && s.Transcript[i].Content != "" {
				text = s.Transcript[i].Content
				break
			}
		}
		s.mu.Unlock()
		s.success(request, map[string]any{"text": text})
	case "get_commands":
		s.success(request, map[string]any{"commands": s.Commands})
	default:
		s.failure(request, fmt.Sprintf("unknown RPC command %q", request.Type))
	}
}

func (s *Server) handleBash(ctx context.Context, request Request) {
	if s.Shell == nil {
		s.failure(request, "shell execution is unavailable")
		return
	}
	output, err := s.Shell(ctx, request.Command)
	message := agent.Message{Role: "bashExecution", Command: request.Command, Content: output, ExcludeFromContext: request.ExcludeFromContext, Timestamp: time.Now().UnixMilli()}
	s.mu.Lock()
	s.Transcript = append(s.Transcript, message)
	s.mu.Unlock()
	if s.Persist != nil {
		if persistErr := s.Persist([]agent.Message{message}); persistErr != nil {
			s.failure(request, persistErr.Error())
			return
		}
	}
	if err != nil {
		s.failure(request, err.Error())
		return
	}
	s.success(request, map[string]any{"output": output, "exitCode": 0, "cancelled": false})
}

func (s *Server) cycleModel(request Request) {
	if s.SetModel == nil || len(s.Models) == 0 {
		s.failure(request, "no models are available")
		return
	}
	state := s.state()
	index := -1
	for i, item := range s.Models {
		if item.Provider == state.Provider && item.ID == state.Model {
			index = i
			break
		}
	}
	item := s.Models[(index+1+len(s.Models))%len(s.Models)]
	if err := s.SetModel(item.Provider, item.ID); err != nil {
		s.failure(request, err.Error())
		return
	}
	s.success(request, map[string]any{"model": item, "thinkingLevel": state.ThinkingLevel})
}

func (s *Server) cycleThinking(request Request) {
	if s.SetThinking == nil {
		s.failure(request, "thinking level control is unavailable")
		return
	}
	levels := []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"}
	state := s.state()
	index := -1
	for i, level := range levels {
		if level == state.ThinkingLevel {
			index = i
			break
		}
	}
	level := levels[(index+1+len(levels))%len(levels)]
	if err := s.SetThinking(level); err != nil {
		s.failure(request, err.Error())
		return
	}
	s.success(request, map[string]any{"level": level})
}

func (s *Server) state() State {
	var state State
	if s.State != nil {
		state = s.State()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state.IsStreaming = s.busy
	state.PendingMessageCount = 0
	state.MessageCount = len(s.Transcript)
	return state
}

func (s *Server) success(request Request, data any) {
	s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: true, Data: data})
}

func (s *Server) failure(request Request, message string) {
	s.writeResponse(response{ID: request.ID, Type: "response", Command: request.Type, Success: false, Error: message})
}

func (s *Server) writeEvent(value agent.Event) {
	s.writeJSON(event{Type: "event", Event: value.Type, Detail: value.Detail})
}

func (s *Server) writeResponse(value response) { s.writeJSON(value) }

func (s *Server) writeJSON(value any) {
	raw, err := json.Marshal(value)
	if err != nil || s.out == nil {
		return
	}
	s.write.Lock()
	defer s.write.Unlock()
	_, _ = fmt.Fprintln(s.out, string(raw))
}
