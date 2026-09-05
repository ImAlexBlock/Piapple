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

type Event struct{ Type, Detail string }
type EventSink func(Event)
