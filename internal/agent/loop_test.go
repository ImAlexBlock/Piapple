package agent_test

import (
	"context"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"testing"
)

type fakeProvider struct {
	replies []agent.Message
	calls   int
}

func (p *fakeProvider) Complete(_ context.Context, _ []agent.Message, _ []agent.ToolDefinition) (agent.Message, error) {
	reply := p.replies[p.calls]
	p.calls++
	return reply, nil
}

type fakeTool struct{}

func (fakeTool) Definition() agent.ToolDefinition {
	return agent.ToolDefinition{Name: "echo", Parameters: map[string]any{"type": "object"}}
}
func (fakeTool) Execute(_ context.Context, raw string) (string, error) { return "executed " + raw, nil }
func TestLoopFeedsToolResultBackIntoTranscript(t *testing.T) {
	p := &fakeProvider{replies: []agent.Message{{Role: "assistant", ToolCalls: []agent.ToolCall{{ID: "call-1", Name: "echo", Arguments: `{"value":"ok"}`}}}, {Role: "assistant", Content: "done"}}}
	loop := agent.NewLoop(p, []agent.Tool{fakeTool{}}, 3, nil)
	messages, answer, err := loop.Run(context.Background(), []agent.Message{{Role: "user", Content: "test"}})
	if err != nil {
		t.Fatal(err)
	}
	if answer != "done" {
		t.Fatalf("answer=%q", answer)
	}
	if p.calls != 2 {
		t.Fatalf("calls=%d", p.calls)
	}
	if len(messages) != 4 || messages[2].Role != "tool" || messages[2].Content != "executed {\"value\":\"ok\"}" {
		t.Fatalf("unexpected transcript: %#v", messages)
	}
}
func TestLoopRejectsInvalidArguments(t *testing.T) {
	p := &fakeProvider{replies: []agent.Message{{ToolCalls: []agent.ToolCall{{ID: "bad", Name: "echo", Arguments: "nope"}}}, {Content: "recovered"}}}
	loop := agent.NewLoop(p, []agent.Tool{fakeTool{}}, 2, nil)
	messages, answer, err := loop.Run(context.Background(), nil)
	if err != nil || answer != "recovered" {
		t.Fatalf("answer=%q err=%v", answer, err)
	}
	if messages[1].Content != "invalid tool arguments: expected JSON object" {
		t.Fatal(messages[1].Content)
	}
}

type fakeStreamingProvider struct{}

func (fakeStreamingProvider) Complete(context.Context, []agent.Message, []agent.ToolDefinition) (agent.Message, error) {
	return agent.Message{}, nil
}
func (fakeStreamingProvider) Stream(context.Context, []agent.Message, []agent.ToolDefinition) (<-chan agent.StreamEvent, error) {
	ch := make(chan agent.StreamEvent, 2)
	ch <- agent.StreamEvent{Type: "delta", Delta: "hi"}
	ch <- agent.StreamEvent{Type: "done", Message: &agent.Message{Role: "assistant", Content: "hi"}}
	close(ch)
	return ch, nil
}
func TestLoopConsumesStreamingProvider(t *testing.T) {
	var delta string
	loop := agent.NewLoop(fakeStreamingProvider{}, nil, 1, func(event agent.Event) {
		if event.Type == "model_delta" {
			delta += event.Detail
		}
	})
	_, answer, err := loop.Run(context.Background(), []agent.Message{{Role: "user", Content: "hello"}})
	if err != nil || answer != "hi" || delta != "hi" {
		t.Fatalf("answer=%q delta=%q err=%v", answer, delta, err)
	}
}
