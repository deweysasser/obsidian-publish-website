package deploy

import (
	"context"
	"crypto/md5" // #nosec G501 -- S3 ETags are MD5; used to detect change, not for security
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/secret"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/deweysasser/obsidian-publish-website/internal/timing"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
	"github.com/rs/zerolog"
)

// objectStore is the bucket side of a sync. Keys use forward slashes.
type objectStore interface {
	// List returns every object's key and MD5 (lowercase hex; "" when the
	// store can't tell, e.g. a multipart upload).
	List(ctx context.Context) (map[string]string, error)
	Put(ctx context.Context, key, file, contentType string) error
	Delete(ctx context.Context, keys []string) error
}

// newStore builds the real store; tests replace it.
var newStore = newMinioStore

// skipKeys are build files that must not be published to a bucket: the
// build marker, and Cloudflare Pages control files, which mean nothing on R2
// and would otherwise be served as ordinary files.
var skipKeys = map[string]bool{target.Marker: true, "_headers": true, "_redirects": true}

// massDeleteFloor is how many deletes max_delete_fraction always allows, so
// a small site can still remove a few pages.
const massDeleteFloor = 10

// purgeGrace bounds the purge of what a failed or cancelled sync changed.
const purgeGrace = 30 * time.Second

type syncOptions struct {
	concurrency       int
	force             bool // upload every file, whatever its checksum
	maxDeleteFraction float64
	allowMassDelete   bool
}

type syncResult struct {
	uploaded, deleted []string
	unchanged         int
}

// Names of the secrets r2 resolves.
const (
	refAccount = "deploy.r2.account_id"
	refKey     = "deploy.r2.access_key_id"
	refSecret  = "deploy.r2.secret_access_key"
	refPurge   = "deploy.r2.purge.api_token"
)

// r2 syncs the build to the bucket, then purges what changed. A sync that
// fails part way still purges what it did change before returning the error:
// the next run sees those files as unchanged, so it would never purge them.
func r2(ctx context.Context, cfg *config.Config, log zerolog.Logger) (string, error) {
	rc := cfg.Deploy.R2
	refs := map[string]string{refKey: rc.AccessKeyID, refSecret: rc.SecretAccessKey}
	if rc.Endpoint == "" {
		refs[refAccount] = rc.AccountID
	}
	if rc.Purge != nil {
		refs[refPurge] = rc.Purge.APIToken
	}
	v, err := resolveAll(ctx, refs) // all up front: a bad token fails before any upload
	if err != nil {
		return "", err
	}
	store, err := newStore(rc, r2Creds{accountID: v[refAccount], accessKeyID: v[refKey], secretAccessKey: v[refSecret]})
	if err != nil {
		return "", err
	}
	log.Info().Str("bucket", rc.Bucket).Bool("force_upload", cfg.Deploy.ForceUpload).Msg("Deploying to R2")
	start := time.Now()
	res, syncErr := syncDir(ctx, store, cfg.Output, syncOptions{
		concurrency: rc.Concurrency, force: cfg.Deploy.ForceUpload,
		maxDeleteFraction: rc.MaxDeleteFraction, allowMassDelete: cfg.Deploy.AllowMassDelete,
	})
	log.Info().Err(syncErr).Int("uploaded", len(res.uploaded)).Int("deleted", len(res.deleted)).
		Int("unchanged", res.unchanged).Msg("Synced to R2")
	purged := 0
	var purgeErr error
	if p := rc.Purge; p != nil {
		pctx := ctx
		if syncErr != nil { // purge what did change, even if the run was cancelled
			var cancel context.CancelFunc
			pctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), purgeGrace)
			defer cancel()
		}
		urls := changedURLs(cfg.Site.BaseURL, slices.Concat(res.uploaded, res.deleted))
		purged, purgeErr = purge(pctx, p, v[refPurge], urls, log)
	}
	if err := errors.Join(syncErr, purgeErr); err != nil {
		return "", err
	}
	log.Info().Str("url", cfg.Site.BaseURL).Int("uploaded", len(res.uploaded)).Int("deleted", len(res.deleted)).
		Int("unchanged", res.unchanged).Int("purged", purged).Func(timing.Elapsed(start)).Msg("Deployed")
	return cfg.Site.BaseURL, nil
}

// syncDir makes the bucket match dir. It uploads new and changed files (by
// MD5) in two phases: assets first, then the pages and search entry points
// that refer to them, so a page never goes live before what it links to.
// Then, only if every upload succeeded, it deletes objects dir no longer
// has. It refuses up front, before uploading anything, a build that would
// delete more than o.maxDeleteFraction of the bucket. On error, res still
// says what was uploaded and deleted.
func syncDir(ctx context.Context, store objectStore, dir string, o syncOptions) (res syncResult, err error) {
	defer func() { sort.Strings(res.uploaded) }()
	remote, err := store.List(ctx)
	if err != nil {
		return res, fmt.Errorf("listing bucket: %w", err)
	}
	local, err := localFiles(dir)
	if err != nil {
		return res, err
	}
	have := make(map[string]bool, len(local))
	for _, k := range local {
		have[k] = true
	}
	var gone []string
	for k := range remote {
		if !have[k] {
			gone = append(gone, k)
		}
	}
	sort.Strings(gone)
	if !o.allowMassDelete {
		if len(local) == 0 {
			return res, errors.New("the build has no files to publish; refusing to empty the bucket (--allow-mass-delete overrides)")
		}
		if len(gone) > massDeleteFloor && float64(len(gone)) > o.maxDeleteFraction*float64(len(remote)) {
			return res, fmt.Errorf("the build would delete %d of the bucket's %d objects, more than max_delete_fraction (%g); "+
				"refusing in case the build is broken (--allow-mass-delete overrides)", len(gone), len(remote), o.maxDeleteFraction)
		}
	}

	var assets, pages []string
	for _, k := range local {
		if publishedLast(k) {
			pages = append(pages, k)
		} else {
			assets = append(assets, k)
		}
	}
	for _, phase := range [][]string{assets, pages} {
		if err := uploadAll(ctx, store, dir, phase, remote, o, &res); err != nil {
			return res, err
		}
	}

	if len(gone) > 0 {
		res.deleted = gone // purged even if the delete fails part way: harmless for a key still there
		if err := store.Delete(ctx, gone); err != nil {
			return res, fmt.Errorf("deleting removed files: %w", err)
		}
	}
	return res, nil
}

