package rpc

import (
	"context"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/agent"
	"github.com/ImAlexBlock/Piapple/internal/models"
)

type fakeProvider struct{}

func (fakeProvider) Complete(context.Context, []agent.Message, []agent.ToolDefinition) (agent.Message, error) {
	return agent.Message{Role: "assistant", Content: "done"}, nil
}

func TestServePromptAndState(t *testing.T) {
	var out strings.Builder
	server := &Server{Loop: agent.NewLoop(fakeProvider{}, nil, 2, nil), Models: []models.Model{{Provider: "openai", ID: "test"}}}
	if err := server.Serve(context.Background(), strings.NewReader("{\"id\":\"1\",\"type\":\"prompt\",\"message\":\"hello\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"command":"prompt"`) || !strings.Contains(out.String(), `"answer":"done"`) {
		t.Fatalf("output=%s", out.String())
	}
	out.Reset()
	if err := server.Serve(context.Background(), strings.NewReader("{\"id\":\"2\",\"type\":\"get_state\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"messageCount":2`) {
		t.Fatalf("state output=%s", out.String())
	}
}

func TestServeReturnsStructuredErrors(t *testing.T) {
	var out strings.Builder
	server := &Server{}
	if err := server.Serve(context.Background(), strings.NewReader("not-json\n{\"id\":\"x\",\"type\":\"unknown\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"success":false`) || !strings.Contains(out.String(), "unknown RPC command") {
		t.Fatalf("output=%s", out.String())
	}
}

func TestServeFiltersAndCyclesModels(t *testing.T) {
	var out strings.Builder
	selected := ""
	server := &Server{Models: []models.Model{{Provider: "openai", ID: "one"}, {Provider: "openai", ID: "two"}}, State: func() State { return State{Provider: "openai", Model: "one"} }, SetModel: func(provider, model string) error { selected = provider + "/" + model; return nil }}
	if err := server.Serve(context.Background(), strings.NewReader("{\"id\":\"1\",\"type\":\"cycle_model\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if selected != "openai/two" || !strings.Contains(out.String(), `"success":true`) {
		t.Fatalf("selected=%q output=%s", selected, out.String())
	}
	if !reflect.DeepEqual(server.Models[0].Provider, "openai") {
		t.Fatal("model catalog mutated")
	}
}

func TestServeSupportsBashPersistence(t *testing.T) {
	var out strings.Builder
	var persisted []agent.Message
	server := &Server{Shell: func(context.Context, string) (string, error) { return "ok", nil }, Persist: func(messages []agent.Message) error { persisted = append(persisted, messages...); return nil }}
	if err := server.Serve(context.Background(), strings.NewReader("{\"id\":\"1\",\"type\":\"bash\",\"command\":\"echo hi\"}\n"), &out); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 1 || persisted[0].Role != "bashExecution" || !strings.Contains(out.String(), `"output":"ok"`) {
		t.Fatalf("persisted=%#v output=%s", persisted, out.String())
	}
}

func TestServeRejectsNilIO(t *testing.T) {
	if err := (&Server{}).Serve(context.Background(), io.Reader(nil), io.Writer(nil)); err == nil {
		t.Fatal("nil io accepted")
	}
}
