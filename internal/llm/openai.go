// SPDX-License-Identifier: GPL-3.0-or-later
//
// OpenAI provider via the Responses API (/v1/responses), mirroring yoshell's
// yo_build_responses_api_request / yo_parse_responses_api_response. Differs from
// Anthropic: system prompt goes in "instructions", the turn in "input", tools
// are flat function definitions with strict mode, tool_choice is "required", and
// function calls come back in output[] with arguments as a JSON string.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/martona/yo/internal/config"
)

const (
	openaiDefaultURL = "https://api.openai.com/v1/responses"
	// Headroom for reasoning-style models that spend tokens before the call.
	openaiMaxTokens = 4096
)

type openaiProvider struct {
	model   string
	key     string
	baseURL string
	profile CommandProfile
}

func newOpenAI(cfg config.Config) *openaiProvider {
	return &openaiProvider{model: cfg.Model, key: cfg.Key, baseURL: cfg.BaseURL, profile: DetectCommandProfile()}
}

// Responses API function tool: flat (type/name/description/parameters/strict),
// not nested under "function" like Chat Completions.
type openaiTool struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
	Strict      bool           `json:"strict"`
}

type openaiRequest struct {
	Model           string       `json:"model"`
	Instructions    string       `json:"instructions"`
	Input           string       `json:"input"`
	MaxOutputTokens int          `json:"max_output_tokens"`
	Tools           []openaiTool `json:"tools"`
	ToolChoice      string       `json:"tool_choice"`
	Store           bool         `json:"store"`
}

type openaiOutputItem struct {
	Type      string `json:"type"`      // "function_call", "reasoning", "message", ...
	Name      string `json:"name"`      // function name (for function_call)
	Arguments string `json:"arguments"` // JSON-encoded string (for function_call)
}

type openaiResponse struct {
	Output []openaiOutputItem `json:"output"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"` // null on success — checked for non-nil + non-empty
}

func (p *openaiProvider) Request(query string) ([]byte, error) {
	req := openaiRequest{
		Model:           p.model,
		Instructions:    openaiSystemPrompt(p.model, "openai", p.profile),
		Input:           query,
		MaxOutputTokens: openaiMaxTokens,
		Tools:           openaiTools(p.profile),
		ToolChoice:      "required", // force a tool call
		Store:           false,      // privacy: don't retain the request
	}
	return json.Marshal(req)
}

func (p *openaiProvider) Generate(ctx context.Context, query string) (Result, error) {
	body, err := p.Request(query)
	if err != nil {
		return Result{}, fmt.Errorf("building request: %w", err)
	}

	url := openaiDefaultURL
	if p.baseURL != "" {
		url = strings.TrimRight(p.baseURL, "/") + "/responses"
	}

	respBody, status, err := postJSON(ctx, url, map[string]string{
		"authorization": "Bearer " + p.key,
	}, body)
	if err != nil {
		return Result{}, err
	}
	return parseOpenAI(respBody, status)
}

func openaiTools(profile CommandProfile) []openaiTool {
	objectSchema := func(props map[string]any, required ...string) map[string]any {
		return map[string]any{
			"type":                 "object",
			"properties":           props,
			"required":             required,
			"additionalProperties": false, // required by strict mode
		}
	}
	return []openaiTool{
		{
			Type:        "function",
			Name:        toolCommand,
			Description: profile.CommandTool + " " + descCommandBias(profile),
			Parameters: objectSchema(map[string]any{
				"command":     map[string]any{"type": "string", "description": profile.CommandField},
				"explanation": map[string]any{"type": "string", "description": descExplainFld},
				"pending":     map[string]any{"type": "boolean", "description": descPending(profile)},
			}, "command", "explanation", "pending"),
			Strict: true,
		},
		{
			Type:        "function",
			Name:        toolChat,
			Description: descChat,
			Parameters: objectSchema(map[string]any{
				"response": map[string]any{"type": "string", "description": descChatFld},
			}, "response"),
			Strict: true,
		},
	}
}

// parseOpenAI maps a Responses API reply (or an error body) to a Result. It
// scans output[] for the first function_call (skipping reasoning/message items)
// and parses its arguments string.
func parseOpenAI(body []byte, status int) (Result, error) {
	var resp openaiResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return Result{}, fmt.Errorf("unexpected response (status %d)", status)
	}
	if resp.Error != nil && resp.Error.Message != "" {
		return Result{}, fmt.Errorf("API error: %s", resp.Error.Message)
	}
	if status < 200 || status >= 300 {
		return Result{}, fmt.Errorf("API returned status %d", status)
	}

	for _, item := range resp.Output {
		if item.Type != "function_call" {
			continue
		}
		switch item.Name {
		case toolCommand:
			var in struct {
				Command     string `json:"command"`
				Explanation string `json:"explanation"`
				Pending     bool   `json:"pending"`
			}
			if err := json.Unmarshal([]byte(item.Arguments), &in); err != nil {
				return Result{}, fmt.Errorf("bad command tool input: %w", err)
			}
			return Result{Type: "command", Command: in.Command, Explanation: in.Explanation, Pending: in.Pending}, nil
		case toolChat:
			var in struct {
				Response string `json:"response"`
			}
			if err := json.Unmarshal([]byte(item.Arguments), &in); err != nil {
				return Result{}, fmt.Errorf("bad chat tool input: %w", err)
			}
			return Result{Type: "chat", Response: in.Response}, nil
		}
	}
	return Result{}, fmt.Errorf("model returned no command or chat")
}