// publishedLast reports whether key refers to other files, and so goes up
// after them: html pages, and Pagefind's entry points (pagefind/pagefind*.js,
// .json, .css; its index fragments live in subdirectories and are named by
// content hash).
func publishedLast(key string) bool {
	if strings.HasSuffix(key, ".html") {
		return true
	}
	rest, ok := strings.CutPrefix(key, "pagefind/")
	return ok && strings.HasPrefix(rest, "pagefind") && !strings.Contains(rest, "/")
}

// uploadAll uploads the keys that differ from remote, o.concurrency at a
// time. The first error cancels the rest and is returned.
func uploadAll(ctx context.Context, store objectStore, dir string, keys []string, remote map[string]string, o syncOptions, res *syncResult) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var (
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	work := make(chan string)
	fail := func(err error) {
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
	}
	for range max(o.concurrency, 1) {
		wg.Go(func() {
			for key := range work {
				// Cancelled: drain without working. This is also what stops
				// a single worker after its own failure.
				if ctx.Err() != nil {
					continue
				}
				file := filepath.Join(dir, filepath.FromSlash(key))
				if !o.force {
					sum, err := fileMD5(file)
					if err != nil {
						fail(err)
						continue
					}
					if remote[key] == sum {
						mu.Lock()
						res.unchanged++
						mu.Unlock()
						continue
					}
				}
				if err := store.Put(ctx, key, file, contentType(key)); err != nil {
					fail(fmt.Errorf("uploading %s: %w", key, err))
					continue
				}
				mu.Lock()
				res.uploaded = append(res.uploaded, key)
				mu.Unlock()
			}
		})
	}
feed:
	for _, key := range keys {
		select {
		case work <- key:
		case <-ctx.Done():
			break feed
		}
	}
	close(work)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return ctx.Err() // the caller's cancellation
}

// localFiles lists the files under dir to publish, as slash-separated keys.
func localFiles(dir string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s: not a regular file", p)
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if key := filepath.ToSlash(rel); !skipKeys[key] {
			keys = append(keys, key)
		}
		return nil
	})
	return keys, err
}

func fileMD5(file string) (string, error) {
	f, err := os.Open(file) // #nosec G304 -- a path inside our own build output
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New() // #nosec G401 -- change detection, see import
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// contentTypes covers what the html target writes; the system MIME table is
// not used, so a deploy sets the same types from any host.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".json":  "application/json",
	".xml":   "application/xml",
	".txt":   "text/plain; charset=utf-8",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".gif":   "image/gif",
	".webp":  "image/webp",
	".avif":  "image/avif",
	".ico":   "image/x-icon",
	".pdf":   "application/pdf",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".ttf":   "font/ttf",
	".wasm":  "application/wasm",
	".mp4":   "video/mp4",
	".webm":  "video/webm",
	".mp3":   "audio/mpeg",
}

func contentType(key string) string {
	if t, ok := contentTypes[strings.ToLower(path.Ext(key))]; ok {
		return t
	}
	return "application/octet-stream"
}

// changedURLs maps changed keys to the public URLs that serve them: the key
// itself, plus the directory URL for an index.html (the html target links
// every page as dir/). Paths are percent-encoded the way the site links
// them, since Cloudflare caches by the URL the browser asked for.
func changedURLs(base string, keys []string) []string {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	out := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		out = append(out, base+urls.Escape(k))
		switch {
		case k == "index.html":
			out = append(out, base)
		case strings.HasSuffix(k, "/index.html"):
			out = append(out, base+urls.Escape(strings.TrimSuffix(k, "index.html")))
		}
	}
	return out
}

// resolveAll resolves several credential references at once (an op://
// reference costs a round trip each), naming each one that fails or is empty.
func resolveAll(ctx context.Context, refs map[string]string) (map[string]string, error) {
	type result struct {
		name, value string
		err         error
	}
	results := make(chan result, len(refs))
	for name, ref := range refs {
		go func() {
			v, err := secret.Resolve(ctx, ref)
			if err == nil && v == "" {
				err = errors.New("is empty")
			}
			results <- result{name, v, err}
		}()
	}
	out := make(map[string]string, len(refs))
	var errs []error
	for range refs {
		r := <-results
		if r.err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", r.name, r.err))
			continue
		}
		out[r.name] = r.value
	}
	if len(errs) > 0 {
		slices.SortFunc(errs, func(a, b error) int { return strings.Compare(a.Error(), b.Error()) })
		return nil, errors.Join(errs...)
	}
	return out, nil
}
