package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

// Pi-compatible message wire types. Runtime messages intentionally stay small;
// these adapters keep provider-specific details out of the session file while
// preserving the content-block shape used by Pi v3 sessions.
type wireText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
type wireThinking struct {
	Type      string `json:"type"`
	Thinking  string `json:"thinking"`
	Signature string `json:"signature,omitempty"`
}
type wireToolCall struct {
	Type             string         `json:"type"`
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments"`
	ThoughtSignature string         `json:"thoughtSignature,omitempty"`
}
type wireImage struct {
	Type     string `json:"type"`
	Data     string `json:"data"`
	MimeType string `json:"mimeType"`
}

type wireUsage struct {
	Input       int      `json:"input"`
	Output      int      `json:"output"`
	CacheRead   int      `json:"cacheRead"`
	CacheWrite  int      `json:"cacheWrite"`
	TotalTokens int      `json:"totalTokens"`
	Cost        wireCost `json:"cost"`
}
type wireCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

type wireMessage struct {
	Role               string           `json:"role"`
	Content            json.RawMessage  `json:"content,omitempty"`
	API                string           `json:"api,omitempty"`
	Provider           string           `json:"provider,omitempty"`
	Model              string           `json:"model,omitempty"`
	Usage              *wireUsage       `json:"usage,omitempty"`
	StopReason         string           `json:"stopReason,omitempty"`
	ToolCallID         string           `json:"toolCallId,omitempty"`
	ToolName           string           `json:"toolName,omitempty"`
	IsError            bool             `json:"isError,omitempty"`
	Timestamp          int64            `json:"timestamp"`
	LegacyReasoning    string           `json:"reasoning,omitempty"`
	LegacyToolCalls    []agent.ToolCall `json:"tool_calls,omitempty"`
	LegacyStop         string           `json:"stop_reason,omitempty"`
	LegacyError        string           `json:"error_message,omitempty"`
	LegacyToolName     string           `json:"tool_name,omitempty"`
	LegacyIsError      bool             `json:"is_error,omitempty"`
	Command            string           `json:"command,omitempty"`
	ExitCode           *int             `json:"exitCode,omitempty"`
	Cancelled          bool             `json:"cancelled,omitempty"`
	Truncated          bool             `json:"truncated,omitempty"`
	FullOutputPath     string           `json:"fullOutputPath,omitempty"`
	ExcludeFromContext bool             `json:"excludeFromContext,omitempty"`
}

func runtimeToWire(m agent.Message) wireMessage {
	timestamp := m.Timestamp
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}
	out := wireMessage{Role: m.Role, ToolCallID: m.ToolCallID, ToolName: m.ToolName, IsError: m.IsError, Timestamp: timestamp, API: m.API, Provider: m.Provider, Model: m.Model, Command: m.Command, ExitCode: m.ExitCode, Cancelled: m.Cancelled, Truncated: m.Truncated, FullOutputPath: m.FullOutputPath, ExcludeFromContext: m.ExcludeFromContext}
	if m.Usage != nil {
		out.Usage = &wireUsage{Input: m.Usage.InputTokens, Output: m.Usage.OutputTokens, TotalTokens: m.Usage.TotalTokens}
	}
	switch m.Role {
	case "assistant":
		out.StopReason = m.StopReason
		if out.StopReason == "" {
			out.StopReason = "stop"
		}
		var blocks []any
		if m.Reasoning != "" {
			blocks = append(blocks, wireThinking{Type: "thinking", Thinking: m.Reasoning, Signature: m.ReasoningSignature})
		}
		if m.Content != "" {
			blocks = append(blocks, wireText{Type: "text", Text: m.Content})
		}
		for _, call := range m.ToolCalls {
			var args map[string]any
			if json.Unmarshal([]byte(call.Arguments), &args) != nil || args == nil {
				args = map[string]any{}
			}
			blocks = append(blocks, wireToolCall{Type: "toolCall", ID: call.ID, Name: call.Name, Arguments: args, ThoughtSignature: call.ThoughtSignature})
			out.StopReason = "toolUse"
		}
		out.Content, _ = json.Marshal(blocks)
	case "tool":
		out.Role = "toolResult"
		out.Content, _ = json.Marshal([]wireText{{Type: "text", Text: m.Content}})
		if !out.IsError {
			out.IsError = len(m.Content) >= 11 && m.Content[:11] == "tool error: "
		}
	case "bashExecution":
		// Pi stores shell execution as a first-class message with output
		// metadata instead of pretending it was authored by the user.
		out.Content = nil
	case "user":
		out.Content, _ = json.Marshal(m.Content)
	default:
		out.Content, _ = json.Marshal(m.Content)
	}
	return out
}

