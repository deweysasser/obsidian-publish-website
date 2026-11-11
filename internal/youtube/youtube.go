// Package youtube recognizes YouTube video URLs and finds out whether a
// video's owner allows it to be embedded on other sites.
package youtube

import (
	"context"
	"encoding/json"
	"fmt"
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

// Endpoint is YouTube's oEmbed endpoint; tests replace it.
var Endpoint = "https://www.youtube.com/oembed"

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
	c := &Checker{Endpoint: Endpoint, Client: &http.Client{Timeout: 10 * time.Second},
		path: path, known: map[string]entry{}}
	if b, err := os.ReadFile(path); err == nil { // #nosec G304 -- the cache path this program chose
		_ = json.Unmarshal(b, &c.known)
	}
	return c
}

// Result says how a Prefetch went.
type Result struct {
	Asked   int // lookups attempted (ids without a fresh cached answer)
	Refused int // answered: the owner disallows embedding
	Failed  int // got no answer: network, TLS, timeout, or an unexpected status
	// Unknown is how many failed with no cached answer to fall back on.
	// Embeddable reports those as embeddable, which may be wrong.
	Unknown int
	Err     error // the first failure, as a sample; nil when Failed is 0
}

// Prefetch asks YouTube about every id without a fresh cached answer. A
// lookup that fails keeps any older cached answer; Result counts both kinds
// of failure, so the caller can tell a flaky video from a check that never
// worked at all.
func (c *Checker) Prefetch(ctx context.Context, ids []string) Result {
	var (
		res Result
		rmu sync.Mutex
		wg  sync.WaitGroup
	)
	fail := func(id string, err error) {
		c.mu.Lock()
		_, cached := c.known[id]
		c.mu.Unlock()
		rmu.Lock()
		defer rmu.Unlock()
		res.Failed++
		if !cached {
			res.Unknown++
		}
		if res.Err == nil {
			res.Err = err
		}
	}
	todo := make(chan string)
	for range 8 {
		wg.Go(func() {
			for id := range todo {
				ok, err := c.ask(ctx, id)
				if err != nil {
					fail(id, err)
					continue
				}
				c.mu.Lock()
				c.known[id] = entry{ok, time.Now()}
				c.mu.Unlock()
				if !ok {
					rmu.Lock()
					res.Refused++
					rmu.Unlock()
				}
			}
		})
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
		res.Asked++
		select {
		case todo <- id:
		case <-ctx.Done(): // out of time: never asked, so no answer either
			fail(id, ctx.Err())
		}
	}
	close(todo)
	wg.Wait()
	return res
}

// ask returns whether YouTube allows embedding id, or an error when it gave
// no answer.
func (c *Checker) ask(ctx context.Context, id string) (bool, error) {
	q := url.Values{"format": {"json"}, "url": {"https://www.youtube.com/watch?v=" + id}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return false, err
	}
	resp, err := c.Client.Do(req)
	if err != nil {
		return false, err
	}
	_ = resp.Body.Close() // only the status matters
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("oEmbed answered HTTP %d", resp.StatusCode)
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
	// Readable by others, like the vault it lives in: the cache holds only
	// public video IDs, and a container runner may read it as another user.
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil { // #nosec G301 -- see above
		return err
	}
	return os.WriteFile(c.path, b, 0o644) // #nosec G306 -- see above
}
