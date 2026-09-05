package provider

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

func TestOpenAIAdapterSendsToolsAndParsesToolCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"tools"`) {
			t.Fatal("tools missing")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}}]}`))
	}))
	defer server.Close()
	p, err := New("openai", Config{Model: "test", BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	reply, err := p.Complete(context.Background(), []agent.Message{{Role: "user", Content: "read"}}, []agent.ToolDefinition{{Name: "read", Parameters: map[string]any{"type": "object"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != "read" {
		t.Fatalf("reply=%#v", reply)
	}
}
