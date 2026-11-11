package deploy

import (
	"bytes"
	"context"
	"crypto/md5" // #nosec G501 -- matches the store's ETag
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/rs/zerolog"
)

// memStore is an in-memory objectStore.
type memStore struct {
	mu        sync.Mutex
	objects   map[string]string // key -> md5
	types     map[string]string // key -> content type
	puts      []string
	deletes   []string
	putErr    map[string]error
	block     map[string]bool // Put waits for cancellation
	listErr   error
	deleteErr error
	started   chan string // receives each key as its Put starts, if set
	inFlight  int
	peak      int
	barrier   *barrier // Put waits here, if set
}

func newMem(objects map[string]string) *memStore {
	if objects == nil {
		objects = map[string]string{}
	}
	return &memStore{objects: objects, types: map[string]string{}, putErr: map[string]error{}, block: map[string]bool{}}
}

func (m *memStore) List(ctx context.Context) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.objects))
	for k, v := range m.objects {
		out[k] = v
	}
	return out, m.listErr
}

func (m *memStore) Put(ctx context.Context, key, file, contentType string) error {
	m.mu.Lock()
	m.puts = append(m.puts, key)
	m.inFlight++
	m.peak = max(m.peak, m.inFlight)
	err, block, started, b := m.putErr[key], m.block[key], m.started, m.barrier
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.inFlight--; m.mu.Unlock() }()
	if started != nil {
		started <- key
	}
	if b != nil {
		b.wait()
	}
	if block {
		<-ctx.Done()
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	data, err := os.ReadFile(file) // #nosec G304 -- test file
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key], m.types[key] = md5hex(data), contentType
	return nil
}

