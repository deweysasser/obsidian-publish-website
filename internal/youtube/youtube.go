// Package youtube recognizes YouTube video URLs and finds out whether a
// video's owner allows it to be embedded on other sites.
package youtube

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var idRe = regexp.MustCompile(`^[A-Za-z0-9_-]{11}$`)

// ID returns the video id of a YouTube watch, short, live, shorts or embed
// URL, or "".
func ID(u *url.URL) string {
	if u == nil || u.Scheme != "https" && u.Scheme != "http" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, p := range []string{"www.", "m."} {
		host = strings.TrimPrefix(host, p)
	}
	id := ""
	switch host {
	case "youtu.be":
		id, _, _ = strings.Cut(strings.TrimPrefix(u.Path, "/"), "/")
	case "youtube.com", "youtube-nocookie.com":
		id = u.Query().Get("v")
		for _, pre := range []string{"/live/", "/shorts/", "/embed/"} {
			if id == "" && strings.HasPrefix(u.Path, pre) {
				id, _, _ = strings.Cut(strings.TrimPrefix(u.Path, pre), "/")
			}
		}
	}
	if !idRe.MatchString(id) {
		return ""
	}
	return id
}

// IDOf parses s and returns its video id, or "".
func IDOf(s string) string {
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	return ID(u)
}

// recheck is how long a cached answer is trusted.
const recheck = 30 * 24 * time.Hour

type entry struct {
	Embeddable bool      `json:"embeddable"`
	Checked    time.Time `json:"checked"`
}

// Checker answers "may this video be embedded?" from YouTube's oEmbed
// endpoint, which refuses (401) videos whose owner disabled embedding.
// Answers are cached on disk; anything unknown counts as embeddable, and the
// page script falls back to a plain link if the player refuses.
type Checker struct {
	Endpoint string
	Client   *http.Client
	path     string
	mu       sync.Mutex
	known    map[string]entry
}

// NewChecker loads the cache at path (missing or unreadable means empty).
func NewChecker(path string) *Checker {
	c := &Checker{Endpoint: "https://www.youtube.com/oembed", Client: &http.Client{Timeout: 10 * time.Second},
		path: path, known: map[string]entry{}}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &c.known)
	}
	return c
}

// Prefetch asks YouTube about every id without a fresh cached answer.
func (c *Checker) Prefetch(ctx context.Context, ids []string) {
	todo := make(chan string)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range todo {
				if ok, known := c.ask(ctx, id); known {
					c.mu.Lock()
					c.known[id] = entry{ok, time.Now()}
					c.mu.Unlock()
				}
			}
		}()
	}
	seen := map[string]bool{}
	for _, id := range ids {
		c.mu.Lock()
		e, cached := c.known[id]
		c.mu.Unlock()
		if seen[id] || cached && time.Since(e.Checked) < recheck {
			continue
		}
		seen[id] = true
		select {
		case todo <- id:
		case <-ctx.Done():
		}
	}
	close(todo)
	wg.Wait()
}

func (c *Checker) ask(ctx context.Context, id string) (embeddable, known bool) {
	q := url.Values{"format": {"json"}, "url": {"https://www.youtube.com/watch?v=" + id}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return false, false
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return false, false
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, true
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false, true
	}
	return false, false
}

// Embeddable reports the cached answer; unknown videos count as embeddable.
func (c *Checker) Embeddable(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.known[id]
	return !ok || e.Embeddable
}

// Save writes the cache.
func (c *Checker) Save() error {
	c.mu.Lock()
	b, err := json.MarshalIndent(c.known, "", " ")
	c.mu.Unlock()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o644)
}
