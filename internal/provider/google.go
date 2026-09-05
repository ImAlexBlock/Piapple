package provider

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

type google struct{ Config }

func (p *google) SetThinking(level string) { p.Thinking = strings.ToLower(strings.TrimSpace(level)) }
func googleThinking(level string) map[string]any {
	budget := map[string]int{"minimal": 512, "low": 1024, "medium": 4096, "high": 8192, "xhigh": 12288, "max": 16384}[strings.ToLower(strings.TrimSpace(level))]
	if budget == 0 {
		return nil
	}
	return map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": budget}}
}

type googleFunctionCall struct {
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}
type googleFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}
type googlePart struct {
	Text             string                  `json:"text,omitempty"`
	Thought          bool                    `json:"thought,omitempty"`
	ThoughtSignature string                  `json:"thoughtSignature,omitempty"`
	FunctionCall     *googleFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *googleFunctionResponse `json:"functionResponse,omitempty"`
}
type googleContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []googlePart `json:"parts"`
}
type googleRequest struct {
	SystemInstruction *googleContent   `json:"systemInstruction,omitempty"`
	Contents          []googleContent  `json:"contents"`
	Tools             []map[string]any `json:"tools,omitempty"`
	GenerationConfig  map[string]any   `json:"generationConfig,omitempty"`
}