func (m *memStore) Delete(ctx context.Context, keys []string) error {
	if err := ctx.Err(); err != nil { // as the real store would
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deleteErr != nil {
		return m.deleteErr
	}
	for _, k := range keys {
		delete(m.objects, k)
	}
	m.deletes = append(m.deletes, keys...)
	return nil
}

// barrier releases its waiters once n have arrived, or after a timeout so a
// broken pool fails the test instead of hanging it.
type barrier struct {
	mu      sync.Mutex
	n       int
	arrived int
	open    chan struct{}
}

func newBarrier(n int) *barrier { return &barrier{n: n, open: make(chan struct{})} }

func (b *barrier) wait() {
	b.mu.Lock()
	if b.arrived++; b.arrived == b.n {
		close(b.open)
	}
	b.mu.Unlock()
	select {
	case <-b.open:
	case <-time.After(2 * time.Second):
	}
}

func md5hex(b []byte) string {
	s := md5.Sum(b) // #nosec G401 -- see import
	return hex.EncodeToString(s[:])
}

func writeSite(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for k, v := range files {
		p := filepath.Join(dir, filepath.FromSlash(k))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func opts(concurrency int) syncOptions {
	return syncOptions{concurrency: concurrency, maxDeleteFraction: 0.25}
}

func TestSyncDir(t *testing.T) {
	site := writeSite(t, map[string]string{
		"index.html":       "home",
		"a/index.html":     "new page",
		"style.css":        "changed",
		"img/logo.png":     "same",
		target.Marker:      "",
		"_headers":         "/*\n  Cache-Control: x",
		"_redirects":       "/a /b",
		"pagefind/x.pf_js": "bin",
	})
	store := newMem(map[string]string{
		"index.html":   md5hex([]byte("home")),
		"style.css":    md5hex([]byte("old")),
		"img/logo.png": md5hex([]byte("same")),
		"gone.html":    "abc",
		"_headers":     "def",
		"old/":         "",
	})
	o := opts(4)
	o.maxDeleteFraction = 1
	res, err := syncDir(context.Background(), store, site, o)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/index.html", "pagefind/x.pf_js", "style.css"}; !reflect.DeepEqual(res.uploaded, want) {
		t.Errorf("uploaded %v, want %v", res.uploaded, want)
	}
	if want := []string{"_headers", "gone.html", "old/"}; !reflect.DeepEqual(res.deleted, want) {
		t.Errorf("deleted %v, want %v", res.deleted, want)
	}
	if res.unchanged != 2 {
		t.Errorf("unchanged %d, want 2", res.unchanged)
	}
	for _, k := range []string{target.Marker, "_headers", "_redirects"} {
		if _, ok := store.objects[k]; ok {
			t.Errorf("%s published", k)
		}
	}
	if got := store.types["a/index.html"]; got != "text/html; charset=utf-8" {
		t.Errorf("html content type %q", got)
	}
	if got := store.types["pagefind/x.pf_js"]; got != "application/octet-stream" {
		t.Errorf("unknown content type %q", got)
	}

	// A second run of the same build does nothing.
	store.puts, store.deletes = nil, nil
	res, err = syncDir(context.Background(), store, site, o)
	if err != nil || len(store.puts) != 0 || len(store.deletes) != 0 || res.unchanged != 5 {
		t.Errorf("no-op sync: res %+v puts %v deletes %v err %v", res, store.puts, store.deletes, err)
	}

	// --force-upload re-uploads everything, unchanged or not.
	store.puts = nil
	o.force = true
	res, err = syncDir(context.Background(), store, site, o)
	if err != nil || len(res.uploaded) != 5 || res.unchanged != 0 {
		t.Errorf("forced sync: res %+v err %v", res, err)
	}
}

func TestSyncDirUploadsAssetsBeforePages(t *testing.T) {
	site := writeSite(t, map[string]string{
		"a/index.html": "p", "index.html": "h", "z.css": "c", "img/z.png": "i",
		"pagefind/pagefind-entry.json": "e", "pagefind/pagefind.js": "j",
		"pagefind/fragment/en_1.pf_fragment": "f", "pagefind/index/en_1.pf_index": "x",
	})
	store := newMem(nil)
	if _, err := syncDir(context.Background(), store, site, opts(1)); err != nil {
		t.Fatal(err)
	}
	lastAsset := -1
	firstPage := len(store.puts)
	for i, k := range store.puts {
		if publishedLast(k) {
			firstPage = min(firstPage, i)
		} else {
			lastAsset = i
		}
	}
	if lastAsset > firstPage {
		t.Errorf("an asset went up after a page: %v", store.puts)
	}
	for _, k := range []string{"pagefind/pagefind-entry.json", "pagefind/pagefind.js", "index.html"} {
		if !publishedLast(k) {
			t.Errorf("%s should go up last", k)
		}
	}
	for _, k := range []string{"pagefind/fragment/en_1.pf_fragment", "z.css", "img/z.png", "pagefind.js"} {
		if publishedLast(k) {
			t.Errorf("%s should go up first", k)
		}
	}
}

func TestSyncDirConcurrencyBound(t *testing.T) {
	files := map[string]string{}
	for i := range 20 {
		files[fmt.Sprintf("f%02d.css", i)] = fmt.Sprint(i)
	}
	store := newMem(nil)
	store.barrier = newBarrier(4) // the first four Puts wait for each other
	if _, err := syncDir(context.Background(), store, writeSite(t, files), opts(4)); err != nil {
		t.Fatal(err)
	}
	if store.peak != 4 {
		t.Errorf("peak uploads in flight %d, want exactly the concurrency, 4", store.peak)
	}
}

func TestSyncDirMassDeleteGuard(t *testing.T) {
	remote := map[string]string{}
	for i := range 100 {
		remote[fmt.Sprintf("p%03d.html", i)] = "x"
	}
	site := writeSite(t, map[string]string{"index.html": "h"})
	for name, tc := range map[string]struct {
		files    map[string]string
		fraction float64
		allow    bool
		want     string // "" = allowed
	}{
		"most of the bucket":      {map[string]string{"index.html": "h"}, 0.25, false, "would delete 100 of the bucket's 100"},
		"override":                {map[string]string{"index.html": "h"}, 0.25, true, ""},
		"guard off":               {map[string]string{"index.html": "h"}, 1, false, ""},
		"empty build":             {map[string]string{}, 1, false, "no files to publish"},
		"few deletes, small site": {nil, 0.25, false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			r := map[string]string{}
			for k, v := range remote {
				r[k] = v
			}
			dir := site
			if tc.files == nil { // a 12-object bucket losing 9: under the floor
				r = map[string]string{}
				for i := range 12 {
					r[fmt.Sprintf("p%d.html", i)] = "x"
				}
				dir = writeSite(t, map[string]string{"p0.html": "a", "p1.html": "b", "p2.html": "c"})
			} else if len(tc.files) == 0 {
				dir = t.TempDir()
			}
			store := newMem(r)
			_, err := syncDir(context.Background(), store, dir, syncOptions{concurrency: 2, maxDeleteFraction: tc.fraction, allowMassDelete: tc.allow})
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v, want %q", err, tc.want)
			}
			if len(store.puts) != 0 || len(store.deletes) != 0 {
				t.Errorf("refused, but uploaded %v / deleted %v", store.puts, store.deletes)
			}
		})
	}
}

