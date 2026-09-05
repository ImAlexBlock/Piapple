package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Loop struct {
	Provider Provider
	Tools    map[string]Tool
	MaxSteps int
	OnEvent  EventSink
	eventMu  sync.RWMutex
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
	names := make([]string, 0, len(l.Tools))
	for name := range l.Tools {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, l.Tools[name].Definition())
	}
	return out
}

func (l *Loop) emit(kind, detail string) {
	l.eventMu.RLock()
	sink := l.OnEvent
	l.eventMu.RUnlock()
	if sink != nil {
		sink(Event{Type: kind, Detail: detail})
	}
}

// SetEventSink replaces the event callback and returns a restore function. It
// is used by TUI/RPC adapters while a stream is active; unlike direct field
// assignment it does not race with emit when a provider finishes on another
// goroutine.
func (l *Loop) SetEventSink(sink EventSink) func() {
	l.eventMu.Lock()
	previous := l.OnEvent
	l.OnEvent = sink
	l.eventMu.Unlock()
	return func() {
		l.eventMu.Lock()
		l.OnEvent = previous
		l.eventMu.Unlock()
	}
}

func (l *Loop) receiveStream(ctx context.Context, provider StreamingProvider, transcript []Message) (Message, error) {
	stream, err := provider.Stream(ctx, transcript, l.definitions())
	if err != nil {
		return Message{}, err
	}
	for event := range stream {
		switch event.Type {
		case "delta":
			if event.Delta != "" {
				l.emit("model_delta", event.Delta)
			}
		case "reasoning_delta":
			if event.Delta != "" {
				l.emit("reasoning_delta", event.Delta)
			}
		case "error":
			if event.Err != nil {
				return Message{}, event.Err
			}
		case "done":
			if event.Message != nil {
				return *event.Message, nil
			}
		}
	}
	return Message{}, fmt.Errorf("provider stream ended without a final message")
}

func (l *Loop) Run(ctx context.Context, transcript []Message) ([]Message, string, error) {
	if l.Provider == nil {
		return transcript, "", fmt.Errorf("no model provider is configured")
	}
	for step := 0; step < l.MaxSteps; step++ {
		if err := ctx.Err(); err != nil {
			return transcript, "", err
		}
		l.emit("model_request", fmt.Sprintf("step %d", step+1))
		var reply Message
		var err error
		if streaming, ok := l.Provider.(StreamingProvider); ok {
			reply, err = l.receiveStream(ctx, streaming, transcript)
		} else {
			reply, err = l.Provider.Complete(ctx, transcript, l.definitions())
		}
		if err != nil {
			return transcript, "", err
		}
		if reply.Role == "" {
			reply.Role = "assistant"
		}
		if reply.StopReason == "" {
			if len(reply.ToolCalls) > 0 {
				reply.StopReason = "toolUse"
			} else {
				reply.StopReason = "stop"
			}
		}
		transcript = append(transcript, reply)
		if len(reply.ToolCalls) == 0 {
			l.emit("model_end", "")
			return transcript, reply.Content, nil
		}
		for _, call := range reply.ToolCalls {
			l.emit("tool_start", call.Name)
			result := ""
			tool, ok := l.Tools[call.Name]
			if !ok {
				result = fmt.Sprintf("tool %q is not available", call.Name)
			} else {
				var arguments map[string]json.RawMessage
				if err := json.Unmarshal([]byte(call.Arguments), &arguments); err != nil || arguments == nil {
					result = "invalid tool arguments: expected JSON object"
				} else if missing := missingRequiredArguments(tool.Definition(), arguments); missing != "" {
					result = "invalid tool arguments: missing required field " + missing
				} else {
					value, toolErr := tool.Execute(ctx, call.Arguments)
					if toolErr != nil {
						result = "tool error: " + toolErr.Error()
					} else {
						result = value
					}
				}
			}
			transcript = append(transcript, Message{Role: "tool", Content: result, ToolCallID: call.ID, ToolName: call.Name, IsError: strings.HasPrefix(result, "tool error:") || strings.HasPrefix(result, "invalid tool arguments:") || strings.HasPrefix(result, "tool ")})
			l.emit("tool_end", call.Name)
		}
	}
	return transcript, "", fmt.Errorf("agent stopped after %d tool rounds", l.MaxSteps)
}

func missingRequiredArguments(def ToolDefinition, arguments map[string]json.RawMessage) string {
	required, ok := def.Parameters["required"].([]string)
	if !ok {
		if values, ok2 := def.Parameters["required"].([]any); ok2 {
			for _, value := range values {
				if name, ok3 := value.(string); ok3 {
					required = append(required, name)
				}
			}
		}
	}
	for _, name := range required {
		raw, exists := arguments[name]
		if !exists || string(raw) == "null" {
			return name
		}
	}
	return ""
}

// SetThinking forwards the interactive reasoning preference to providers that
// expose a native thinking/reasoning parameter. Providers may ignore levels
// they do not support while retaining the selected value for later requests.
func (l *Loop) SetThinking(level string) {
	if provider, ok := l.Provider.(interface{ SetThinking(string) }); ok {
		provider.SetThinking(level)
	}
}
