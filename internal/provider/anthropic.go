package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

type anthropic struct{ Config }

func (p *anthropic) SetThinking(level string) { p.Thinking = strings.ToLower(strings.TrimSpace(level)) }

func anthropicMaxTokens(level string) int {
	budget := anthropicThinkingBudget(level)
	if budget == 0 {
		return 4096
	}
	return budget + 4096
}
func anthropicThinking(level string) map[string]any {
	budget := anthropicThinkingBudget(level)
	if budget == 0 {
		return nil
	}
	return map[string]any{"type": "enabled", "budget_tokens": budget}
}

func anthropicThinkingBudget(level string) int {
	return map[string]int{
		"minimal": 1024,
		"low":     2048,
		"medium":  4096,
		"high":    8192,
		"xhigh":   12288,
		"max":     16384,
	}[strings.ToLower(strings.TrimSpace(level))]
}

type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}
type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}
type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Tools     []map[string]any   `json:"tools,omitempty"`
	Thinking  map[string]any     `json:"thinking,omitempty"`
	Stream    bool               `json:"stream"`
}

func anthropicMessages(p *anthropic, messages []agent.Message, tools []agent.ToolDefinition, stream bool) anthropicRequest {
	body := anthropicRequest{Model: p.Model, MaxTokens: anthropicMaxTokens(p.Thinking), System: p.SystemPrompt, Stream: stream, Thinking: anthropicThinking(p.Thinking)}
	for _, m := range messages {
		if m.Role == "bashExecution" {
			if m.ExcludeFromContext {
				continue
			}
			body.Messages = append(body.Messages, anthropicMessage{Role: "user", Content: []anthropicContent{{Type: "text", Text: bashExecutionText(m)}}})
			continue
		}
		role := m.Role
		if role == "system" {
			role = "user"
		}
		if role == "tool" || role == "toolResult" {
			body.Messages = append(body.Messages, anthropicMessage{Role: "user", Content: []anthropicContent{{Type: "tool_result", ToolUseID: m.ToolCallID, Content: m.Content, IsError: m.IsError}}})
			continue
		}
		if role == "assistant" {
			parts := make([]anthropicContent, 0, 1+len(m.ToolCalls))
			// Anthropic requires the provider-issued signature when replaying
			// thinking blocks. Only send a block when the signature survived the
			// session round trip; an unsigned internal trace would be rejected.
			if m.Reasoning != "" && m.ReasoningSignature != "" {
				parts = append(parts, anthropicContent{Type: "thinking", Thinking: m.Reasoning, Signature: m.ReasoningSignature})
			}
			if m.Content != "" {
				parts = append(parts, anthropicContent{Type: "text", Text: m.Content})
			}
			for _, call := range m.ToolCalls {
				input := json.RawMessage(call.Arguments)
				if !json.Valid(input) {
					input = json.RawMessage(`{}`)
				}
				parts = append(parts, anthropicContent{Type: "tool_use", ID: call.ID, Name: call.Name, Input: input})
			}
			if len(parts) == 0 {
				parts = append(parts, anthropicContent{Type: "text", Text: ""})
			}
			body.Messages = append(body.Messages, anthropicMessage{Role: "assistant", Content: parts})
			continue
		}
		body.Messages = append(body.Messages, anthropicMessage{Role: "user", Content: []anthropicContent{{Type: "text", Text: m.Content}}})
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, map[string]any{"name": t.Name, "description": t.Description, "input_schema": t.Parameters})
	}
	return body
}

func (p *anthropic) request(ctx context.Context, body anthropicRequest) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Content-Type": "application/json", "x-api-key": p.APIKey, "anthropic-version": "2023-06-01"}
	if body.Stream {
		headers["Accept"] = "text/event-stream"
	}
	for key, value := range p.Headers {
		headers[key] = value
	}
	return doJSONWithRetry(ctx, p.Client, http.MethodPost, p.BaseURL+"/messages", raw, headers, p.MaxRetries, p.RetryBackoff)
}