func googleContents(p *google, messages []agent.Message, tools []agent.ToolDefinition) googleRequest {
	body := googleRequest{GenerationConfig: googleThinking(p.Thinking)}
	if p.SystemPrompt != "" {
		body.SystemInstruction = &googleContent{Parts: []googlePart{{Text: p.SystemPrompt}}}
	}
	for _, m := range messages {
		if m.Role == "bashExecution" {
			if m.ExcludeFromContext {
				continue
			}
			body.Contents = append(body.Contents, googleContent{Role: "user", Parts: []googlePart{{Text: bashExecutionText(m)}}})
			continue
		}
		if m.Role == "tool" || m.Role == "toolResult" {
			name := m.ToolName
			if name == "" {
				name = m.ToolCallID
			}
			body.Contents = append(body.Contents, googleContent{Role: "user", Parts: []googlePart{{FunctionResponse: &googleFunctionResponse{Name: name, Response: map[string]any{"result": m.Content, "is_error": m.IsError}}}}})
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		parts := make([]googlePart, 0, 1+len(m.ToolCalls))
		// Gemini requires the provider-issued thought signature when replaying
		// an internal thought. Do not send an unsigned local trace.
		if m.Reasoning != "" && m.ReasoningSignature != "" {
			parts = append(parts, googlePart{Text: m.Reasoning, Thought: true, ThoughtSignature: m.ReasoningSignature})
		}
		if m.Content != "" {
			parts = append(parts, googlePart{Text: m.Content})
		}
		for _, c := range m.ToolCalls {
			var args map[string]any
			if json.Unmarshal([]byte(c.Arguments), &args) != nil || args == nil {
				args = map[string]any{}
			}
			parts = append(parts, googlePart{FunctionCall: &googleFunctionCall{Name: c.Name, Args: args}, ThoughtSignature: c.ThoughtSignature})
		}
		if len(parts) == 0 {
			parts = append(parts, googlePart{Text: ""})
		}
		body.Contents = append(body.Contents, googleContent{Role: role, Parts: parts})
	}
	for _, t := range tools {
		body.Tools = append(body.Tools, map[string]any{"functionDeclarations": []map[string]any{{"name": t.Name, "description": t.Description, "parameters": t.Parameters}}})
	}
	// Gemini accepts one tool object containing all declarations. Flatten the
	// declarations assembled above to match the public API shape.
	if len(body.Tools) > 1 {
		declarations := make([]map[string]any, 0, len(tools))
		for _, t := range tools {
			declarations = append(declarations, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
		}
		body.Tools = []map[string]any{{"functionDeclarations": declarations}}
	}
	return body
}

func (p *google) request(ctx context.Context, body googleRequest, stream bool) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	suffix := "generateContent"
	if stream {
		suffix = "streamGenerateContent?alt=sse"
	}
	endpoint := p.BaseURL + "/models/" + url.PathEscape(p.Model) + ":" + suffix + "&key=" + url.QueryEscape(p.APIKey)
	if !stream {
		endpoint = p.BaseURL + "/models/" + url.PathEscape(p.Model) + ":generateContent?key=" + url.QueryEscape(p.APIKey)
	}
	headers := map[string]string{"Content-Type": "application/json"}
	if stream {
		headers["Accept"] = "text/event-stream"
	}
	for key, value := range p.Headers {
		headers[key] = value
	}
	return doJSONWithRetry(ctx, p.Client, http.MethodPost, endpoint, raw, headers, p.MaxRetries, p.RetryBackoff)
}

func (p *google) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Message, error) {
	resp, err := p.request(ctx, googleContents(p, messages, tools), false)
	if err != nil {
		return agent.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return agent.Message{}, readHTTPError(resp)
	}
	var result struct {
		Candidates []struct {
			Content      googleContent `json:"content"`
			FinishReason string        `json:"finishReason"`
		} `json:"candidates"`
		UsageMetadata struct {
			PromptTokenCount     int `json:"promptTokenCount"`
			CandidatesTokenCount int `json:"candidatesTokenCount"`
			TotalTokenCount      int `json:"totalTokenCount"`
		} `json:"usageMetadata"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return agent.Message{}, err
	}
	if len(result.Candidates) == 0 {
		return agent.Message{}, fmt.Errorf("google returned no candidates")
	}
	c := result.Candidates[0]
	out := agent.Message{Role: "assistant", API: string(ProtocolGoogle), Provider: p.Provider, Model: p.Model, StopReason: normalizeGoogleStop(c.FinishReason), Timestamp: time.Now().UnixMilli()}
	if result.UsageMetadata.TotalTokenCount > 0 || result.UsageMetadata.PromptTokenCount > 0 || result.UsageMetadata.CandidatesTokenCount > 0 {
		out.Usage = &agent.Usage{InputTokens: result.UsageMetadata.PromptTokenCount, OutputTokens: result.UsageMetadata.CandidatesTokenCount, TotalTokens: result.UsageMetadata.TotalTokenCount}
	}
	for _, piece := range c.Content.Parts {
		if piece.Text != "" {
			if piece.Thought {
				out.Reasoning += piece.Text
				if piece.ThoughtSignature != "" {
					out.ReasoningSignature = piece.ThoughtSignature
				}
			} else {
				out.Content += piece.Text
			}
		}
		if piece.FunctionCall != nil {
			args, _ := json.Marshal(piece.FunctionCall.Args)
			out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: piece.FunctionCall.Name, Name: piece.FunctionCall.Name, Arguments: string(args), ThoughtSignature: piece.ThoughtSignature})
		}
	}
	if out.StopReason == "" {
		if len(out.ToolCalls) > 0 {
			out.StopReason = "toolUse"
		} else {
			out.StopReason = "stop"
		}
	}
	return out, nil
}

func (p *google) Stream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (<-chan agent.StreamEvent, error) {
	resp, err := p.request(ctx, googleContents(p, messages, tools), true)
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
		message := agent.Message{Role: "assistant", API: string(ProtocolGoogle), Provider: p.Provider, Model: p.Model, Timestamp: time.Now().UnixMilli()}
		calls := map[string]*agent.ToolCall{}
		var usage *agent.Usage
		finish := ""
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
			var result struct {
				Error *struct {
					Message string `json:"message"`
				} `json:"error,omitempty"`
				Candidates []struct {
					Content      googleContent `json:"content"`
					FinishReason string        `json:"finishReason"`
				} `json:"candidates"`
				UsageMetadata struct {
					PromptTokenCount     int `json:"promptTokenCount"`
					CandidatesTokenCount int `json:"candidatesTokenCount"`
					TotalTokenCount      int `json:"totalTokenCount"`
				} `json:"usageMetadata"`
			}
			if err := json.Unmarshal([]byte(data), &result); err != nil {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: fmt.Errorf("google stream: %w", err)})
				return
			}
			if result.Error != nil && result.Error.Message != "" {
				emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: fmt.Errorf("google stream: %s", result.Error.Message)})
				return
			}
			if result.UsageMetadata.TotalTokenCount > 0 || result.UsageMetadata.PromptTokenCount > 0 || result.UsageMetadata.CandidatesTokenCount > 0 {
				usage = &agent.Usage{InputTokens: result.UsageMetadata.PromptTokenCount, OutputTokens: result.UsageMetadata.CandidatesTokenCount, TotalTokens: result.UsageMetadata.TotalTokenCount}
			}
			if len(result.Candidates) == 0 {
				continue
			}
			candidate := result.Candidates[0]
			if candidate.FinishReason != "" {
				finish = candidate.FinishReason
			}
			for _, piece := range candidate.Content.Parts {
				if piece.Text != "" {
					if piece.Thought {
						message.Reasoning += piece.Text
						if !emitStreamEvent(ctx, out, agent.StreamEvent{Type: "reasoning_delta", Delta: piece.Text}) {
							return
						}
					} else {
						message.Content += piece.Text
						if !emitStreamEvent(ctx, out, agent.StreamEvent{Type: "delta", Delta: piece.Text}) {
							return
						}
					}
				}
				if piece.FunctionCall != nil {
					args, _ := json.Marshal(piece.FunctionCall.Args)
					call := calls[piece.FunctionCall.Name]
					if call == nil {
						call = &agent.ToolCall{ID: piece.FunctionCall.Name, Name: piece.FunctionCall.Name}
						calls[piece.FunctionCall.Name] = call
					}
					call.Arguments = string(args)
				}
			}
		}
		if err := scanner.Err(); err != nil {
			emitStreamEvent(ctx, out, agent.StreamEvent{Type: "error", Err: err})
			return
		}
		for _, call := range calls {
			message.ToolCalls = append(message.ToolCalls, *call)
		}
		message.Usage = usage
		message.StopReason = normalizeGoogleStop(finish)
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

func normalizeGoogleStop(reason string) string {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION":
		return "error"
	case "OTHER":
		return "error"
	default:
		return ""
	}
}
