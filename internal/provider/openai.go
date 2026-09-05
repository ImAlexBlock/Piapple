package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

type openAI struct{ Config }
type openAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}
type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func (p *openAI) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Message, error) {
	body := struct {
		Model       string          `json:"model"`
		Messages    []openAIMessage `json:"messages"`
		Tools       []any           `json:"tools,omitempty"`
		Temperature float64         `json:"temperature"`
	}{Model: p.Model, Temperature: 0}
	if p.SystemPrompt != "" {
		body.Messages = append(body.Messages, openAIMessage{Role: "system", Content: p.SystemPrompt})
	}
	for _, m := range messages {
		om := openAIMessage{Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}
		for _, c := range m.ToolCalls {
			tc := openAIToolCall{ID: c.ID, Type: "function"}
			tc.Function.Name = c.Name
			tc.Function.Arguments = c.Arguments
			om.ToolCalls = append(om.ToolCalls, tc)
		}
		body.Messages = append(body.Messages, om)
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return agent.Message{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return agent.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	resp, err := p.Client.Do(req)
	if err != nil {
		return agent.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return agent.Message{}, readHTTPError(resp)
	}
	var decoded struct {
		Choices []struct {
			Message openAIMessage `json:"message"`
		}
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return agent.Message{}, err
	}
	if len(decoded.Choices) == 0 {
		return agent.Message{}, fmt.Errorf("openai returned no choices")
	}
	m := decoded.Choices[0].Message
	out := agent.Message{Role: "assistant", Content: m.Content}
	for _, c := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
	}
	return out, nil
}
func readHTTPError(resp *http.Response) error {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error.Message != "" {
		return fmt.Errorf("provider returned %s: %s", resp.Status, body.Error.Message)
	}
	return fmt.Errorf("provider returned %s", resp.Status)
}

var _ = strings.TrimSpace
