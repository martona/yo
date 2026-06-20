// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"context"
	"fmt"

	"github.com/martona/yo/internal/config"
)

// Provider is one LLM backend. Implementations build their own native request,
// call their API, and normalize the reply to a Result.
type Provider interface {
	// Generate sends the query and returns a normalized Result.
	Generate(ctx context.Context, query string) (Result, error)
	// Request returns the assembled request body without sending it (for --dry-run).
	Request(query string) ([]byte, error)
}

// New selects a provider from the resolved config.
func New(cfg config.Config) (Provider, error) {
	switch cfg.Provider {
	case "anthropic":
		return newAnthropic(cfg), nil
	case "openai":
		return newOpenAI(cfg), nil
	default:
		return nil, fmt.Errorf("provider %q not supported", cfg.Provider)
	}
}
