package agent

import "context"

type Usage struct {
	InputTokens      int     `json:"input_tokens,omitempty"`
	OutputTokens     int     `json:"output_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	InputCost        float64 `json:"input_cost,omitempty"`
	OutputCost       float64 `json:"output_cost,omitempty"`
	TotalCost        float64 `json:"total_cost,omitempty"`
}

type Message struct {
	Role               string     `json:"role"`
	Name               string     `json:"name,omitempty"`
	Reasoning          string     `json:"reasoning,omitempty"`
	ReasoningSignature string     `json:"reasoning_signature,omitempty"`
	Content            string     `json:"content,omitempty"`
	API                string     `json:"api,omitempty"`
	Provider           string     `json:"provider,omitempty"`
	Model              string     `json:"model,omitempty"`
	StopReason         string     `json:"stop_reason,omitempty"`
	ErrorMessage       string     `json:"error_message,omitempty"`
	Timestamp          int64      `json:"timestamp,omitempty"`
	Usage              *Usage     `json:"usage,omitempty"`
	ToolCallID         string     `json:"tool_call_id,omitempty"`
	ToolName           string     `json:"tool_name,omitempty"`
	IsError            bool       `json:"is_error,omitempty"`
	Command            string     `json:"command,omitempty"`
	ExitCode           *int       `json:"exit_code,omitempty"`
	Cancelled          bool       `json:"cancelled,omitempty"`
	Truncated          bool       `json:"truncated,omitempty"`
	FullOutputPath     string     `json:"full_output_path,omitempty"`
	ExcludeFromContext bool       `json:"exclude_from_context,omitempty"`
	ToolCalls          []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Tool interface {
	Definition() ToolDefinition
	Execute(context.Context, string) (string, error)
}

type Provider interface {
	Complete(context.Context, []Message, []ToolDefinition) (Message, error)
}

// StreamEvent is the provider-neutral incremental response protocol. Delta
// events are emitted as text arrives; done contains the complete assistant
// message, including any accumulated tool calls.
type StreamEvent struct {
	Type    string // delta, reasoning_delta, done, error
	Delta   string
	Message *Message
	Err     error
}

type StreamingProvider interface {
	Stream(context.Context, []Message, []ToolDefinition) (<-chan StreamEvent, error)
}

type Event struct{ Type, Detail string }
type EventSink func(Event)
