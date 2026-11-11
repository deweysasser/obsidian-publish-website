package deploy

import (
	"context"
	"crypto/x509"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func fastRetries(t *testing.T) {
	t.Helper()
	a, b, c, d := retryAttempts, retryBase, retryCap, retryAfterCap
	retryAttempts, retryBase, retryCap, retryAfterCap = 4, time.Millisecond, 5*time.Millisecond, 50*time.Millisecond
	t.Cleanup(func() { retryAttempts, retryBase, retryCap, retryAfterCap = a, b, c, d })
}

func TestWithRetry(t *testing.T) {
	fastRetries(t)
	ctx := context.Background()
	calls := 0
	err := withRetry(ctx, &throttle{}, func() error {
		if calls++; calls < 3 {
			return &url.Error{Op: "Put", URL: "u", Err: &transientHTTPError{status: 429}}
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Errorf("throttled: calls %d err %v", calls, err)
	}

	calls = 0
	start := time.Now()
	_ = withRetry(ctx, &throttle{}, func() error {
		if calls++; calls == 1 {
			return &transientHTTPError{status: 503, after: 30 * time.Millisecond}
		}
		return nil
	})
	if time.Since(start) < 30*time.Millisecond {
		t.Error("Retry-After not honoured")
	}

	for name, e := range map[string]error{
		"4xx":      minio.ErrorResponse{StatusCode: 403, Code: "AccessDenied"},
		"plain":    errors.New("bad"),
		"canceled": context.Canceled,
	} {
		calls = 0
		if err := withRetry(ctx, &throttle{}, func() error { calls++; return e }); err == nil || calls != 1 {
			t.Errorf("%s: retried (%d calls)", name, calls)
		}
	}

	calls = 0
	if err := withRetry(ctx, &throttle{}, func() error { calls++; return minio.ErrorResponse{StatusCode: 500} }); err == nil || calls != retryAttempts {
		t.Errorf("5xx: %d calls, want %d", calls, retryAttempts)
	}
}

func TestRetryAfterIsCapped(t *testing.T) {
	fastRetries(t) // cap 50ms
	start := time.Now()
	calls := 0
	_ = withRetry(context.Background(), &throttle{}, func() error {
		if calls++; calls == 1 {
			return &transientHTTPError{status: 429, after: time.Hour}
		}
		return nil
	})
	if d := time.Since(start); d > time.Second {
		t.Errorf("waited %v for a Retry-After of an hour", d)
	}
}

func TestRetryStopsWhenCancelledWhileWaiting(t *testing.T) {
	retryAfterCap = time.Hour
	t.Cleanup(func() { retryAfterCap = 2 * time.Minute })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(20*time.Millisecond, cancel)
	start := time.Now()
	err := withRetry(ctx, &throttle{}, func() error { return &transientHTTPError{status: 429, after: time.Hour} })
	if err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("err %v after %v", err, time.Since(start))
	}
}

func TestThrottleIsShared(t *testing.T) {
	fastRetries(t)
	th := &throttle{}
	th.pause(60 * time.Millisecond) // as if another worker had been told to wait
	start := time.Now()
	var ran time.Duration
	if err := withRetry(context.Background(), th, func() error { ran = time.Since(start); return nil }); err != nil {
		t.Fatal(err)
	}
	if ran < 50*time.Millisecond {
		t.Errorf("ran after %v, before the shared pause ended", ran)
	}

	// A throttling answer sets the shared pause for everyone.
	th = &throttle{}
	calls := 0
	_ = withRetry(context.Background(), th, func() error {
		if calls++; calls == 1 {
			return &transientHTTPError{status: 429, after: 40 * time.Millisecond}
		}
		return nil
	})
	th.mu.Lock()
	set := !th.until.IsZero()
	th.mu.Unlock()
	if !set {
		t.Error("throttling did not pause the shared throttle")
	}
}

func TestRetryable(t *testing.T) {
	timeout := &url.Error{Op: "Put", URL: "u", Err: &net.OpError{Op: "read", Err: errTimeoutLike{}}}
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"throttled":         {&url.Error{Err: &transientHTTPError{status: 503}}, true},
		"network":           {&url.Error{Op: "Put", Err: errors.New("connection reset")}, true},
		"timeout":           {timeout, true},
		"header timeout":    {&url.Error{Op: "Put", Err: context.DeadlineExceeded}, true},
		"5xx":               {minio.ErrorResponse{StatusCode: 502}, true},
		"4xx":               {minio.ErrorResponse{StatusCode: 404, Code: "NoSuchBucket"}, false},
		"per-key SlowDown":  {minio.ErrorResponse{Code: "SlowDown"}, true},
		"per-key Internal":  {minio.ErrorResponse{Code: "InternalError"}, true},
		"per-key denied":    {minio.ErrorResponse{Code: "AccessDenied"}, false},
		"unknown CA":        {&url.Error{Err: x509.UnknownAuthorityError{}}, false},
		"bad hostname":      {&url.Error{Err: x509.HostnameError{Certificate: &x509.Certificate{}, Host: "h"}}, false},
		"no such host":      {&url.Error{Err: &net.DNSError{IsNotFound: true}}, false},
		"http to https":     {&url.Error{Err: errors.New("http: server gave HTTP response to HTTPS client")}, false},
		"caller cancelled":  {&url.Error{Err: context.Canceled}, false},
		"plain local error": {errors.New("open x: permission denied"), false},
	} {
		if got := retryable(tc.err); got != tc.want {
			t.Errorf("%s: retryable = %v, want %v", name, got, tc.want)
		}
	}
}

type errTimeoutLike struct{}

func (errTimeoutLike) Error() string   { return "i/o timeout" }
func (errTimeoutLike) Timeout() bool   { return true }
func (errTimeoutLike) Temporary() bool { return true }

func TestThrottleTransport(t *testing.T) {
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n++; n == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := &http.Client{Transport: &throttleTransport{base: http.DefaultTransport}}
	_, err := c.Get(srv.URL)
	var te *transientHTTPError
	if !errors.As(err, &te) || te.status != 429 || te.after != 7*time.Second {
		t.Errorf("err %v", err)
	}
	resp, err := c.Get(srv.URL)
	if err != nil || resp.StatusCode != 200 {
		t.Errorf("resp %v err %v", resp, err)
	} else {
		_ = resp.Body.Close()
	}
}

func TestRetryAfter(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for h, want := range map[string]time.Duration{
		"":                              0,
		"3":                             3 * time.Second,
		"-3":                            0,
		"1.5":                           0,
		"junk":                          0,
		"Thu, 01 Jan 2026 00:00:10 GMT": 10 * time.Second,
		"Wed, 31 Dec 2025 00:00:00 GMT": 0,
	} {
		if got := retryAfter(h, now); got != want {
			t.Errorf("%q: %v, want %v", h, got, want)
		}
	}
}
