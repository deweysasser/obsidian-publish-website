package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/rs/zerolog"
)

const testToken = "sekrit-token"

func usePurgeAPI(t *testing.T, u string) {
	t.Helper()
	old := purgeAPI
	purgeAPI = u
	t.Cleanup(func() { purgeAPI = old })
}

// purgeServer records purge requests; respond decides each answer.
type purgeServer struct {
	mu      sync.Mutex
	paths   []string
	auths   []string
	bodies  []map[string]any
	respond func(n int, body map[string]any, w http.ResponseWriter)
}

func (s *purgeServer) start(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]any
		_ = json.NewDecoder(r.Body).Decode(&b)
		s.mu.Lock()
		s.paths = append(s.paths, r.Method+" "+r.URL.Path)
		s.auths = append(s.auths, r.Header.Get("Authorization"))
		s.bodies = append(s.bodies, b)
		n := len(s.bodies)
		respond := s.respond
		s.mu.Unlock()
		if respond != nil {
			respond(n, b, w)
			return
		}
		_, _ = w.Write([]byte(`{"success":true,"errors":[]}`))
	}))
	t.Cleanup(srv.Close)
	usePurgeAPI(t, srv.URL)
	t.Setenv("TEST_PURGE_TOKEN", testToken)
}

func (s *purgeServer) count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, b := range s.bodies {
		if b[key] != nil {
			n++
		}
	}
	return n
}

func purgeCfg(batch, over int) *config.Purge {
	return &config.Purge{ZoneID: "zone1", APIToken: "env:TEST_PURGE_TOKEN", BatchSize: batch, URLsPerSecond: 100000, EverythingOver: over}
}

func urlList(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("https://example.org/%d.html", i)
	}
	return out
}

func doPurge(p *config.Purge, urls []string) (int, error) {
	return purge(context.Background(), p, testToken, urls, zerolog.Nop())
}

func TestPurgeBatches(t *testing.T) {
	s := &purgeServer{}
	s.start(t)
	n, err := doPurge(purgeCfg(30, 500), urlList(65))
	if err != nil || n != 65 {
		t.Fatalf("n %d err %v", n, err)
	}
	if len(s.bodies) != 3 {
		t.Fatalf("%d requests, want 3", len(s.bodies))
	}
	got := map[string]bool{}
	sizes := map[int]int{}
	for _, b := range s.bodies {
		files := b["files"].([]any)
		sizes[len(files)]++
		for _, u := range files {
			got[u.(string)] = true
		}
	}
	if len(got) != 65 || sizes[30] != 2 || sizes[5] != 1 {
		t.Errorf("batches %v covering %d URLs", sizes, len(got))
	}
	for i := range s.paths {
		if s.paths[i] != "POST /zones/zone1/purge_cache" || s.auths[i] != "Bearer "+testToken {
			t.Errorf("request %d: %s auth %q", i, s.paths[i], s.auths[i])
		}
	}
}

func TestPurgeBatchesRunConcurrently(t *testing.T) {
	b := newBarrier(5)
	s := &purgeServer{respond: func(_ int, _ map[string]any, w http.ResponseWriter) {
		b.wait() // all five batches must be in flight at once to pass quickly
		_, _ = w.Write([]byte(`{"success":true}`))
	}}
	s.start(t)
	start := time.Now()
	if _, err := doPurge(purgeCfg(100, 5000), urlList(500)); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Error("batches ran one at a time")
	}
}

func TestPurgeRateLimit(t *testing.T) {
	var waits []time.Duration
	old := sleep
	sleep = func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil }
	t.Cleanup(func() { sleep = old })
	l := newRateLimiter(100)
	ctx := context.Background()
	_ = l.take(ctx, 100) // the one-second burst: free
	_ = l.take(ctx, 50)  // half a second of debt
	if len(waits) != 1 || waits[0] < 450*time.Millisecond || waits[0] > 510*time.Millisecond {
		t.Errorf("waits %v, want one of about 500ms", waits)
	}
}

