package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

type openAI struct{ Config }

func (p *openAI) SetThinking(level string) { p.Thinking = strings.ToLower(strings.TrimSpace(level)) }

type openAIMessage struct {
	Role       string           `json:"role"`
	Name       string           `json:"name,omitempty"`
	Content    string           `json:"content,omitempty"`
	Reasoning  string           `json:"reasoning_content,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []openAIToolCall `json:"tool_calls,omitempty"`
}
type openAIRequest struct {
	Model           string          `json:"model"`
	Messages        []openAIMessage `json:"messages"`
	Tools           []any           `json:"tools,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	StreamOptions   map[string]any  `json:"stream_options,omitempty"`
	ReasoningEffort string          `json:"reasoning_effort,omitempty"`
}

type openAIStreamChunk struct {
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Delta        struct {
			Role         string `json:"role"`
			Content      string `json:"content"`
			Reasoning    string `json:"reasoning_content"`
			ReasoningAlt string `json:"reasoning"`
			ToolCalls    []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

type openAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func openAIReasoningEffort(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "minimal", "low", "medium", "high":
		return level
	case "xhigh", "max":
		// Chat Completions exposes high as its largest portable effort. Keep
		// Pi's richer thinking scale usable on OpenAI-compatible gateways
		// instead of silently disabling reasoning for the two upper levels.
		return "high"
	default:
		return ""
	}
}

func openAIMessages(p *openAI, messages []agent.Message, tools []agent.ToolDefinition, stream bool) openAIRequest {
	body := openAIRequest{Model: p.Model, ReasoningEffort: openAIReasoningEffort(p.Thinking), Stream: stream}
	if stream {
		body.StreamOptions = map[string]any{"include_usage": true}
	}
	if p.SystemPrompt != "" {
		body.Messages = append(body.Messages, openAIMessage{Role: "system", Content: p.SystemPrompt})
	}
	for _, m := range messages {
		if m.Role == "bashExecution" {
			if m.ExcludeFromContext {
				continue
			}
			body.Messages = append(body.Messages, openAIMessage{Role: "user", Content: bashExecutionText(m)})
			continue
		}
		role := m.Role
		if role == "toolResult" {
			role = "tool"
		}
		om := openAIMessage{Role: role, Name: m.Name, Content: m.Content, Reasoning: m.Reasoning, ToolCallID: m.ToolCallID}
		for _, c := range m.ToolCalls {
			tc := openAIToolCall{ID: c.ID, Type: "function"}
			tc.Function.Name, tc.Function.Arguments = c.Name, c.Arguments
			om.ToolCalls = append(om.ToolCalls, tc)
		}
		body.Messages = append(body.Messages, om)
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, map[string]any{"type": "function", "function": map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters}})
	}
	return body
}

func (p *openAI) request(ctx context.Context, body openAIRequest) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + p.APIKey}
	if body.Stream {
		headers["Accept"] = "text/event-stream"
	}
	for key, value := range p.Headers {
		headers[key] = value
	}
	return doJSONWithRetry(ctx, p.Client, http.MethodPost, p.BaseURL+"/chat/completions", raw, headers, p.MaxRetries, p.RetryBackoff)
}

