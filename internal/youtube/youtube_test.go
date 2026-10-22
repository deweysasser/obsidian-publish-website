package youtube

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sync/atomic"
	"testing"
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
	c.Prefetch(context.Background(), []string{"okokokokoko", "nonononono1", "flakyflaky1", "okokokokoko"})
	if !c.Embeddable("okokokokoko") || c.Embeddable("nonononono1") || !c.Embeddable("flakyflaky1") || !c.Embeddable("neverseen11") {
		t.Error("classification wrong: 200→yes, 401→no, errors and unknown→yes")
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
