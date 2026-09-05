package agent

import (
	"context"
	"encoding/json"
	"fmt"
)

type Loop struct {
	Provider Provider
	Tools    map[string]Tool
	MaxSteps int
	OnEvent  EventSink
}

func NewLoop(provider Provider, tools []Tool, maxSteps int, sink EventSink) *Loop {
	registry := make(map[string]Tool, len(tools))
	for _, tool := range tools {
		registry[tool.Definition().Name] = tool
	}
	if maxSteps < 1 {
		maxSteps = 12
	}
	return &Loop{Provider: provider, Tools: registry, MaxSteps: maxSteps, OnEvent: sink}
}

func (l *Loop) definitions() []ToolDefinition {
	out := make([]ToolDefinition, 0, len(l.Tools))
	for _, tool := range l.Tools {
		out = append(out, tool.Definition())
	}
	return out
}

func (l *Loop) emit(kind, detail string) {
	if l.OnEvent != nil {
		l.OnEvent(Event{Type: kind, Detail: detail})
	}
}

func (l *Loop) Run(ctx context.Context, transcript []Message) ([]Message, string, error) {
	for step := 0; step < l.MaxSteps; step++ {
		l.emit("model_request", fmt.Sprintf("step %d", step+1))
		reply, err := l.Provider.Complete(ctx, transcript, l.definitions())
		if err != nil {
			return transcript, "", err
		}
		if reply.Role == "" {
			reply.Role = "assistant"
		}
		transcript = append(transcript, reply)
		if len(reply.ToolCalls) == 0 {
			return transcript, reply.Content, nil
		}
		for _, call := range reply.ToolCalls {
			l.emit("tool_start", call.Name)
			result := ""
			tool, ok := l.Tools[call.Name]
			if !ok {
				result = fmt.Sprintf("tool %q is not available", call.Name)
			} else {
				if !json.Valid([]byte(call.Arguments)) {
					result = "invalid tool arguments: expected JSON object"
				} else {
					value, toolErr := tool.Execute(ctx, call.Arguments)
					if toolErr != nil {
						result = "tool error: " + toolErr.Error()
					} else {
						result = value
					}
				}
			}
			transcript = append(transcript, Message{Role: "tool", Content: result, ToolCallID: call.ID})
			l.emit("tool_end", call.Name)
		}
	}
	return transcript, "", fmt.Errorf("agent stopped after %d tool rounds", l.MaxSteps)
}
