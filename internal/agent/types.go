package agent

import "context"

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
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
	Type    string // delta, done, error
	Delta   string
	Message *Message
	Err     error
}

type StreamingProvider interface {
	Stream(context.Context, []Message, []ToolDefinition) (<-chan StreamEvent, error)
}

type Event struct{ Type, Detail string }
type EventSink func(Event)
