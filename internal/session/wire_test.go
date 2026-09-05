package session

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

func TestMessageEntryUsesPiContentBlocks(t *testing.T) {
	r, err := Create(t.TempDir(), "/project")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.AppendMessage(agent.Message{Role: "assistant", Reasoning: "plan", ReasoningSignature: "sig", Content: "done", ToolCalls: []agent.ToolCall{{ID: "call-1", Name: "read", Arguments: `{"path":"x"}`, ThoughtSignature: "thought-sig"}}}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"role":"assistant"`)) || !bytes.Contains(raw, []byte(`"type":"toolCall"`)) || bytes.Contains(raw, []byte(`"tool_calls"`)) {
		t.Fatalf("not pi wire format: %s", raw)
	}
	loaded, err := Open(r.Path())
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.Context()[0]
	if got.Content != "done" || got.Reasoning != "plan" || got.ReasoningSignature != "sig" || len(got.ToolCalls) != 1 || got.ToolCalls[0].Name != "read" || got.ToolCalls[0].ThoughtSignature != "thought-sig" {
		t.Fatalf("round trip: %#v", got)
	}
}

func TestWireMessageAcceptsPiAssistantFixture(t *testing.T) {
	raw, _ := json.Marshal(wireMessage{Role: "assistant", Content: json.RawMessage(`[{"type":"text","text":"hello"},{"type":"thinking","thinking":"thought"}]`), Timestamp: 1})
	got, err := wireToRuntime(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "hello" || got.Reasoning != "thought" {
		t.Fatalf("got=%#v", got)
	}
}

func TestWireMessagePreservesProviderSignatures(t *testing.T) {
	raw := json.RawMessage(`{"role":"assistant","content":[{"type":"thinking","thinking":"plan","signature":"sig"},{"type":"toolCall","id":"call-1","name":"read","arguments":{"path":"x"},"thoughtSignature":"thought-sig"}],"timestamp":1}`)
	got, err := wireToRuntime(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningSignature != "sig" || len(got.ToolCalls) != 1 || got.ToolCalls[0].ThoughtSignature != "thought-sig" {
		t.Fatalf("got=%#v", got)
	}
}
