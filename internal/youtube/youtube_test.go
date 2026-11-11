package youtube

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestID(t *testing.T) {
	for in, want := range map[string]string{
		"https://www.youtube.com/watch?v=abcdefghijk":        "abcdefghijk",
		"https://m.youtube.com/watch?v=abcdefghijk&t=10s":    "abcdefghijk",
		"https://youtu.be/abcdefghijk?t=5":                   "abcdefghijk",
		"https://www.youtube.com/live/abcdefghijk?si=x":      "abcdefghijk",
		"https://youtube.com/shorts/abcdefghijk":             "abcdefghijk",
		"https://www.youtube-nocookie.com/embed/abcdefghijk": "abcdefghijk",
		"https://www.youtube.com/playlist?list=PL1":          "",
		"https://example.com/watch?v=abcdefghijk":            "",
		"javascript:alert(1)":                                "",
		"https://youtu.be/short":                             "",
	} {
		u, _ := url.Parse(in)
		if got := ID(u); got != want {
			t.Errorf("%s: got %q want %q", in, got, want)
		}
	}
}

func TestCheckerCachesAndClassifies(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		u, _ := url.Parse(r.URL.Query().Get("url"))
		switch ID(u) {
		case "okokokokoko":
			w.WriteHeader(200)
		case "nonononono1":
			w.WriteHeader(401)
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	cache := filepath.Join(t.TempDir(), "yt.json")
	c := NewChecker(cache)
	c.Endpoint = srv.URL
	res := c.Prefetch(context.Background(), []string{"okokokokoko", "nonononono1", "flakyflaky1", "okokokokoko"})
	if !c.Embeddable("okokokokoko") || c.Embeddable("nonononono1") || !c.Embeddable("flakyflaky1") || !c.Embeddable("neverseen11") {
		t.Error("classification wrong: 200→yes, 401→no, errors and unknown→yes")
	}
	if res.Asked != 3 || res.Refused != 1 || res.Failed != 1 || res.Unknown != 1 || res.Err == nil || !strings.Contains(res.Err.Error(), "HTTP 500") {
		t.Errorf("result %+v", res)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	before := calls.Load()
	c2 := NewChecker(cache)
	c2.Endpoint = srv.URL
	c2.Prefetch(context.Background(), []string{"okokokokoko", "nonononono1", "flakyflaky1"})
	if got := calls.Load() - before; got != 1 {
		t.Errorf("only the unanswered id should be re-checked; made %d calls", got)
	}
	if c2.Embeddable("nonononono1") {
		t.Error("cached answer lost")
	}
}

// TestCheckerReportsFailures pins that a lookup with no answer is counted
// and its error surfaced, not silently treated as success: a container with
// no CA bundle once failed every lookup while logging as if all was well.
func TestCheckerReportsFailures(t *testing.T) {
	ids := []string{"aaaaaaaaaaa", "bbbbbbbbbbb"}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	untrusted := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer untrusted.Close()
	throttled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) }))
	defer throttled.Close()
	for name, tc := range map[string]struct{ endpoint, want string }{
		"untrusted certificate": {untrusted.URL, "certificate"},
		"connection refused":    {closed.URL, "refused"},
		"throttled":             {throttled.URL, "HTTP 429"},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewChecker(filepath.Join(t.TempDir(), "yt.json"))
			c.Endpoint = tc.endpoint // the default client trusts only the system roots
			res := c.Prefetch(context.Background(), ids)
			if res.Asked != 2 || res.Failed != 2 || res.Unknown != 2 || res.Err == nil || !strings.Contains(res.Err.Error(), tc.want) {
				t.Errorf("result %+v, want both failed with %q", res, tc.want)
			}
			if !c.Embeddable(ids[0]) {
				t.Error("an unknown video should still count as embeddable")
			}
		})
	}
}

func TestCheckerFailedRecheckKeepsCachedAnswer(t *testing.T) {
	throttled := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(429) }))
	defer throttled.Close()
	c := NewChecker(filepath.Join(t.TempDir(), "yt.json"))
	c.Endpoint = throttled.URL
	c.known["stalestale1"] = entry{Embeddable: false, Checked: time.Now().Add(-2 * recheck)}
	res := c.Prefetch(context.Background(), []string{"stalestale1"})
	if res.Failed != 1 || res.Unknown != 0 {
		t.Errorf("result %+v: a failed re-check of a cached answer is not unknown", res)
	}
	if c.Embeddable("stalestale1") {
		t.Error("the cached answer was lost")
	}
}

func TestCheckerCountsWhatTimeoutLeftUnasked(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)
	var ids []string
	for i := range 20 { // more than the 8 workers
		ids = append(ids, fmt.Sprintf("vid%08d", i))
	}
	c := NewChecker(filepath.Join(t.TempDir(), "yt.json"))
	c.Endpoint = srv.URL
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	res := c.Prefetch(ctx, ids)
	if res.Asked != 20 || res.Failed != 20 || res.Unknown != 20 {
		t.Errorf("result %+v: every video should be counted as unknown", res)
	}
}
