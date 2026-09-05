package provider

import (
	"context"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAnthropicAdapterParsesToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "key" {
			t.Fatal("missing api key")
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use","id":"x","name":"read","input":{"path":"a"}}]}`))
	}))
	defer server.Close()
	p, _ := New("anthropic", Config{Model: "m", BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	reply, err := p.Complete(context.Background(), []agent.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Arguments != `{"path":"a"}` {
		t.Fatalf("%#v", reply)
	}
}
func TestGoogleAdapterParsesFunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key") != "key" {
			t.Fatal("missing key")
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"functionCall":{"name":"read","args":{"path":"a"}}}]}}]}`))
	}))
	defer server.Close()
	p, _ := New("google", Config{Model: "m", BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	reply, err := p.Complete(context.Background(), []agent.Message{{Role: "user", Content: "x"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != "read" {
		t.Fatalf("%#v", reply)
	}
}