func TestPurgeEverythingOverThreshold(t *testing.T) {
	s := &purgeServer{}
	s.start(t)
	if n, err := doPurge(purgeCfg(30, 10), urlList(11)); err != nil || n != 11 {
		t.Fatalf("n %d err %v", n, err)
	}
	if len(s.bodies) != 1 || s.bodies[0]["purge_everything"] != true || s.bodies[0]["files"] != nil {
		t.Errorf("bodies %v", s.bodies)
	}
	// At the threshold, still by URL.
	s.bodies = nil
	if _, err := doPurge(purgeCfg(30, 10), urlList(10)); err != nil || s.bodies[0]["files"] == nil {
		t.Errorf("at threshold: %v %v", s.bodies, err)
	}
}

func TestPurgeNothing(t *testing.T) {
	s := &purgeServer{}
	s.start(t)
	if n, err := doPurge(purgeCfg(30, 500), nil); n != 0 || err != nil || len(s.bodies) != 0 {
		t.Errorf("n %d err %v requests %d", n, err, len(s.bodies))
	}
}

func TestPurgeRetriesTransientAnswers(t *testing.T) {
	fastRetries(t)
	for _, status := range []int{http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable} {
		s := &purgeServer{respond: func(n int, _ map[string]any, w http.ResponseWriter) {
			if n < 3 {
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`<html>bad gateway</html>`))
				return
			}
			_, _ = w.Write([]byte(`{"success":true}`))
		}}
		s.start(t)
		if _, err := doPurge(purgeCfg(30, 500), urlList(2)); err != nil {
			t.Fatalf("%d: %v", status, err)
		}
		if len(s.bodies) != 3 || s.count("purge_everything") != 0 {
			t.Errorf("%d: requests %v", status, s.bodies)
		}
	}
}

func TestPurgeFallsBackToEverythingOnlyWhenTransient(t *testing.T) {
	fastRetries(t)
	s := &purgeServer{respond: func(_ int, b map[string]any, w http.ResponseWriter) {
		if b["files"] != nil {
			w.WriteHeader(http.StatusServiceUnavailable) // every retry
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}}
	s.start(t)
	n, err := doPurge(purgeCfg(30, 500), urlList(40))
	if err != nil || n != 40 {
		t.Fatalf("n %d err %v", n, err)
	}
	if s.count("purge_everything") != 1 {
		t.Errorf("bodies %v", s.bodies)
	}
}

func TestPurgePermanentFailures(t *testing.T) {
	fastRetries(t)
	for name, tc := range map[string]struct {
		status int
		body   string
		want   string
	}{
		"success false at 200": {200, `{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`, "HTTP 200: 10000 Authentication error"},
		"400 with detail":      {400, `{"success":false,"errors":[{"code":1012,"message":"bad url"}]}`, "1012 bad url"},
		"400 claiming success": {400, `{"success":true}`, "HTTP 400: no error detail"},
		"non-JSON 403":         {403, `<html>denied</html>`, "HTTP 403: unreadable response"},
		"redirect":             {307, ``, "HTTP 307: unexpected redirect"},
	} {
		t.Run(name, func(t *testing.T) {
			s := &purgeServer{respond: func(_ int, _ map[string]any, w http.ResponseWriter) {
				if tc.status == 307 {
					w.Header().Set("Location", "/elsewhere")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}}
			s.start(t)
			_, err := doPurge(purgeCfg(30, 500), urlList(1))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "sekrit") {
				t.Error("token in the error")
			}
			if len(s.bodies) != 1 {
				t.Errorf("%d requests: a permanent failure must not be retried or fall back", len(s.bodies))
			}
		})
	}
}

func TestPurgeFallbackFailureReportsBoth(t *testing.T) {
	fastRetries(t)
	s := &purgeServer{respond: func(_ int, b map[string]any, w http.ResponseWriter) {
		if b["files"] != nil {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"no purge everything"}]}`))
	}}
	s.start(t)
	_, err := doPurge(purgeCfg(30, 500), urlList(1))
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") || !strings.Contains(err.Error(), "9109 no purge everything") || strings.Contains(err.Error(), "sekrit") {
		t.Errorf("err %v", err)
	}
}

func TestPurgeCancelledDoesNotFallBack(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &purgeServer{respond: func(_ int, _ map[string]any, w http.ResponseWriter) {
		cancel()
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusBadGateway)
	}}
	s.start(t)
	if _, err := purge(ctx, purgeCfg(30, 500), testToken, urlList(1), zerolog.Nop()); err == nil {
		t.Fatal("no error")
	}
	if s.count("purge_everything") != 0 {
		t.Error("purged everything after cancellation")
	}
}
