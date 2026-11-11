package deploy

import (
	"context"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
)

// fakeS3 speaks enough of the S3 API (path-style ListObjectsV2, PUT,
// DeleteObjects) to drive minioStore, and records every request.
type fakeS3 struct {
	mu       sync.Mutex
	bucket   string
	objects  map[string]fakeObject
	requests []fakeRequest
	pageSize int
	// fail[method] holds statuses to answer with, one per request, before
	// behaving normally; 0 means answer this one normally.
	fail map[string][]int
	// keyErrors[key] is how many DeleteObjects calls report that key failed
	// with InternalError before it succeeds.
	keyErrors map[string]int
	// garbleDelete answers the next DeleteObjects call with unparseable XML.
	garbleDelete bool
}

type fakeObject struct {
	etag, contentType string
	body              []byte
}

type fakeRequest struct {
	method, key string
	query       url.Values
	header      http.Header
	deleteKeys  []string
}

func newFakeS3(t *testing.T, bucket string) (*fakeS3, *config.R2) {
	t.Helper()
	f := &fakeS3{bucket: bucket, objects: map[string]fakeObject{}, pageSize: 1000, fail: map[string][]int{}, keyErrors: map[string]int{}}
	srv := httptest.NewTLSServer(f)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	tlsRoots = pool
	t.Cleanup(func() { tlsRoots = nil })
	return f, &config.R2{Bucket: bucket, Endpoint: srv.URL, Concurrency: 8}
}

func (f *fakeS3) store(t *testing.T, rc *config.R2) *minioStore {
	t.Helper()
	s, err := newMinioStore(rc, r2Creds{accessKeyID: "AKID", secretAccessKey: "SECRET"})
	if err != nil {
		t.Fatal(err)
	}
	return s.(*minioStore)
}

func (f *fakeS3) put(key, body string) {
	f.objects[key] = fakeObject{etag: md5hex([]byte(body)), body: []byte(body)}
}

func (f *fakeS3) log(method string) []fakeRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeRequest
	for _, r := range f.requests {
		if r.method == method {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeS3) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"+f.bucket), "/")
	req := fakeRequest{method: r.Method, key: key, query: r.URL.Query(), header: r.Header.Clone()}
	body, _ := io.ReadAll(r.Body)
	if r.Method == http.MethodPost {
		var d struct {
			Objects []struct{ Key string } `xml:"Object"`
		}
		_ = xml.Unmarshal(body, &d)
		for _, o := range d.Objects {
			req.deleteKeys = append(req.deleteKeys, o.Key)
		}
	}
	f.requests = append(f.requests, req)
	if q := f.fail[r.Method]; len(q) > 0 {
		f.fail[r.Method] = q[1:]
		if q[0] != 0 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(q[0])
			return
		}
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Query().Get("list-type") == "2":
		f.list(w, r)
	case r.Method == http.MethodPut:
		f.objects[key] = fakeObject{etag: md5hex(body), contentType: r.Header.Get("Content-Type"), body: body}
		w.Header().Set("ETag", `"`+md5hex(body)+`"`)
	case r.Method == http.MethodPost && r.URL.Query().Has("delete"):
		f.deleteObjects(w, req.deleteKeys)
	default:
		w.WriteHeader(http.StatusNotImplemented)
	}
}

