package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/ImAlexBlock/Piapple/internal/agent"
	"net/http"
	"net/url"
	"strings"
)

type google struct{ Config }

func (p *google) SetThinking(level string) { p.Thinking = strings.ToLower(strings.TrimSpace(level)) }

func googleThinking(level string) map[string]any {
	budget := map[string]int{"minimal": 512, "low": 1024, "medium": 4096, "high": 8192}[strings.ToLower(strings.TrimSpace(level))]
	if budget == 0 {
		return nil
	}
	return map[string]any{"thinkingConfig": map[string]any{"thinkingBudget": budget}}
}

func (p *google) Stream(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (<-chan agent.StreamEvent, error) {
	type part struct {
		Text         string `json:"text,omitempty"`
		FunctionCall *struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		} `json:"functionCall,omitempty"`
		FunctionResponse *struct {
			Name     string         `json:"name"`
			Response map[string]any `json:"response"`
		} `json:"functionResponse,omitempty"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	body := struct {
		SystemInstruction *content         `json:"systemInstruction,omitempty"`
		Contents          []content        `json:"contents"`
		Tools             []map[string]any `json:"tools,omitempty"`
		GenerationConfig  map[string]any   `json:"generationConfig,omitempty"`
	}{GenerationConfig: googleThinking(p.Thinking)}
	if p.SystemPrompt != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: p.SystemPrompt}}}
	}
	for _, m := range messages {
		if m.Role == "tool" {
			body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{FunctionResponse: &struct {
				Name     string         `json:"name"`
				Response map[string]any `json:"response"`
			}{Name: m.ToolCallID, Response: map[string]any{"result": m.Content}}}}})
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		parts := []part{}
		if m.Content != "" {
			parts = append(parts, part{Text: m.Content})
		}
		for _, c := range m.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(c.Arguments), &args)
			parts = append(parts, part{FunctionCall: &struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			}{Name: c.Name, Args: args}})
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: parts})
	}
	if len(tools) > 0 {
		declarations := []map[string]any{}
		for _, t := range tools {
			declarations = append(declarations, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
		}
		body.Tools = []map[string]any{{"functionDeclarations": declarations}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	endpoint := p.BaseURL + "/models/" + url.PathEscape(p.Model) + ":streamGenerateContent?alt=sse&key=" + url.QueryEscape(p.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
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
		calls := map[string]*agent.ToolCall{}
		var usage *agent.Usage
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 4096), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var result struct {
				Candidates []struct {
					Content content `json:"content"`
				} `json:"candidates"`
				UsageMetadata struct {
					PromptTokenCount     int `json:"promptTokenCount"`
					CandidatesTokenCount int `json:"candidatesTokenCount"`
					TotalTokenCount      int `json:"totalTokenCount"`
				} `json:"usageMetadata"`
			}
			if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &result) != nil {
				continue
			}
			if result.UsageMetadata.TotalTokenCount > 0 {
				usage = &agent.Usage{InputTokens: result.UsageMetadata.PromptTokenCount, OutputTokens: result.UsageMetadata.CandidatesTokenCount, TotalTokens: result.UsageMetadata.TotalTokenCount}
			}
			if len(result.Candidates) == 0 {
				continue
			}
			for _, piece := range result.Candidates[0].Content.Parts {
				if piece.Text != "" {
					message.Content += piece.Text
					out <- agent.StreamEvent{Type: "delta", Delta: piece.Text}
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
		if scanErr := scanner.Err(); scanErr != nil {
			out <- agent.StreamEvent{Type: "error", Err: scanErr}
			return
		}
		for _, call := range calls {
			message.ToolCalls = append(message.ToolCalls, *call)
		}
		message.Usage = usage
		out <- agent.StreamEvent{Type: "done", Message: &message}
	}()
	return out, nil
}

func (p *google) Complete(ctx context.Context, messages []agent.Message, tools []agent.ToolDefinition) (agent.Message, error) {
	type part struct {
		Text         string `json:"text,omitempty"`
		FunctionCall *struct {
			Name string         `json:"name"`
			Args map[string]any `json:"args"`
		} `json:"functionCall,omitempty"`
		FunctionResponse *struct {
			Name     string         `json:"name"`
			Response map[string]any `json:"response"`
		} `json:"functionResponse,omitempty"`
	}
	type content struct {
		Role  string `json:"role,omitempty"`
		Parts []part `json:"parts"`
	}
	body := struct {
		SystemInstruction *content         `json:"systemInstruction,omitempty"`
		Contents          []content        `json:"contents"`
		Tools             []map[string]any `json:"tools,omitempty"`
		GenerationConfig  map[string]any   `json:"generationConfig,omitempty"`
	}{GenerationConfig: googleThinking(p.Thinking)}
	if p.SystemPrompt != "" {
		body.SystemInstruction = &content{Parts: []part{{Text: p.SystemPrompt}}}
	}
	for _, m := range messages {
		if m.Role == "tool" {
			body.Contents = append(body.Contents, content{Role: "user", Parts: []part{{FunctionResponse: &struct {
				Name     string         `json:"name"`
				Response map[string]any `json:"response"`
			}{Name: m.ToolCallID, Response: map[string]any{"result": m.Content}}}}})
			continue
		}
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		parts := []part{}
		if m.Content != "" {
			parts = append(parts, part{Text: m.Content})
		}
		for _, c := range m.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(c.Arguments), &args)
			parts = append(parts, part{FunctionCall: &struct {
				Name string         `json:"name"`
				Args map[string]any `json:"args"`
			}{Name: c.Name, Args: args}})
		}
		body.Contents = append(body.Contents, content{Role: role, Parts: parts})
	}
	if len(tools) > 0 {
		declarations := []map[string]any{}
		for _, t := range tools {
			declarations = append(declarations, map[string]any{"name": t.Name, "description": t.Description, "parameters": t.Parameters})
		}
		body.Tools = []map[string]any{{"functionDeclarations": declarations}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return agent.Message{}, err
	}
	endpoint := p.BaseURL + "/models/" + url.PathEscape(p.Model) + ":generateContent?key=" + url.QueryEscape(p.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return agent.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.Client.Do(req)
	if err != nil {
		return agent.Message{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return agent.Message{}, readHTTPError(resp)
	}
	var result struct {
		Candidates []struct {
			Content content `json:"content"`
		}
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
	out := agent.Message{Role: "assistant"}
	if result.UsageMetadata.TotalTokenCount > 0 {
		out.Usage = &agent.Usage{InputTokens: result.UsageMetadata.PromptTokenCount, OutputTokens: result.UsageMetadata.CandidatesTokenCount, TotalTokens: result.UsageMetadata.TotalTokenCount}
	}
	for _, piece := range result.Candidates[0].Content.Parts {
		if piece.Text != "" {
			out.Content += piece.Text
		}
		if piece.FunctionCall != nil {
			args, _ := json.Marshal(piece.FunctionCall.Args)
			out.ToolCalls = append(out.ToolCalls, agent.ToolCall{ID: piece.FunctionCall.Name, Name: piece.FunctionCall.Name, Arguments: string(args)})
		}
	}
	return out, nil
}
