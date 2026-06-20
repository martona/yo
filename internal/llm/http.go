// SPDX-License-Identifier: GPL-3.0-or-later
package llm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
)

// postJSON POSTs body to url with the given headers (content-type is set
// automatically) and returns the response body and status. A cancelled context
// (Ctrl-C) is reported as a clean "cancelled" error. Shared by all providers.
func postJSON(ctx context.Context, url string, headers map[string]string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("content-type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, 0, fmt.Errorf("cancelled")
		}
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}
	return b, resp.StatusCode, nil
}
