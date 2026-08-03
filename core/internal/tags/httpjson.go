package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// getJSON is the one Danbooru JSON fetch for this package: retry with
// backoff on 429/5xx (Danbooru rate-limits politely), bounded body reads,
// and the truncated body in terminal errors. The live-validation path used
// to hard-fail on the same rate limit the batch ingester rode out — both
// now share this implementation.
func getJSON(ctx context.Context, client *http.Client, base, path, userAgent string, bodyLimit int64, v any) error {
	var lastErr error
	for attempt := 1; attempt <= 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
			resp.Body.Close()
			switch {
			case rerr != nil:
				lastErr = rerr
			case resp.StatusCode == http.StatusOK:
				return json.Unmarshal(body, v)
			case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
				lastErr = fmt.Errorf("GET %s: %s", path, resp.Status)
			default:
				return fmt.Errorf("GET %s: %s: %.200s", path, resp.Status, body)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	return lastErr
}
