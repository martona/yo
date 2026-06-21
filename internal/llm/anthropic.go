// SPDX-License-Identifier: GPL-3.0-or-later
//
// Anthropic Messages API provider. Mirrors yoshell's yo_build_anthropic_request
// / yo_parse_anthropic_response (readline-8.2.13/yo.c), reduced to the v0.1
// single-shot path (command + chat; no scrollback/docs yet).
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/martona/yo/internal/config"
)

const (
	anthropicDefaultURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion    = "2023-06-01"
	anthropicMaxTokens  = 1024
)

type anthropicProvider struct {
	model   string
	key     string
	baseURL string
}

func newAnthropic(cfg config.Config) *anthropicProvider {
	return &anthropicProvider{model: cfg.Model, key: cfg.Key, baseURL: cfg.BaseURL}
}

type anthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model      string             `json:"model"`
	MaxTokens  int                `json:"max_tokens"`
	System     string             `json:"system"`
	Messages   []anthropicMessage `json:"messages"`
	Tools      []anthropicTool    `json:"tools"`
	ToolChoice map[string]any     `json:"tool_choice"`
}

type anthropicContentBlock struct {
	Type  string          `json:"type"` // "text" | "tool_use"
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

type anthropicResponse struct {
	Type    string                  `json:"type"`
	Content []anthropicContentBlock `json:"content"`
	Error   *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *anthropicProvider) Request(query string) ([]byte, error) {
	req := anthropicRequest{
		Model:      p.model,
		MaxTokens:  anthropicMaxTokens,
		System:     anthropicSystemPrompt(p.model),
		Messages:   []anthropicMessage{{Role: "user", Content: query}},
		Tools:      anthropicTools(),
		ToolChoice: map[string]any{"type": "any"}, // force exactly one tool call
	}
	return json.Marshal(req)
}

func (p *anthropicProvider) Generate(ctx context.Context, query string) (Result, error) {
	body, err := p.Request(query)
	if err != nil {
		return Result{}, fmt.Errorf("building request: %w", err)
	}

	url := anthropicDefaultURL
	if p.baseURL != "" {
		url = strings.TrimRight(p.baseURL, "/") + "/messages"
	}

	respBody, status, err := postJSON(ctx, url, map[string]string{
		"x-api-key":         p.key,
		"anthropic-version": anthropicVersion,
	}, body)
	if err != nil {
		return Result{}, err
	}
	return parseAnthropic(respBody, status)
}

func anthropicTools() []anthropicTool {
	return []anthropicTool{
		{
			Name:        toolCommand,
			Description: descCommand + " " + descCommandBias,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":     map[string]any{"type": "string", "description": descCommandFld},
					"explanation": map[string]any{"type": "string", "description": descExplainFld},
					"pending":     map[string]any{"type": "boolean", "description": descPendingFld},
				},
				"required": []string{"command", "explanation"},
			},
		},
		{
			Name:        toolChat,
			Description: descChat,
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"response": map[string]any{"type": "string", "description": descChatFld},
				},
				"required": []string{"response"},
			},
		},
	}
}

// parseAnthropic maps the API response (or an error body) to a Result. It takes
// the first tool_use block; multiple-tool-call handling is a later robustness
// item (see DESIGN-NOTES Q2).
func parseAnthropic(body []byte, status int) (Result, error) {
	var resp anthropicResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Result{}, fmt.Errorf("unexpected response (status %d)", status)
	}
	if resp.Error != nil {
		return Result{}, fmt.Errorf("API error: %s", resp.Error.Message)
	}
	if status < 200 || status >= 300 {
		return Result{}, fmt.Errorf("API returned status %d", status)
	}

	for _, b := range resp.Content {
		if b.Type != "tool_use" {
			continue
		}
		switch b.Name {
		case toolCommand:
			var in struct {
				Command     string `json:"command"`
				Explanation string `json:"explanation"`
				Pending     bool   `json:"pending"`
			}
			if err := json.Unmarshal(b.Input, &in); err != nil {
				return Result{}, fmt.Errorf("bad command tool input: %w", err)
			}
			return Result{Type: "command", Command: in.Command, Explanation: in.Explanation, Pending: in.Pending}, nil
		case toolChat:
			var in struct {
				Response string `json:"response"`
			}
			if err := json.Unmarshal(b.Input, &in); err != nil {
				return Result{}, fmt.Errorf("bad chat tool input: %w", err)
			}
			return Result{Type: "chat", Response: in.Response}, nil
		}
	}
	return Result{}, fmt.Errorf("model returned no command or chat")
}
