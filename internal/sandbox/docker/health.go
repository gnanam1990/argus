package docker

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// httpHealthCheck polls the guest's /status endpoint until it responds 200 or
// the attempts are exhausted, so Provision returns only once guestd is ready.
func httpHealthCheck(ctx context.Context, baseURL string) error {
	const (
		attempts = 50
		interval = 200 * time.Millisecond
	)
	client := &http.Client{Timeout: interval}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/status", nil)
		res, err := client.Do(req)
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("status %d", res.StatusCode)
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
	return fmt.Errorf("guest did not become healthy after %d attempts: %w", attempts, lastErr)
}