func wireToRuntime(raw json.RawMessage) (agent.Message, error) {
	var w wireMessage
	if err := json.Unmarshal(raw, &w); err != nil {
		return agent.Message{}, err
	}
	out := agent.Message{Role: w.Role, ToolCallID: w.ToolCallID, ToolName: w.ToolName, IsError: w.IsError, Reasoning: w.LegacyReasoning, ToolCalls: append([]agent.ToolCall(nil), w.LegacyToolCalls...), API: w.API, Provider: w.Provider, Model: w.Model, StopReason: w.StopReason, ErrorMessage: w.LegacyError, Timestamp: w.Timestamp, Command: w.Command, ExitCode: w.ExitCode, Cancelled: w.Cancelled, Truncated: w.Truncated, FullOutputPath: w.FullOutputPath, ExcludeFromContext: w.ExcludeFromContext}
	if out.StopReason == "" {
		out.StopReason = w.LegacyStop
	}
	if out.ToolName == "" {
		out.ToolName = w.LegacyToolName
	}
	if !out.IsError {
		out.IsError = w.LegacyIsError
	}
	if w.Role == "toolResult" {
		out.Role = "tool"
	}
	if w.Usage != nil {
		out.Usage = &agent.Usage{InputTokens: w.Usage.Input, OutputTokens: w.Usage.Output, TotalTokens: w.Usage.TotalTokens}
	}
	if len(w.Content) == 0 {
		return out, nil
	}
	var text string
	if json.Unmarshal(w.Content, &text) == nil {
		out.Content = text
		return out, nil
	}
	var blocks []json.RawMessage
	if err := json.Unmarshal(w.Content, &blocks); err != nil {
		return out, fmt.Errorf("invalid %s content: %w", w.Role, err)
	}
	for _, block := range blocks {
		var kind struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(block, &kind) != nil {
			continue
		}
		switch kind.Type {
		case "text":
			var v wireText
			if json.Unmarshal(block, &v) == nil {
				text += v.Text
			}
		case "thinking":
			var v wireThinking
			if json.Unmarshal(block, &v) == nil {
				out.Reasoning += v.Thinking
				if v.Signature != "" {
					out.ReasoningSignature = v.Signature
				}
			}
		case "toolCall":
			var v wireToolCall
			if json.Unmarshal(block, &v) == nil {
				args, _ := json.Marshal(v.Arguments)
				out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: v.ID, Name: v.Name, Arguments: string(args), ThoughtSignature: v.ThoughtSignature})
			}
		}
	}
	out.Content = text
	return out, nil
}

// MarshalJSON stores runtime messages as Pi's AgentMessage wire shape.
func (e Entry) MarshalJSON() ([]byte, error) {
	type entryAlias Entry
	aux := struct {
		entryAlias
		Message json.RawMessage `json:"message,omitempty"`
	}{entryAlias: entryAlias(e)}
	if e.Message != nil {
		raw, err := json.Marshal(runtimeToWire(*e.Message))
		if err != nil {
			return nil, err
		}
		aux.Message = raw
	}
	return json.Marshal(aux)
}

// UnmarshalJSON accepts both Pi content-block messages and the legacy
// Piapple runtime message shape, so existing sessions remain readable.
func (e *Entry) UnmarshalJSON(data []byte) error {
	type entryAlias Entry
	var aux struct {
		entryAlias
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*e = Entry(aux.entryAlias)
	if len(aux.Message) == 0 || string(aux.Message) == "null" {
		return nil
	}
	if msg, err := wireToRuntime(aux.Message); err == nil {
		e.Message = &msg
		return nil
	}
	var legacy agent.Message
	if err := json.Unmarshal(aux.Message, &legacy); err != nil {
		return err
	}
	e.Message = &legacy
	return nil
}
