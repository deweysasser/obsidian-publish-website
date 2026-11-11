package deploy

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
)

// Retry tuning; tests shrink these.
var (
	retryAttempts = 8
	retryBase     = 250 * time.Millisecond
	retryCap      = 30 * time.Second
	retryAfterCap = 2 * time.Minute
)

// sleep waits for d or until ctx is done; tests replace it.
var sleep = func(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// transientHTTPError is an HTTP answer worth retrying (429 and 503 from R2;
// 429 and 5xx from the purge API), with the server's Retry-After.
type transientHTTPError struct {
	status int
	after  time.Duration
}

func (e *transientHTTPError) Error() string {
	if e.after > 0 {
		return fmt.Sprintf("HTTP %d (retry after %s)", e.status, e.after)
	}
	return fmt.Sprintf("HTTP %d", e.status)
}

// transientError marks an error the caller knows is worth retrying.
type transientError struct{ error }

func (e transientError) Unwrap() error { return e.error }

// throttled reports whether the server asked us to slow down, as opposed to
// failing: every caller sharing its throttle should then wait, not just this one.
func (e *transientHTTPError) throttled() bool {
	return e.status == http.StatusTooManyRequests || e.status == http.StatusServiceUnavailable
}

// retryAfter parses a Retry-After header (seconds or an HTTP date).
func retryAfter(h string, now time.Time) time.Duration {
	if h == "" {
		return 0
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h)); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// throttle is a pause shared by every worker talking to one service. When
// one worker is told to slow down, all of them wait, instead of the others
// carrying on at full width into the same limit.
type throttle struct {
	mu    sync.Mutex
	until time.Time
}

func (t *throttle) pause(d time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if u := time.Now().Add(d); u.After(t.until) {
		t.until = u
	}
}

func (t *throttle) wait(ctx context.Context) error {
	for {
		t.mu.Lock()
		d := time.Until(t.until)
		t.mu.Unlock()
		if d <= 0 {
			return ctx.Err()
		}
		if err := sleep(ctx, d); err != nil {
			return err
		}
	}
}

// withRetry runs op until it succeeds, fails permanently, or runs out of
// attempts. A throttling answer pauses everyone sharing t for at least its
// Retry-After; other transient errors (network, timeouts, 5xx) back off this
// caller only, exponentially with full jitter.
func withRetry(ctx context.Context, t *throttle, op func() error) error {
	var err error
	for attempt := 0; ; attempt++ {
		if werr := t.wait(ctx); werr != nil {
			if err != nil {
				return err
			}
			return werr
		}
		if err = op(); err == nil || ctx.Err() != nil || !retryable(err) || attempt+1 >= retryAttempts {
			return err
		}
		wait := time.Duration(rand.Int64N(int64(min(retryCap, retryBase<<attempt)) + 1)) // #nosec G404 -- jitter, not security
		var te *transientHTTPError
		if errors.As(err, &te) && te.throttled() {
			t.pause(max(wait, min(te.after, retryAfterCap)))
			continue
		}
		if sleep(ctx, wait) != nil {
			return err
		}
	}
}

// retryableS3Codes are per-key DeleteObjects error codes (which arrive with
// no HTTP status) that mean "try again".
var retryableS3Codes = map[string]bool{
	"SlowDown": true, "InternalError": true, "ServiceUnavailable": true, "RequestTimeout": true,
}

// retryable reports whether err is worth another attempt: throttling, a
// server error, a timeout, or a dropped connection. Not: a 4xx answer (bad
// credentials, no such bucket), a certificate or TLS failure, an unknown
// host, or the caller's own cancellation; retrying those only delays the
// failure.
func retryable(err error) bool {
	var (
		te        *transientHTTPError
		s3        minio.ErrorResponse
		unknownCA x509.UnknownAuthorityError
		hostname  x509.HostnameError
		invalid   x509.CertificateInvalidError
		verify    *tls.CertificateVerificationError
		record    tls.RecordHeaderError
		dns       *net.DNSError
		ue        *url.Error
	)
	switch {
	case errors.Is(err, context.Canceled):
		return false
	case errors.As(err, &te), errors.As(err, new(transientError)):
		return true
	case errors.As(err, &s3) && (s3.StatusCode != 0 || s3.Code != ""):
		if s3.StatusCode == 0 {
			return retryableS3Codes[s3.Code]
		}
		return s3.StatusCode >= 500
	case errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalid),
		errors.As(err, &verify), errors.As(err, &record):
		return false
	case errors.As(err, &dns) && dns.IsNotFound:
		return false
	case strings.Contains(err.Error(), "server gave HTTP response to HTTPS client"): // net/http drops the type
		return false
	}
	return errors.As(err, &ue) // network failures and timeouts, including net/http's own
}
