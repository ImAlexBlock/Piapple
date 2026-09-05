// Shared HTTP/SSE helpers used by the compiled provider adapters.
package provider

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ImAlexBlock/Piapple/internal/agent"
)

func applyHeaders(req *http.Request, cfg Config) {
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}
}

func emitStreamEvent(ctx context.Context, out chan<- agent.StreamEvent, event agent.StreamEvent) bool {
	select {
	case out <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func doJSONWithRetry(ctx context.Context, client *http.Client, method, endpoint string, body []byte, headers map[string]string, maxRetries int, backoff time.Duration) (*http.Response, error) {
	if maxRetries < 0 {
		maxRetries = 0
	}
	if backoff <= 0 {
		backoff = 250 * time.Millisecond
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := client.Do(req)
		if err == nil && !retryableStatus(resp.StatusCode) {
			return resp, nil
		}
		if attempt >= maxRetries {
			if resp != nil {
				return resp, nil
			}
			return nil, err
		}
		if resp != nil {
			resp.Body.Close()
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		delay := backoff * time.Duration(1<<min(attempt, 5))
		if resp != nil {
			if seconds, parseErr := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); parseErr == nil && seconds > 0 {
				delay = time.Duration(seconds) * time.Second
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
}

func retryableStatus(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusTooManyRequests || code >= 500
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// readHTTPErrorBody is useful to callers that need to preserve an error body
// after a retry loop has already consumed a response.
func readHTTPErrorBody(body io.Reader) string {
	data, _ := io.ReadAll(io.LimitReader(body, 64*1024))
	return string(data)
}