func (f *fakeS3) list(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after := q.Get("start-after")
	if tok := q.Get("continuation-token"); tok != "" {
		after = tok
	}
	keys := make([]string, 0, len(f.objects))
	for k := range f.objects {
		if k > after && strings.HasPrefix(k, q.Get("prefix")) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	type content struct {
		Key          string
		ETag         string
		Size         int
		LastModified string
	}
	res := struct {
		XMLName               xml.Name `xml:"ListBucketResult"`
		Name                  string
		KeyCount              int
		MaxKeys               int
		IsTruncated           bool
		NextContinuationToken string    `xml:",omitempty"`
		Contents              []content `xml:"Contents"`
	}{Name: f.bucket, MaxKeys: f.pageSize}
	if len(keys) > f.pageSize {
		keys = keys[:f.pageSize]
		res.IsTruncated = true
		res.NextContinuationToken = keys[len(keys)-1]
	}
	for _, k := range keys {
		o := f.objects[k]
		res.Contents = append(res.Contents, content{Key: k, ETag: `"` + o.etag + `"`, Size: len(o.body), LastModified: "2026-01-01T00:00:00.000Z"})
	}
	res.KeyCount = len(res.Contents)
	w.Header().Set("Content-Type", "application/xml")
	_ = xml.NewEncoder(w).Encode(res)
}

func (f *fakeS3) deleteObjects(w http.ResponseWriter, keys []string) {
	if f.garbleDelete {
		f.garbleDelete = false
		_, _ = w.Write([]byte("<DeleteResult><Deleted>"))
		return
	}
	var b strings.Builder
	b.WriteString("<DeleteResult>")
	for _, k := range keys {
		if f.keyErrors[k] > 0 {
			f.keyErrors[k]--
			fmt.Fprintf(&b, "<Error><Key>%s</Key><Code>InternalError</Code><Message>try again</Message></Error>", k)
			continue
		}
		delete(f.objects, k)
		fmt.Fprintf(&b, "<Deleted><Key>%s</Key></Deleted>", k)
	}
	b.WriteString("</DeleteResult>")
	_, _ = w.Write([]byte(b.String()))
}

func TestMinioStoreSyncEndToEnd(t *testing.T) {
	f, rc := newFakeS3(t, "site")
	f.put("gone.html", "old")
	f.objects["multi.css"] = fakeObject{etag: "0123abcd-3", body: []byte("c")} // a multipart ETag
	f.put("same.css", "same")
	store := f.store(t, rc)
	site := writeSite(t, map[string]string{"index.html": "home", "multi.css": "c", "same.css": "same", "img/a b.png": "png"})
	o := syncOptions{concurrency: 4, maxDeleteFraction: 1}

	res, err := syncDir(context.Background(), store, site, o)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(res.uploaded) != "[img/a b.png index.html multi.css]" || fmt.Sprint(res.deleted) != "[gone.html]" || res.unchanged != 1 {
		t.Errorf("res %+v", res)
	}
	for _, r := range f.log(http.MethodPut) {
		if r.query.Has("uploadId") || r.query.Has("uploads") {
			t.Errorf("%s: multipart upload", r.key)
		}
		if r.header.Get("Content-Md5") == "" {
			t.Errorf("%s: no Content-MD5", r.key)
		}
	}
	if got := f.objects["index.html"].contentType; got != "text/html; charset=utf-8" {
		t.Errorf("index.html content type %q", got)
	}
	if got := f.objects["img/a b.png"].contentType; got != "image/png" {
		t.Errorf("png content type %q", got)
	}
	if len(f.log(http.MethodHead)) != 0 {
		t.Error("HEAD requests sent")
	}

	// The second run of the same build is a no-op: the ETags the store set
	// are the files' MD5s.
	f.requests = nil
	res, err = syncDir(context.Background(), store, site, o)
	if err != nil || len(res.uploaded) != 0 || len(res.deleted) != 0 || res.unchanged != 4 {
		t.Errorf("second run: res %+v err %v", res, err)
	}
	if n := len(f.log(http.MethodPut)) + len(f.log(http.MethodPost)); n != 0 {
		t.Errorf("second run sent %d writes", n)
	}
}

func TestMinioStoreListPagesAndResumes(t *testing.T) {
	fastRetries(t)
	f, rc := newFakeS3(t, "site")
	f.pageSize = 2
	for i := range 7 {
		f.put(fmt.Sprintf("k%d", i), "x")
	}
	store := f.store(t, rc)
	got, err := store.List(context.Background())
	if err != nil || len(got) != 7 || len(f.log(http.MethodGet)) != 4 {
		t.Fatalf("got %d keys in %d requests, err %v", len(got), len(f.log(http.MethodGet)), err)
	}

	// A 503 on the second page: the retry starts after the last key seen,
	// rather than from the top.
	f.requests = nil
	f.failSeq(http.MethodGet, 0, http.StatusServiceUnavailable)
	got, err = store.List(context.Background())
	if err != nil || len(got) != 7 {
		t.Fatalf("after a 503: got %d keys err %v", len(got), err)
	}
	gets := f.log(http.MethodGet)
	if len(gets) != 5 || gets[2].query.Get("start-after") != "k1" || gets[2].query.Get("continuation-token") != "" {
		var q []string
		for _, r := range gets {
			q = append(q, fmt.Sprint(r.query))
		}
		t.Errorf("%d list requests: %v", len(gets), q)
	}
}

// failSeq queues answers for the next requests of method: 0 means behave
// normally, anything else is that HTTP status.
func (f *fakeS3) failSeq(method string, statuses ...int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[method] = statuses
}

func TestMinioStoreThrottledUploadRetries(t *testing.T) {
	fastRetries(t)
	f, rc := newFakeS3(t, "site")
	store := f.store(t, rc)
	f.failSeq(http.MethodPut, http.StatusTooManyRequests, http.StatusServiceUnavailable)
	site := writeSite(t, map[string]string{"a.html": "a"})
	if err := store.Put(context.Background(), "a.html", site+"/a.html", "text/html; charset=utf-8"); err != nil {
		t.Fatal(err)
	}
	if n := len(f.log(http.MethodPut)); n != 3 {
		t.Errorf("%d PUTs, want 3", n)
	}
	// A 403 is not retried.
	f.requests = nil
	f.failSeq(http.MethodPut, http.StatusForbidden)
	if err := store.Put(context.Background(), "a.html", site+"/a.html", "text/html"); err == nil || len(f.log(http.MethodPut)) != 1 {
		t.Errorf("403: err %v, %d PUTs", err, len(f.log(http.MethodPut)))
	}
}

func TestMinioStoreDelete(t *testing.T) {
	fastRetries(t)
	f, rc := newFakeS3(t, "site")
	var keys []string
	for i := range 1001 {
		k := fmt.Sprintf("k%04d", i)
		f.put(k, "x")
		keys = append(keys, k)
	}
	f.keyErrors["k0005"] = 1 // fails once, then deletes
	store := f.store(t, rc)
	if err := store.Delete(context.Background(), keys); err != nil {
		t.Fatal(err)
	}
	if len(f.objects) != 0 {
		t.Errorf("%d objects left", len(f.objects))
	}
	var sizes []int
	for _, r := range f.log(http.MethodPost) {
		sizes = append(sizes, len(r.deleteKeys))
	}
	sort.Ints(sizes)
	if fmt.Sprint(sizes) != "[1 1 1000]" { // two batches, then only the failed key again
		t.Errorf("delete request sizes %v", sizes)
	}

	// An unreadable answer names no keys: the whole batch is retried.
	f.requests = nil
	f.put("a", "x")
	f.put("b", "x")
	f.garbleDelete = true
	if err := store.Delete(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if posts := f.log(http.MethodPost); len(posts) != 2 || len(posts[1].deleteKeys) != 2 || len(f.objects) != 0 {
		t.Errorf("after a garbled answer: %d requests, %d objects left", len(posts), len(f.objects))
	}
}

func TestNewMinioStoreValidates(t *testing.T) {
	base := config.R2{Bucket: "site", Concurrency: 4}
	creds := r2Creds{accessKeyID: "k", secretAccessKey: "s"}
	for name, mod := range map[string]func(*config.R2, *r2Creds){
		"bad account":  func(_ *config.R2, c *r2Creds) { c.accountID = "not/an.id" },
		"upper case":   func(_ *config.R2, c *r2Creds) { c.accountID = "0123456789ABCDEF0123456789abcdef" },
		"path":         func(r *config.R2, _ *r2Creds) { r.Endpoint = "https://host/path" },
		"http":         func(r *config.R2, _ *r2Creds) { r.Endpoint = "http://host" },
		"credentials":  func(r *config.R2, _ *r2Creds) { r.Endpoint = "https://user:pw@host" },
		"query string": func(r *config.R2, _ *r2Creds) { r.Endpoint = "https://host?x=1" },
	} {
		r, c := base, creds
		mod(&r, &c)
		_, err := newMinioStore(&r, c)
		if err == nil {
			t.Errorf("%s: accepted", name)
		} else if strings.Contains(err.Error(), "not/an.id") || strings.Contains(err.Error(), "pw") {
			t.Errorf("%s: error echoes the value: %v", name, err)
		}
	}
	creds.accountID = "0123456789abcdef0123456789abcdef"
	if _, err := newMinioStore(&base, creds); err != nil {
		t.Errorf("good config: %v", err)
	}
}