func (p *openAI) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Message, error) {
	resp, err := p.request(ctx, openAIMessages(p, messages, tools, false))
	if err != nil {
		return agent.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return agent.Message{}, readHTTPError(resp)
	}
	var decoded struct {
		Choices []struct {
			FinishReason string        `json:"finish_reason"`
			Message      openAIMessage `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return agent.Message{}, err
	}
	if len(decoded.Choices) == 0 {
		return agent.Message{}, fmt.Errorf("openai returned no choices")
	}
	choice := decoded.Choices[0]
	m := choice.Message
	out := agent.Message{Role: "assistant", Content: m.Content, Reasoning: m.Reasoning, API: string(ProtocolOpenAI), Provider: p.Provider, Model: p.Model, StopReason: normalizeStopReason(choice.FinishReason), Timestamp: time.Now().UnixMilli()}
	if out.StopReason == "" {
		out.StopReason = "stop"
	}
	if decoded.Usage.TotalTokens > 0 || decoded.Usage.PromptTokens > 0 || decoded.Usage.CompletionTokens > 0 {
		out.Usage = &agent.Usage{InputTokens: decoded.Usage.PromptTokens, OutputTokens: decoded.Usage.CompletionTokens, TotalTokens: decoded.Usage.TotalTokens}
	}
	for _, c := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: c.ID, Name: c.Function.Name, Arguments: c.Function.Arguments})
	}
	if len(out.ToolCalls) > 0 && choice.FinishReason == "" {
		out.StopReason = "toolUse"
	}
	return out, nil
}

func (p *openAI) Stream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (<-chan agent.StreamEvent, error) {
	resp, err := p.request(ctx, openAIMessages(p, messages, tools, true))
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
		message := agent.Message{Role: "assistant", API: string(ProtocolOpenAI), Provider: p.Provider, Model: p.Model, Timestamp: time.Now().UnixMilli()}
		calls := map[int]*agent.ToolCall{}
		var usage *agent.Usage
		finishReason := ""
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
			if data == "[DONE]" {
				break
			}
			if data == "" {
				continue
			}
			var chunk openAIStreamChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: fmt.Errorf("openai stream: %w", err)})
				return
			}
			if chunk.Error != nil && chunk.Error.Message != "" {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: fmt.Errorf("openai stream: %s", chunk.Error.Message)})
				return
			}
			if chunk.Usage != nil {
				usage = &agent.Usage{InputTokens: chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens, TotalTokens: chunk.Usage.TotalTokens}
			}
			if len(chunk.Choices) == 0 {
				continue
			}
			choice := chunk.Choices[0]
			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
			delta := choice.Delta
			reasoning := delta.Reasoning
			if reasoning == "" {
				reasoning = delta.ReasoningAlt
			}
			if reasoning != "" {
				message.Reasoning += reasoning
				if !emitStreamEvent(ctx, out, agent.StreamEvent{Type: "reasoning_delta", Delta: reasoning}) {
					return
				}
			}
			if delta.Content != "" {
				message.Content += delta.Content
				if !emitStreamEvent(ctx, out, agent.StreamEvent{Type: "delta", Delta: delta.Content}) {
					return
				}
			}
			for _, tc := range delta.ToolCalls {
				call := calls[tc.Index]
				if call == nil {
					call = &agent.ToolCall{}
					calls[tc.Index] = call
				}
				if tc.ID != "" {
					call.ID = tc.ID
				}
				call.Name += tc.Function.Name
				call.Arguments += tc.Function.Arguments
			}
		}
		if err := scanner.Err(); err != nil {
			emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: err})
			return
		}
		indices := make([]int, 0, len(calls))
		for index := range calls {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			message.ToolCalls = append(message.ToolCalls, *calls[index])
		}
		message.Usage = usage
		message.StopReason = normalizeStopReason(finishReason)
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

func bashExecutionText(m agent.Message) string {
	text := "Ran `" + m.Command + "`\n"
	if m.Content != "" {
		text += "```\n" + m.Content + "\n```"
	} else {
		text += "(no output)"
	}
	if m.Cancelled {
		text += "\n\n(command cancelled)"
	} else if m.ExitCode != nil && *m.ExitCode != 0 {
		text += fmt.Sprintf("\n\nCommand exited with code %d", *m.ExitCode)
	}
	return text
}

func normalizeStopReason(reason string) string {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "tool_calls", "function_call", "tool_use":
		return "toolUse"
	case "length", "max_tokens":
		return "length"
	case "content_filter":
		return "error"
	case "stop":
		return "stop"
	default:
		return ""
	}
}

func readHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.Error.Message != "" {
			return fmt.Errorf("provider returned %s: %s", resp.Status, envelope.Error.Message)
		}
		if envelope.Message != "" {
			return fmt.Errorf("provider returned %s: %s", resp.Status, envelope.Message)
		}
	}
	return fmt.Errorf("provider returned %s", resp.Status)
}