func (p *anthropic) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Message, error) {
	resp, err := p.request(ctx, anthropicMessages(p, messages, tools, false))
	if err != nil {
		return agent.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return agent.Message{}, readHTTPError(resp)
	}
	var result struct {
		Content    []anthropicContent `json:"content"`
		StopReason string             `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.Message{}, err
	}
	out := agent.Message{Role: "assistant", API: string(ProtocolAnthropic), Provider: p.Provider, Model: p.Model, StopReason: normalizeAnthropicStop(result.StopReason), Timestamp: time.Now().UnixMilli()}
	if result.Usage.InputTokens > 0 || result.Usage.OutputTokens > 0 {
		out.Usage = &agent.Usage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, TotalTokens: result.Usage.InputTokens + result.Usage.OutputTokens}
	}
	for _, c := range result.Content {
		switch c.Type {
		case "text":
			out.Content += c.Text
		case "thinking":
			out.Reasoning += c.Thinking
			if c.Signature != "" {
				out.ReasoningSignature = c.Signature
			}
		case "tool_use":
			out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: c.ID, Name: c.Name, Arguments: string(c.Input)})
		}
	}
	if out.StopReason == "" {
		if len(out.ToolCalls) > 0 {
			out.StopReason = "toolUse"
		} else {
			out.StopReason = "stop"
		}
	}
	if len(result.Content) == 0 {
		return agent.Message{}, fmt.Errorf("anthropic returned no content")
	}
	return out, nil
}

func (p *anthropic) Stream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (<-chan agent.StreamEvent, error) {
	resp, err := p.request(ctx, anthropicMessages(p, messages, tools, true))
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
		message := agent.Message{Role: "assistant", API: string(ProtocolAnthropic), Provider: p.Provider, Model: p.Model, Timestamp: time.Now().UnixMilli()}
		calls := map[int]*agent.ToolCall{}
		usage := &agent.Usage{}
		finish := ""
		currentBlock := -1
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 4*1024*1024)
		for scanner.Scan() {
			if ctx.Err() != nil {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: ctx.Err()})
				return
			}
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" {
				continue
			}
			var event struct {
				Type  string `json:"type"`
				Index int    `json:"index"`
				Error *struct {
					Message string `json:"message"`
				} `json:"error,omitempty"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
				ContentBlock anthropicContent `json:"content_block"`
				Delta        struct {
					Type        string `json:"type"`
					Text        string `json:"text"`
					PartialJSON string `json:"partial_json"`
					Thinking    string `json:"thinking"`
					Signature   string `json:"signature"`
					StopReason  string `json:"stop_reason"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(data), &event); err != nil {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: fmt.Errorf("anthropic stream: %w", err)})
				return
			}
			if event.Error != nil && event.Error.Message != "" {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: fmt.Errorf("anthropic stream: %s", event.Error.Message)})
				return
			}
			if event.Usage.InputTokens > 0 {
				usage.InputTokens = event.Usage.InputTokens
			}
			if event.Usage.OutputTokens > 0 {
				usage.OutputTokens = event.Usage.OutputTokens
			}
			if event.Delta.StopReason != "" {
				finish = event.Delta.StopReason
			}
			switch event.Type {
			case "content_block_start":
				currentBlock = event.Index
				if event.ContentBlock.Type == "thinking" && event.ContentBlock.Signature != "" {
					message.ReasoningSignature = event.ContentBlock.Signature
				}
				if event.ContentBlock.Type == "tool_use" {
					calls[currentBlock] = &agent.ToolCall{ID: event.ContentBlock.ID, Name: event.ContentBlock.Name}
				}
			case "content_block_delta":
				if event.Delta.Type == "thinking_delta" && event.Delta.Thinking != "" {
					message.Reasoning += event.Delta.Thinking
					if !emitStreamEvent(ctx, out, agent.StreamEvent{Type: "reasoning_delta", Delta: event.Delta.Thinking}) {
						return
					}
				}
				if event.Delta.Type == "signature_delta" && event.Delta.Signature != "" {
					message.ReasoningSignature += event.Delta.Signature
				}
				if event.Delta.Type == "text_delta" && event.Delta.Text != "" {
					message.Content += event.Delta.Text
					if !emitStreamEvent(ctx, out, agent.StreamEvent{Type: "delta", Delta: event.Delta.Text}) {
						return
					}
				}
				if event.Delta.Type == "input_json_delta" && calls[event.Index] != nil {
					calls[event.Index].Arguments += event.Delta.PartialJSON
				}
			}
		}
		if err := scanner.Err(); err != nil {
			emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: err})
			return
		}
		indices := make([]int, 0, len(calls))
		for i := range calls {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		for _, i := range indices {
			message.ToolCalls = append(message.ToolCalls, *calls[i])
		}
		if usage.InputTokens > 0 || usage.OutputTokens > 0 {
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			message.Usage = usage
		}
		message.StopReason = normalizeAnthropicStop(finish)
		if message.StopReason == "" {
			if len(message.ToolCalls) > 0 {
				message.StopReason = "toolUse"
			} else {
				message.StopReason = "stop"
			}
		}
		emitStreamEvent(ctx, out, agent.StreamEvent{Type: "done", Message: &message})
	}()
	return out, nil
}

func normalizeAnthropicStop(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool_use":
		return "toolUse"
	case "max_tokens":
		return "length"
	case "stop_sequence", "end_turn":
		return "stop"
	default:
		return ""
	}
}
