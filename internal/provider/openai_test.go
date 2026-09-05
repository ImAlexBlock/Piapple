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

func TestOpenAIStreamEmitsDeltasAndToolCalls(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Fatalf("accept=%q", r.Header.Get("Accept"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\n")
		_, _ = io.WriteString(w, `data: {"choices":[{"delta":{"content":"lo","tool_calls":[{"index":0,"id":"call-1","function":{"name":"read","arguments":"{\"path\":\"README.md\"}"}}]}}]}`+"\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer server.Close()
	p, err := New("openai", Config{Model: "test", BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	streaming, ok := p.(agent.StreamingProvider)
	if !ok {
		t.Fatal("openai provider is not streaming")
	}
	stream, err := streaming.Stream(context.Background(), []agent.Message{{Role: "user", Content: "hi"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var deltas string
	var final *agent.Message
	for event := range stream {
		if event.Type == "delta" {
			deltas += event.Delta
		}
		if event.Type == "done" {
			final = event.Message
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if deltas != "hello" || final == nil || len(final.ToolCalls) != 1 || final.ToolCalls[0].Name != "read" {
		t.Fatalf("deltas=%q final=%#v", deltas, final)
	}
}
