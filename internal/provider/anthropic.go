package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"net/http"
)

type anthropic struct{ Config }

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
