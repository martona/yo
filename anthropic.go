// SPDX-License-Identifier: GPL-3.0-or-later
//
// Anthropic Messages API client. Request/response shapes mirror yoshell's
// yo_build_anthropic_request / yo_parse_anthropic_response (readline-8.2.13/yo.c),
// reduced to the v0.1 single-shot path (command + chat, no scrollback/docs).
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const (
	anthropicDefaultURL = "https://api.anthropic.com/v1/messages"
	anthropicVersion    = "2023-06-01"
	maxTokens           = 1024
)

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
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// buildAnthropicRequest assembles the request body JSON. Shared by the live call
// and by --dry-run (note: the API key is a header, never in the body, so the
// dry-run output is safe to print).
func buildAnthropicRequest(cfg Config, query string) ([]byte, error) {
	req := anthropicRequest{
		Model:     cfg.Model,
		MaxTokens: maxTokens,
		System:    systemPrompt(cfg.Model),
		Messages: []anthropicMessage{
			{Role: "user", Content: query},
		},
		Tools:      buildTools(),
		ToolChoice: map[string]any{"type": "any"}, // force exactly one tool call
	}
	return json.Marshal(req)
}

// callAnthropic performs the request and normalizes the response to a Result.
func callAnthropic(ctx context.Context, cfg Config, query string) (Result, error) {
	if err := cfg.ready(); err != nil {
		return Result{}, err
	}

	body, err := buildAnthropicRequest(cfg, query)
	if err != nil {
		return Result{}, fmt.Errorf("building request: %w", err)
	}

	url := anthropicDefaultURL
	if cfg.BaseURL != "" {
		url = strings.TrimRight(cfg.BaseURL, "/") + "/messages"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", cfg.Key)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return Result{}, fmt.Errorf("cancelled")
		}
		return Result{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return Result{}, fmt.Errorf("reading response: %w", err)
	}

	return parseAnthropicResponse(respBody, resp.StatusCode)
}

// parseAnthropicResponse maps the API response (or an error body) to a Result.
// It takes the first tool_use block; multiple-tool-call handling is a v0.2
// robustness item (see DESIGN-NOTES Q2).
func parseAnthropicResponse(body []byte, status int) (Result, error) {
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
		case "command":
			var in struct {
				Command     string `json:"command"`
				Explanation string `json:"explanation"`
			}
			if err := json.Unmarshal(b.Input, &in); err != nil {
				return Result{}, fmt.Errorf("bad command tool input: %w", err)
			}
			return Result{Type: "command", Command: in.Command, Explanation: in.Explanation}, nil
		case "chat":
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