func TestSyncDirNoDeletesAfterFailedUpload(t *testing.T) {
	site := writeSite(t, map[string]string{"a.html": "a", "b.html": "b", "c.html": "c", "d.html": "d"})
	store := newMem(map[string]string{"gone.html": "x"})
	store.putErr["b.html"] = errors.New("boom")
	if _, err := syncDir(context.Background(), store, site, opts(2)); err == nil || !errors.Is(err, store.putErr["b.html"]) {
		t.Fatalf("err %v", err)
	}
	if len(store.deletes) != 0 {
		t.Errorf("deleted %v after a failed upload", store.deletes)
	}
	if _, ok := store.objects["gone.html"]; !ok {
		t.Error("gone.html deleted")
	}
}

func TestSyncDirFirstErrorStopsTheRest(t *testing.T) {
	// One worker: after "a" fails, nothing else is uploaded. (It relies on
	// the worker's own ctx check; see uploadAll.)
	site := writeSite(t, map[string]string{"a.html": "a", "b.html": "b", "c.html": "c"})
	store := newMem(nil)
	store.putErr["a.html"] = errors.New("boom")
	if _, err := syncDir(context.Background(), store, site, opts(1)); err == nil {
		t.Fatal("no error")
	}
	if !reflect.DeepEqual(store.puts, []string{"a.html"}) {
		t.Errorf("puts %v, want only a.html", store.puts)
	}

	// Many workers: an upload in flight is cancelled by another's failure,
	// and the error returned is that failure, not the cancellation.
	site = writeSite(t, map[string]string{"a.html": "a", "b.html": "b"})
	store = newMem(nil)
	store.block["a.html"] = true
	store.putErr["b.html"] = errors.New("boom")
	done := make(chan error, 1)
	go func() { _, err := syncDir(context.Background(), store, site, opts(2)); done <- err }()
	select {
	case err := <-done:
		if err == nil || err.Error() != "uploading b.html: boom" {
			t.Errorf("err %v, want the first failure", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("in-flight upload not cancelled")
	}

	// Two failures at once: one of them is returned, never a context error.
	site = writeSite(t, map[string]string{"a.html": "a", "b.html": "b"})
	store = newMem(nil)
	store.putErr["a.html"], store.putErr["b.html"] = errors.New("a failed"), errors.New("b failed")
	store.barrier = newBarrier(2)
	_, err := syncDir(context.Background(), store, site, opts(2))
	if err == nil || errors.Is(err, context.Canceled) || !strings.Contains(err.Error(), " failed") {
		t.Errorf("err %v", err)
	}
}

func TestSyncDirCallerCancellation(t *testing.T) {
	files := map[string]string{}
	for i := range 10 {
		files[fmt.Sprintf("f%d.css", i)] = fmt.Sprint(i)
	}
	store := newMem(map[string]string{"gone.html": "x"})
	store.block["f0.css"] = true
	store.started = make(chan string, 20)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := syncDir(ctx, store, writeSite(t, files), opts(1)); done <- err }()
	<-store.started // f0.css is uploading
	cancel()
	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err %v, want context.Canceled", err)
	}
	if len(store.puts) != 1 || len(store.deletes) != 0 {
		t.Errorf("after cancel: puts %v deletes %v", store.puts, store.deletes)
	}
}

