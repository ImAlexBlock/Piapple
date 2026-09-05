package provider

import (
	"context"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"io"
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

func TestAllHTTPProvidersExposeStreaming(t *testing.T) {
	for _, name := range []string{"openai", "anthropic", "google"} {
		p, err := New(name, Config{Model: "test", BaseURL: "http://127.0.0.1:1", APIKey: "key"})
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := p.(agent.StreamingProvider); !ok {
			t.Fatalf("%s is not streaming", name)
		}
	}
}

func TestAnthropicStreamParsesTextAndToolUse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"tool_use\",\"id\":\"tool-1\",\"name\":\"read\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"path\\\":\\\"README.md\\\"}\"}}\n\n")
	}))
	defer server.Close()
	p, _ := New("anthropic", Config{Model: "test", BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	stream, ok := p.(agent.StreamingProvider)
	if !ok {
		t.Fatal("not streaming")
	}
	ch, err := stream.Stream(context.Background(), []agent.Message{{Role: "user", Content: "read"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var answer string
	var final *agent.Message
	for event := range ch {
		if event.Type == "delta" {
			answer += event.Delta
		}
		if event.Type == "done" {
			final = event.Message
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if answer != "hello" || final == nil || len(final.ToolCalls) != 1 || final.ToolCalls[0].Arguments != `{"path":"README.md"}` {
		t.Fatalf("answer=%q final=%#v", answer, final)
	}
}

func TestGoogleStreamParsesTextAndFunctionCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") != "sse" {
			t.Fatalf("alt=%q", r.URL.Query().Get("alt"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"hello\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"functionCall\":{\"name\":\"read\",\"args\":{\"path\":\"README.md\"}}}]}}]}\n\n")
	}))
	defer server.Close()
	p, _ := New("google", Config{Model: "test", BaseURL: server.URL, APIKey: "key", Client: server.Client()})
	stream, ok := p.(agent.StreamingProvider)
	if !ok {
		t.Fatal("not streaming")
	}
	ch, err := stream.Stream(context.Background(), []agent.Message{{Role: "user", Content: "read"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	var answer string
	var final *agent.Message
	for event := range ch {
		if event.Type == "delta" {
			answer += event.Delta
		}
		if event.Type == "done" {
			final = event.Message
		}
		if event.Err != nil {
			t.Fatal(event.Err)
		}
	}
	if answer != "hello" || final == nil || len(final.ToolCalls) != 1 || final.ToolCalls[0].Name != "read" {
		t.Fatalf("answer=%q final=%#v", answer, final)
	}
}

func TestOpenAICompatibleChannelsAreConstructible(t *testing.T) {
	for _, name := range []string{"xai", "groq", "mistral", "deepseek", "openrouter", "together", "fireworks", "perplexity", "moonshot", "zai", "minimax", "siliconflow", "qwen", "github"} {
		if _, err := New(name, Config{Model: "test", BaseURL: "http://localhost", APIKey: "key"}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
