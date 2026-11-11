package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/rs/zerolog"
)

// purgeAPI is the Cloudflare API base; tests replace it.
var purgeAPI = "https://api.cloudflare.com/client/v4"

// purge clears urls from the zone's cache: by URL, in concurrent batches of
// p.BatchSize paced to p.URLsPerSecond, or the whole zone when there are
// more than p.EverythingOver. If a batch still fails transiently after its
// retries, it purges the whole zone once instead: the sync has already
// happened, so the next deploy would see nothing changed and never retry
// those URLs. A permanent failure (a 4xx, success:false) is returned, so it
// is seen rather than papered over. It returns how many URLs were purged.
func purge(ctx context.Context, p *config.Purge, token string, urls []string, log zerolog.Logger) (int, error) {
	if len(urls) == 0 {
		return 0, nil
	}
	c := &purgeClient{
		zone: p.ZoneID, token: token, throttle: &throttle{},
		limit: newRateLimiter(p.URLsPerSecond),
		http: &http.Client{
			Timeout: 60 * time.Second,
			// Never resend the token somewhere else, even on the same host.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	if len(urls) > p.EverythingOver {
		log.Info().Int("urls", len(urls)).Msg("Purging the whole zone")
		if err := c.everything(ctx); err != nil {
			return 0, fmt.Errorf("purging cache: %w", err)
		}
		return len(urls), nil
	}
	err := c.byURL(ctx, urls, p.BatchSize)
	switch {
	case err == nil:
		return len(urls), nil
	case ctx.Err() != nil || !retryable(err):
		return 0, fmt.Errorf("purging cache: %w", err)
	}
	log.Warn().Err(err).Int("urls", len(urls)).Msg("Purge by URL failed; purging the whole zone")
	if err2 := c.everything(ctx); err2 != nil {
		return 0, fmt.Errorf("purging cache: %w (purging the whole zone instead also failed: %w)", err, err2)
	}
	return len(urls), nil
}

type purgeClient struct {
	zone, token string
	http        *http.Client
	throttle    *throttle
	limit       *rateLimiter
}

// byURL sends every batch at once, each waiting its turn under the URL rate
// limit. The first failure cancels the rest.
func (c *purgeClient) byURL(ctx context.Context, urls []string, size int) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for i := 0; i < len(urls); i += size {
		batch := urls[i:min(i+size, len(urls))]
		wg.Go(func() {
			err := c.limit.take(ctx, len(batch))
			if err == nil {
				err = c.post(ctx, map[string]any{"files": batch})
			}
			if err != nil { // the first is the real failure; later ones are its cancellation
				once.Do(func() { first = err; cancel() })
			}
		})
	}
	wg.Wait()
	return first
}

func (c *purgeClient) everything(ctx context.Context) error {
	return c.post(ctx, map[string]any{"purge_everything": true})
}

// purgeResponse is the part of Cloudflare's API envelope we read.
type purgeResponse struct {
	Success bool `json:"success"`
	Errors  []struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// post sends one purge_cache request, retrying 429 and 5xx answers.
func (c *purgeClient) post(ctx context.Context, body map[string]any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	endpoint := strings.TrimRight(purgeAPI, "/") + "/zones/" + c.zone + "/purge_cache"
	return withRetry(ctx, c.throttle, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.http.Do(req)
		if err != nil {
			return err // a *url.Error: retried
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			return &transientHTTPError{status: resp.StatusCode, after: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
		case resp.StatusCode/100 == 3:
			return fmt.Errorf("HTTP %d: unexpected redirect", resp.StatusCode)
		}
		var r purgeResponse
		if err := json.Unmarshal(raw, &r); err != nil {
			return fmt.Errorf("HTTP %d: unreadable response", resp.StatusCode)
		}
		if r.Success && resp.StatusCode/100 == 2 {
			return nil
		}
		msgs := make([]string, 0, len(r.Errors))
		for _, e := range r.Errors {
			msgs = append(msgs, fmt.Sprintf("%d %s", e.Code, e.Message))
		}
		if len(msgs) == 0 {
			msgs = append(msgs, "no error detail")
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.Join(msgs, "; "))
	})
}

// rateLimiter paces purge URLs to a per-second budget, allowing a burst of
// one second's worth. A taker may run the balance negative and then waits
// for it to refill, so a batch is never split.
type rateLimiter struct {
	mu     sync.Mutex
	rate   float64
	tokens float64
	last   time.Time
}

func newRateLimiter(perSecond int) *rateLimiter {
	r := float64(max(perSecond, 1))
	return &rateLimiter{rate: r, tokens: r, last: time.Now()}
}

func (l *rateLimiter) take(ctx context.Context, n int) error {
	l.mu.Lock()
	now := time.Now()
	l.tokens = min(l.rate, l.tokens+now.Sub(l.last).Seconds()*l.rate)
	l.last = now
	l.tokens -= float64(n)
	debt := -l.tokens
	l.mu.Unlock()
	if debt <= 0 {
		return ctx.Err()
	}
	return sleep(ctx, time.Duration(debt/l.rate*float64(time.Second)))
}