func TestSyncDirLocalFailures(t *testing.T) {
	store := newMem(nil)
	store.listErr = errors.New("denied")
	if _, err := syncDir(context.Background(), store, writeSite(t, map[string]string{"a": "a"}), opts(1)); err == nil || len(store.puts) != 0 {
		t.Errorf("list failure: err %v puts %v", err, store.puts)
	}

	if runtime.GOOS != "windows" {
		site := writeSite(t, map[string]string{"a.html": "a"})
		if err := os.Symlink("/etc/hostname", filepath.Join(site, "link.html")); err != nil {
			t.Fatal(err)
		}
		if _, err := syncDir(context.Background(), newMem(nil), site, opts(1)); err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("symlink: err %v", err)
		}

		site = writeSite(t, map[string]string{"a.html": "a", "b.html": "b"})
		if err := os.Chmod(filepath.Join(site, "b.html"), 0); err != nil {
			t.Fatal(err)
		}
		if os.Getuid() != 0 { // root reads anything
			store := newMem(nil)
			if _, err := syncDir(context.Background(), store, site, opts(1)); err == nil || slices.Contains(store.puts, "b.html") {
				t.Errorf("unreadable file: err %v puts %v", err, store.puts)
			}
		}
	}
}

func TestChangedURLs(t *testing.T) {
	got := changedURLs("https://example.org", []string{"index.html", "a/b/index.html", "style.css", "x/notindex.html", "notes/café/index.html", "assets/my file#1.png"})
	want := []string{
		"https://example.org/index.html", "https://example.org/",
		"https://example.org/a/b/index.html", "https://example.org/a/b/",
		"https://example.org/style.css",
		"https://example.org/x/notindex.html",
		"https://example.org/notes/caf%C3%A9/index.html", "https://example.org/notes/caf%C3%A9/",
		"https://example.org/assets/my%20file%231.png",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
	if got := changedURLs("https://example.org/sub/", []string{"index.html"}); !reflect.DeepEqual(got, []string{"https://example.org/sub/index.html", "https://example.org/sub/"}) {
		t.Errorf("base with path: %v", got)
	}
}

func TestContentType(t *testing.T) {
	for key, want := range map[string]string{"a/index.html": "text/html; charset=utf-8", "X.PNG": "image/png", "s.css": "text/css; charset=utf-8", "noext": "application/octet-stream"} {
		if got := contentType(key); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
}

func TestResolveAll(t *testing.T) {
	t.Setenv("TEST_A", "a")
	got, err := resolveAll(context.Background(), map[string]string{"x": "env:TEST_A", "lit": "literal"})
	if err != nil || got["x"] != "a" || got["lit"] != "literal" {
		t.Errorf("got %v err %v", got, err)
	}
	_, err = resolveAll(context.Background(), map[string]string{"z.empty": "", "a.unset": "env:UNSET_XYZ_123"})
	if err == nil || !strings.HasPrefix(err.Error(), "a.unset: ") || !strings.Contains(err.Error(), "z.empty: is empty") {
		t.Errorf("err %v: want both names, in order", err)
	}
}

// r2Fixture is an R2 config pointed at memStore and a recording purge API.
type r2Fixture struct {
	store  *memStore
	purges *purgeServer
	cfg    *config.Config
	logs   bytes.Buffer
}

func newR2Fixture(t *testing.T, files map[string]string, remote map[string]string) *r2Fixture {
	t.Helper()
	f := &r2Fixture{store: newMem(remote), purges: &purgeServer{}}
	f.purges.start(t)
	site := builtSite(t)
	for k, v := range files {
		if err := os.WriteFile(filepath.Join(site, k), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := newStore
	newStore = func(*config.R2, r2Creds) (objectStore, error) { return f.store, nil }
	t.Cleanup(func() { newStore = old })
	t.Setenv("TEST_R2_KEY", "k")
	t.Setenv("TEST_R2_SECRET", "s3cr3t")
	f.cfg = &config.Config{Output: site, Target: "html", Site: config.Site{BaseURL: "https://example.org/"},
		Deploy: config.Deploy{R2: &config.R2{Bucket: "site", Concurrency: 4, MaxDeleteFraction: 1,
			AccessKeyID: "env:TEST_R2_KEY", SecretAccessKey: "env:TEST_R2_SECRET", Endpoint: "https://s3.example.com",
			Purge: &config.Purge{ZoneID: "zone1", APIToken: "env:TEST_PURGE_TOKEN", BatchSize: 100, URLsPerSecond: 10000, EverythingOver: 500}}}}
	return f
}

func (f *r2Fixture) run() (string, error) {
	return Run(context.Background(), f.cfg, &fakeRunner{}, zerolog.New(&f.logs))
}

func (f *r2Fixture) logLine(msg string) map[string]any {
	for line := range bytes.SplitSeq(bytes.TrimSpace(f.logs.Bytes()), []byte("\n")) {
		var m map[string]any
		if json.Unmarshal(line, &m) == nil && m["message"] == msg {
			return m
		}
	}
	return nil
}

func TestR2DeployLogsAndPurges(t *testing.T) {
	f := newR2Fixture(t, map[string]string{"index.html": "home"}, map[string]string{"old.html": "x"})
	got, err := f.run()
	if err != nil || got != "https://example.org/" {
		t.Fatalf("url %q err %v", got, err)
	}
	if b := f.purges.bodies; len(b) != 1 || !reflect.DeepEqual(b[0]["files"], []any{"https://example.org/index.html", "https://example.org/", "https://example.org/old.html"}) {
		t.Errorf("purge bodies %v", b)
	}
	deployed := f.logLine("Deployed")
	for k, want := range map[string]any{"url": "https://example.org/", "uploaded": 1.0, "deleted": 1.0, "unchanged": 0.0, "purged": 3.0} {
		if deployed[k] != want {
			t.Errorf("Deployed %s = %v, want %v (%v)", k, deployed[k], want, deployed)
		}
	}
	if _, ok := deployed["elapsed_ms"]; !ok {
		t.Error("Deployed has no elapsed_ms")
	}
	if f.logLine("Deploying to R2") == nil || f.logLine("Synced to R2") == nil {
		t.Errorf("missing log lines: %s", f.logs.String())
	}
	for _, s := range []string{"sekrit", "s3cr3t"} {
		if strings.Contains(f.logs.String(), s) {
			t.Errorf("%s in the logs", s)
		}
	}
}

func TestR2DeployNoPurgeWhenNothingChanged(t *testing.T) {
	f := newR2Fixture(t, map[string]string{"a.html": "a"}, map[string]string{"a.html": md5hex([]byte("a"))})
	if _, err := f.run(); err != nil {
		t.Fatal(err)
	}
	if len(f.purges.bodies) != 0 {
		t.Errorf("purged %v", f.purges.bodies)
	}
}

func TestR2DeployPurgesWhatItChangedWhenSyncFails(t *testing.T) {
	// Every upload succeeds, then the delete fails: the uploads (and the
	// attempted delete) are still purged, and the delete error returned.
	f := newR2Fixture(t, map[string]string{"a.html": "a"}, map[string]string{"gone.html": "x"})
	f.store.deleteErr = errors.New("access denied")
	_, err := f.run()
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("err %v", err)
	}
	if b := f.purges.bodies; len(b) != 1 || !reflect.DeepEqual(b[0]["files"], []any{"https://example.org/a.html", "https://example.org/gone.html"}) {
		t.Errorf("purge bodies %v", b)
	}
	if f.logLine("Deployed") != nil {
		t.Error("Deployed logged for a failed deploy")
	}
	if s := f.logLine("Synced to R2"); s == nil || s["uploaded"] != 1.0 || s["error"] == nil {
		t.Errorf("Synced line %v", s)
	}
}

func TestR2DeployPurgesAfterCancellation(t *testing.T) {
	f := newR2Fixture(t, map[string]string{"a.css": "a", "b.html": "b"}, nil)
	f.cfg.Deploy.R2.Concurrency = 1
	f.store.block["b.html"] = true // the page phase hangs until cancelled
	f.store.started = make(chan string, 4)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := Run(ctx, f.cfg, &fakeRunner{}, zerolog.Nop()); done <- err }()
	for k := range f.store.started {
		if k == "b.html" {
			break
		}
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
	if b := f.purges.bodies; len(b) != 1 || !reflect.DeepEqual(b[0]["files"], []any{"https://example.org/a.css"}) {
		t.Errorf("the finished upload was not purged: %v", b)
	}
}

func TestR2DeployPurgeFailureIsAnError(t *testing.T) {
	f := newR2Fixture(t, map[string]string{"a.html": "a"}, nil)
	f.purges.respond = func(_ int, _ map[string]any, w http.ResponseWriter) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":10000,"message":"Authentication error"}]}`))
	}
	_, err := f.run()
	if err == nil || !strings.Contains(err.Error(), "Authentication error") {
		t.Fatalf("err %v", err)
	}
	if s := f.logLine("Synced to R2"); s == nil || s["uploaded"] != 1.0 {
		t.Errorf("sync counts not logged: %v", s)
	}
	if len(f.purges.bodies) != 1 {
		t.Errorf("a permanent failure fell back to purging everything: %v", f.purges.bodies)
	}
}

func TestR2DeployResolvesSecretsFirst(t *testing.T) {
	f := newR2Fixture(t, map[string]string{"a.html": "a"}, nil)
	f.cfg.Deploy.R2.Purge.APIToken = "env:UNSET_PURGE_TOKEN_XYZ"
	if _, err := f.run(); err == nil || !strings.Contains(err.Error(), "deploy.r2.purge.api_token") {
		t.Fatalf("err %v", err)
	}
	if len(f.store.puts) != 0 {
		t.Error("uploaded before finding the purge token missing")
	}
}

func TestBothTargetsRunPagesFirst(t *testing.T) {
	f := newR2Fixture(t, map[string]string{"a.html": "a"}, nil)
	t.Setenv("TEST_CF_TOKEN", "tok")
	f.cfg.Deploy.CloudflarePages = cfgFor(f.cfg.Output).Deploy.CloudflarePages
	r := &fakeRunner{out: "https://abc.proj.pages.dev"}
	got, err := Run(context.Background(), f.cfg, r, zerolog.Nop())
	if err != nil || r.got.Tool != "wrangler" || got != "https://example.org/" {
		t.Errorf("url %q err %v runner %+v", got, err, r.got)
	}
	// A Pages failure stops before R2.
	r.err = errors.New("wrangler failed")
	newStore = func(*config.R2, r2Creds) (objectStore, error) {
		t.Error("R2 ran after Pages failed")
		return newMem(nil), nil
	}
	if _, err := Run(context.Background(), f.cfg, r, zerolog.Nop()); err == nil {
		t.Error("no error")
	}
}

// TestR2Live syncs a small tree to a real bucket and checks a second sync is
// a no-op. Set R2_TEST_BUCKET, R2_TEST_ENDPOINT (or CLOUDFLARE_ACCOUNT_ID),
// R2_ACCESS_KEY_ID and R2_SECRET_ACCESS_KEY to run it. It makes the whole
// bucket match a three-file tree, deleting everything else: use a scratch
// bucket, never the site's.
func TestR2Live(t *testing.T) {
	bucket := os.Getenv("R2_TEST_BUCKET")
	if bucket == "" {
		t.Skip("R2_TEST_BUCKET not set")
	}
	ctx := context.Background()
	rc := &config.R2{Bucket: bucket, Endpoint: os.Getenv("R2_TEST_ENDPOINT"), Concurrency: 8}
	v, err := resolveAll(ctx, map[string]string{"k": "env:R2_ACCESS_KEY_ID", "s": "env:R2_SECRET_ACCESS_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	store, err := newMinioStore(rc, r2Creds{accountID: os.Getenv("CLOUDFLARE_ACCOUNT_ID"), accessKeyID: v["k"], secretAccessKey: v["s"]})
	if err != nil {
		t.Fatal(err)
	}
	site := writeSite(t, map[string]string{"index.html": "live", "a/index.html": "a", "s.css": "c"})
	o := syncOptions{concurrency: 8, maxDeleteFraction: 1, allowMassDelete: true}
	res, err := syncDir(ctx, store, site, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("first sync: %+v", res)
	res, err = syncDir(ctx, store, site, o)
	if err != nil || len(res.uploaded) != 0 || len(res.deleted) != 0 || res.unchanged != 3 {
		t.Errorf("second sync not a no-op: %+v %v", res, err)
	}
	got, err := store.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	keys := slices.Sorted(func(yield func(string) bool) {
		for k := range got {
			if !yield(k) {
				return
			}
		}
	})
	if !reflect.DeepEqual(keys, []string{"a/index.html", "index.html", "s.css"}) {
		t.Errorf("bucket holds %v", keys)
	}
}
