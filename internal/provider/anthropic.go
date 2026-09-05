package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"net/http"
	"strings"
)

type anthropic struct{ Config }

func (p *anthropic) Stream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (<-chan agent.StreamEvent, error) {
	type content struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   string          `json:"content,omitempty"`
	}
	type message struct {
		Role    string    `json:"role"`
		Content []content `json:"content"`
	}
	body := struct {
		Model     string           `json:"model"`
		MaxTokens int              `json:"max_tokens"`
		System    string           `json:"system,omitempty"`
		Messages  []message        `json:"messages"`
		Tools     []map[string]any `json:"tools,omitempty"`
		Stream    bool             `json:"stream"`
	}{Model: p.Model, MaxTokens: 4096, System: p.SystemPrompt, Stream: true}
	for _, m := range messages {
		if m.Role == "tool" {
			body.Messages = append(body.Messages, message{Role: "user", Content: []content{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}}})
			continue
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			parts := []content{}
			if m.Content != "" {
				parts = append(parts, content{Type: "text", Text: m.Content})
			}
			for _, call := range m.ToolCalls {
				parts = append(parts, content{Type: "tool_use", ID: call.ID, Name: call.Name, Input: json.RawMessage(call.Arguments)})
			}
			body.Messages = append(body.Messages, message{Role: "assistant", Content: parts})
			continue
		}
		body.Messages = append(body.Messages, message{Role: m.Role, Content: []content{{Type: "text", Text: m.Content}}})
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/messages", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := p.Client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		return nil, readHTTPError(resp)
	}
	out := make(chan agent.StreamEvent)
	go func() {
		defer close(out)
		defer resp.Body.Close()
		message := agent.Message{Role: "assistant"}
		calls := map[int]*agent.ToolCall{}
		currentBlock := -1
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var event struct {
				Type         string  `json:"type"`
				Index        int     `json:"index"`
				ContentBlock content `json:"content_block"`
				Delta        struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event) != nil {
				continue
			}
			switch event.Type {
			case "content_block_start":
				currentBlock = event.Index
				if event.ContentBlock.Type == "tool_use" {
					calls[currentBlock] = &agent.ToolCall{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name}
				}
			case "content_block_delta":
				if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
					message.Content += event.Delta.Text
					out <- agent.StreamEvent{Type: "delta", Delta: event.Delta.Text}
				}
				if event.Delta.Type == "input_json_delta" && calls[currentBlock] != nil {
					calls[currentBlock].Arguments += event.Delta.PartialJSON
				}
			}
		}
		if scanErr := scanner.Err(); scanErr != nil {
			out <- agent.StreamEvent{Type: "error", Err: scanErr}
			return
		}
		for i := 0; i <= currentBlock; i++ {
			if call := calls[i]; call != nil {
				message.ToolCalls = append(message.ToolCalls, *call)
			}
		}
		out <- agent.StreamEvent{Type: "done", Message: &message}
	}()
	return out, nil
}

func (p *anthropic) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Message, error) {
	type content struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   string          `json:"content,omitempty"`
	}
	type message struct {
		Role    string    `json:"role"`
		Content []content `json:"content"`
	}
	body := struct {
		Model     string           `json:"model"`
		MaxTokens int              `json:"max_tokens"`
		System    string           `json:"system,omitempty"`
		Messages  []message        `json:"messages"`
		Tools     []map[string]any `json:"tools,omitempty"`
	}{Model: p.Model, MaxTokens: 4096, System: p.SystemPrompt}
	for _, m := range messages {
		if m.Role == "tool" {
			body.Messages = append(body.Messages, message{Role: "user", Content: []content{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content}}})
			continue
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			c := []content{}
			if m.Content != "" {
				c = append(c, content{Type: "text", Text: m.Content})
			}
			for _, call := range m.ToolCalls {
				c = append(c, content{Type: "tool_use", ID: call.ID, Name: call.Name, Input: json.RawMessage(call.Arguments)})
			}
			body.Messages = append(body.Messages, message{Role: "assistant", Content: c})
			continue
		}
		body.Messages = append(body.Messages, message{Role: m.Role, Content: []content{{Type: "text", Text: m.Content}}})
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return agent.Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/messages", bytes.NewReader(raw))
	if err != nil {
		return agent.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := p.Client.Do(req)
	if err != nil {
		return agent.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return agent.Message{}, readHTTPError(resp)
	}
	var result struct {
		Content []content `json:"content"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.Message{}, err
	}
	out := agent.Message{Role: "assistant"}
	for _, c := range result.Content {
		if c.Type == "text" {
			out.Content += c.Text
		}
		if c.Type == "tool_use" {
			out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: c.ID, Name: c.Name, Arguments: string(c.Input)})
		}
	}
	if len(result.Content) == 0 {
		return agent.Message{}, fmt.Errorf("anthropic returned no content")
	}
	return out, nil
}
