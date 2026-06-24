// SPDX-License-Identifier: GPL-3.0-or-later
//
// OpenAI-compatible Chat Completions provider, for backends that speak the Chat
// Completions API rather than OpenAI's Responses API. Currently xAI (Grok); the
// same client should serve Gemini's OpenAI-compat endpoint later. Differs from
// openai.go (Responses): the system prompt + turn ride in a messages[] array,
// tools are nested under "function", and the call comes back in
// choices[0].message.tool_calls[].function.arguments (a JSON-encoded string).
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/martona/yo/internal/config"
)

const chatMaxTokens = 4096

type chatProvider struct {
	model      string
	key        string
	baseURL    string // override from ~/.yoconf; we append /chat/completions
	defaultURL string // the backend's endpoint when baseURL is empty
	provider   string // label for the system prompt (e.g. "xai")
	profile    CommandProfile
}

// newGrok wires the xAI (Grok) backend onto the shared Chat Completions client.
func newGrok(cfg config.Config) *chatProvider {
	return &chatProvider{
		model:      cfg.Model,
		key:        cfg.Key,
		baseURL:    cfg.BaseURL,
		defaultURL: "https://api.x.ai/v1/chat/completions",
		provider:   "xai",
		profile:    DetectCommandProfile(),
	}
}

// Chat Completions function tool: nested under "function" (vs the Responses API's
// flat tool shape in openai.go).
type chatTool struct {
	Type     string       `json:"type"` // "function"
	Function chatToolFunc `json:"function"`
}

type chatToolFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model      string        `json:"model"`
	Messages   []chatMessage `json:"messages"`
	Tools      []chatTool    `json:"tools"`
	ToolChoice string        `json:"tool_choice"` // "required" forces a tool call
	MaxTokens  int           `json:"max_tokens"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			ToolCalls []struct {
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"` // JSON-encoded string
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (p *chatProvider) Request(query string) ([]byte, error) {
	req := chatRequest{
		Model: p.model,
		Messages: []chatMessage{
			{Role: "system", Content: openaiSystemPrompt(p.model, p.provider, p.profile)},
			{Role: "user", Content: query},
		},
		Tools:      chatTools(p.profile),
		ToolChoice: "required", // force a command/chat tool call
		MaxTokens:  chatMaxTokens,
	}
	return json.Marshal(req)
}

func (p *chatProvider) Generate(ctx context.Context, query string) (Result, error) {
	body, err := p.Request(query)
	if err != nil {
		return Result{}, fmt.Errorf("building request: %w", err)
	}
	url := p.defaultURL
	if p.baseURL != "" {
		url = strings.TrimRight(p.baseURL, "/") + "/chat/completions"
	}
	respBody, status, err := postJSON(ctx, url, map[string]string{
		"authorization": "Bearer " + p.key,
	}, body)
	if err != nil {
		return Result{}, err
	}
	return parseChat(respBody, status)
}

func chatTools(profile CommandProfile) []chatTool {
	objectSchema := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{
			"type":       "object",
			"properties": props,
			"required":   required,
		}
	}
	return []chatTool{
		{
			Type: "function",
			Function: chatToolFunc{
				Name:        toolCommand,
				Description: profile.CommandTool + " " + descCommandBias(profile),
				Parameters: objectSchema(map[string]any{
					"command":     map[string]any{"type": "string", "description": profile.CommandField},
					"explanation": map[string]any{"type": "string", "description": descExplainFld},
					"pending":     map[string]any{"type": "boolean", "description": descPending(profile)},
				}, "command", "explanation", "pending"),
			},
		},
		{
			Type: "function",
			Function: chatToolFunc{
				Name:        toolChat,
				Description: descChat,
				Parameters: objectSchema(map[string]any{
					"response": map[string]any{"type": "string", "description": descChatFld},
				}, "response"),
			},
		},
	}
}

// parseChat maps a Chat Completions reply (or an error body) to a Result. It takes
// the first tool_call of the first choice and parses its arguments string.
func parseChat(body []byte, status int) (Result, error) {
	var resp chatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Result{}, fmt.Errorf("unexpected response (status %d)", status)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return Result{}, fmt.Errorf("API error: %s", resp.Error.Message)
	}
	if status < 200 || status >= 300 {
		return Result{}, fmt.Errorf("API returned status %d", status)
	}
	for _, choice := range resp.Choices {
		for _, tc := range choice.Message.ToolCalls {
			switch tc.Function.Name {
			case toolCommand:
				var in struct {
					Command     string `json:"command"`
					Explanation string `json:"explanation"`
					Pending     bool   `json:"pending"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
					return Result{}, fmt.Errorf("bad command tool input: %w", err)
				}
				return Result{Type: "command", Command: in.Command, Explanation: in.Explanation, Pending: in.Pending}, nil
			case toolChat:
				var in struct {
					Response string `json:"response"`
				}
				if err := json.Unmarshal([]byte(tc.Function.Arguments), &in); err != nil {
					return Result{}, fmt.Errorf("bad chat tool input: %w", err)
				}
				return Result{Type: "chat", Response: in.Response}, nil
			}
		}
	}
	return Result{}, fmt.Errorf("model returned no command or chat")
}
