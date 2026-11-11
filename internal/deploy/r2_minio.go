package deploy

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// r2Creds are the resolved S3 credentials and, when no endpoint is
// configured, the account ID that names it.
type r2Creds struct {
	accountID, accessKeyID, secretAccessKey string
}

// minioStore is an objectStore on an S3-compatible bucket (R2).
type minioStore struct {
	c        *minio.Client
	bucket   string
	throttle *throttle
}

// tlsRoots replaces the system roots when set; tests point it at an
// httptest TLS server.
var tlsRoots *x509.CertPool

// deleteBatch is the most keys one DeleteObjects request takes.
const deleteBatch = 1000

// newMinioStore connects to the bucket. minio-go's own retries are turned
// off (its backoff caps at a second and ignores Retry-After); withRetry does
// it instead, fed by throttleTransport, with one throttle for all workers.
func newMinioStore(r *config.R2, creds r2Creds) (objectStore, error) {
	endpoint := r.Endpoint
	if endpoint == "" {
		if !config.IsCloudflareID(creds.accountID) {
			return nil, errors.New("deploy.r2.account_id is not a 32-character lowercase hex Cloudflare account ID")
		}
		endpoint = "https://" + creds.accountID + ".r2.cloudflarestorage.com"
	}
	if err := config.ValidateEndpoint(endpoint); err != nil {
		return nil, fmt.Errorf("deploy.r2.endpoint: %w", err)
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	base, err := minio.DefaultTransport(true)
	if err != nil {
		return nil, err
	}
	// One idle connection per worker: minio-go's default keeps 16, so a
	// wider pool would redo the TLS handshake on most requests.
	base.MaxIdleConnsPerHost = r.Concurrency
	base.MaxIdleConns = max(256, r.Concurrency)
	if tlsRoots != nil {
		base.TLSClientConfig.RootCAs = tlsRoots
	}
	c, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4(creds.accessKeyID, creds.secretAccessKey, ""),
		Secure:       true,
		Region:       "auto", // R2's region; also skips a GetBucketLocation call
		BucketLookup: minio.BucketLookupPath,
		Transport:    &throttleTransport{base: base},
		MaxRetries:   1,
	})
	if err != nil {
		return nil, fmt.Errorf("deploy.r2: %w", err)
	}
	return &minioStore{c: c, bucket: r.Bucket, throttle: &throttle{}}, nil
}

// List pages through the bucket, resuming after the last key seen when a
// page has to be retried.
func (s *minioStore) List(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	after := ""
	err := withRetry(ctx, s.throttle, func() error {
		for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Recursive: true, StartAfter: after}) {
			if o.Err != nil {
				return o.Err
			}
			sum := strings.ToLower(o.ETag)  // minio-go strips the quotes
			if strings.Contains(sum, "-") { // multipart: not an MD5
				sum = ""
			}
			out[o.Key] = sum
			after = o.Key
		}
		return nil
	})
	return out, err
}

// Put uploads file in a single PUT, so the object's ETag is its MD5.
func (s *minioStore) Put(ctx context.Context, key, file, contentType string) error {
	return withRetry(ctx, s.throttle, func() error {
		_, err := s.c.FPutObject(ctx, s.bucket, key, file, minio.PutObjectOptions{
			ContentType:      contentType,
			DisableMultipart: true,
			SendContentMd5:   true,
		})
		return err
	})
}

// Delete removes keys with bulk DeleteObjects, one request per 1000 keys,
// all at once.
func (s *minioStore) Delete(ctx context.Context, keys []string) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for i := 0; i < len(keys); i += deleteBatch {
		batch := keys[i:min(i+deleteBatch, len(keys))]
		wg.Go(func() {
			if err := s.deleteBatch(ctx, batch); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

// deleteBatch deletes up to 1000 keys, retrying only the keys that failed.
func (s *minioStore) deleteBatch(ctx context.Context, keys []string) error {
	pending := keys
	return withRetry(ctx, s.throttle, func() error {
		ch := make(chan minio.ObjectInfo, len(pending))
		for _, k := range pending {
			ch <- minio.ObjectInfo{Key: k}
		}
		close(ch)
		var (
			failed []string
			first  error
			whole  bool
		)
		for e := range s.c.RemoveObjects(ctx, s.bucket, ch, minio.RemoveObjectsOptions{}) {
			if e.ObjectName == "" { // the request itself failed: no key named
				whole = true
			}
			failed = append(failed, e.ObjectName)
			if first == nil {
				first = fmt.Errorf("deleting %q: %w", e.ObjectName, e.Err)
			}
		}
		if !whole {
			pending = failed
		} else if first != nil && !errors.As(first, new(minio.ErrorResponse)) {
			// An answer that named no key and wasn't an S3 error (a cut-off
			// or garbled response): retry the whole batch.
			return transientError{first}
		}
		return first
	})
}

// throttleTransport turns 429 and 503 responses into a transientHTTPError,
// so the retry loop can see Retry-After, which minio-go's error type drops.
type throttleTransport struct{ base http.RoundTripper }

func (t *throttleTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusServiceUnavailable {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	return nil, &transientHTTPError{status: resp.StatusCode, after: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
}
