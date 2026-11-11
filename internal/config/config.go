// Package config loads and validates publish.yaml.
package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/deweysasser/obsidian-publish-website/internal/secret"
	"gopkg.in/yaml.v3"
)

type Site struct {
	Title   string `yaml:"title"`
	BaseURL string `yaml:"base_url"`
	Home    string `yaml:"home"`
	// About is the note (relative to root) linked as "About" from every
	// page's footer and the sidebar.
	About string `yaml:"about"`
	// Favicon is an image in the vault (path relative to the vault root);
	// a generic icon is used when empty.
	Favicon string `yaml:"favicon"`
	// HiddenTags are organizing tags left off the site (case-insensitive,
	// '#' optional; hiding "a" also hides "a/b").
	HiddenTags []string `yaml:"hidden_tags"`
	// Tags set to false leaves every tag off the site: no tag pages, tag
	// links or inline tags. Default true.
	Tags *bool `yaml:"tags"`
	// Support configures PayPal payment links: the ```support block, and a
	// link to the support note from every page (html target).
	Support *Support `yaml:"support"`
}

// Support configures the payment links a ```support block renders: one
// PayPal.me link per amount (use the PayPal.me name of the account to be
// paid) and one subscribe link per monthly plan.
type Support struct {
	// Page is the support note (relative to root), linked from every page.
	Page     string    `yaml:"page"`
	PayPalMe string    `yaml:"paypal_me"`
	Amounts  []float64 `yaml:"amounts"`
	Currency string    `yaml:"currency"`
	// Monthly are PayPal Subscriptions plans, one per monthly amount.
	Monthly []MonthlyPlan `yaml:"monthly"`
}

// MonthlyPlan is a PayPal Subscriptions plan offered at a monthly amount.
type MonthlyPlan struct {
	Amount float64 `yaml:"amount"`
	PlanID string  `yaml:"plan_id"`
}

// DefaultSupportAmounts are the amounts offered when none are configured.
var DefaultSupportAmounts = []float64{1, 2, 3, 5, 7, 10}

var (
	paypalMeName = regexp.MustCompile(`^[A-Za-z0-9]{1,20}$`)
	currencyCode = regexp.MustCompile(`^[A-Z]{3}$`)
	paypalPlanID = regexp.MustCompile(`^P-[A-Z0-9]+$`)
)

func validAmount(a float64) bool { return a > 0 && math.Round(a*100) == a*100 }

// TagHidden reports whether tag (with or without '#') is in HiddenTags, or
// whether tags are off altogether.
func (s Site) TagHidden(tag string) bool {
	if s.Tags != nil && !*s.Tags {
		return true
	}
	t := strings.ToLower(strings.TrimPrefix(tag, "#"))
	for _, h := range s.HiddenTags {
		h = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(h), "#"))
		if h != "" && (t == h || strings.HasPrefix(t, h+"/")) {
			return true
		}
	}
	return false
}

type Hugo struct {
	Section string `yaml:"section"`
}

type Jekyll struct {
	Collection string `yaml:"collection"`
}

type HTML struct {
	TemplateDir string `yaml:"template_dir"`
}

// Tools selects how external programs (pagefind, wrangler) run.
type Tools struct {
	// Runner is "docker" (default: a pinned image built from the Dockerfile
	// embedded in this program) or "host" (programs on PATH).
	Runner string `yaml:"runner"`
	// Image replaces the built-in image (docker runner only).
	Image string `yaml:"image"`
}

// CloudflarePages deploys the html output with wrangler.
type CloudflarePages struct {
	Project string `yaml:"project"`
	Branch  string `yaml:"branch"`
	// AccountID and APIToken accept "op://vault/item/field" (1Password),
	// "env:NAME", or (AccountID only) a literal value.
	AccountID string `yaml:"account_id"`
	APIToken  string `yaml:"api_token"`
}

// R2 syncs the html output to a Cloudflare R2 bucket over its S3 API: it
// uploads new and changed files, deletes removed ones, and optionally purges
// their URLs from Cloudflare's cache. Credentials accept "op://vault/item/field"
// or "env:NAME"; AccountID may also be a literal.
type R2 struct {
	AccountID       string `yaml:"account_id"`
	Bucket          string `yaml:"bucket"`
	AccessKeyID     string `yaml:"access_key_id"`
	SecretAccessKey string `yaml:"secret_access_key"`
	// Endpoint overrides https://<account_id>.r2.cloudflarestorage.com.
	Endpoint string `yaml:"endpoint"`
	// Concurrency is how many uploads run at once.
	Concurrency int `yaml:"concurrency"`
	// MaxDeleteFraction refuses a deploy that would delete more than this
	// fraction of the bucket's objects (a guard against publishing a broken,
	// near-empty build). 1 turns the guard off; --allow-mass-delete overrides
	// it for one run.
	MaxDeleteFraction float64 `yaml:"max_delete_fraction"`
	// Purge, when set, purges changed URLs (site.base_url + path) afterwards.
	Purge *Purge `yaml:"purge"`
}

// Purge clears changed URLs from a Cloudflare zone's cache.
type Purge struct {
	ZoneID   string `yaml:"zone_id"`
	APIToken string `yaml:"api_token"`
	// BatchSize is URLs per purge request (Cloudflare Free plan: 100).
	BatchSize int `yaml:"batch_size"`
	// URLsPerSecond paces purge requests (Cloudflare Free plan: 800 per
	// account, as a moving average).
	URLsPerSecond int `yaml:"urls_per_second"`
	// EverythingOver purges the whole zone instead when more URLs changed.
	EverythingOver int `yaml:"everything_over"`
}

type Deploy struct {
	CloudflarePages *CloudflarePages `yaml:"cloudflare_pages"`
	R2              *R2              `yaml:"r2"`

	// ForceUpload re-uploads every file to R2 whatever its checksum, so
	// metadata such as Content-Type is rewritten (set from the command line).
	ForceUpload bool `yaml:"-"`
	// AllowMassDelete lifts R2's max_delete_fraction guard for one run (set
	// from the command line).
	AllowMassDelete bool `yaml:"-"`
}

// Any reports whether a deploy target is configured.
func (d Deploy) Any() bool { return d.CloudflarePages != nil || d.R2 != nil }

type Config struct {
	Vault    string   `yaml:"vault"`
	Root     string   `yaml:"root"`
	Exclude  []string `yaml:"exclude"`
	Target   string   `yaml:"target"`
	Output   string   `yaml:"output"`
	Search   string   `yaml:"search"`
	Dataview string   `yaml:"dataview"`
	// DataviewScope is the set of notes queries can read: "published" (default)
	// or "vault" (every note, including excluded ones — their fields and task
	// text can then appear in query results).
	DataviewScope string `yaml:"dataview_scope"`
	// DataviewSettings override the vault's Dataview plugin settings for the
	// published site only (e.g. defaultDateFormat: "yyyy-MM-dd").
	DataviewSettings map[string]any `yaml:"dataview_settings"`
	Site             Site           `yaml:"site"`
	Hugo             Hugo           `yaml:"hugo"`
	Jekyll           Jekyll         `yaml:"jekyll"`
	HTML             HTML           `yaml:"html"`
	Transforms       []string       `yaml:"transforms"`
	// ExportFrontmatter copies each note's frontmatter under an "obsidian"
	// key in hugo/jekyll exports. Off by default: properties often hold data
	// that is not meant to be published.
	ExportFrontmatter bool   `yaml:"export_frontmatter"`
	Tools             Tools  `yaml:"tools"`
	Deploy            Deploy `yaml:"deploy"`

	Strict bool `yaml:"-"`
}

// RunnerEnv, when set, overrides tools.runner. The published container
// image sets it to "host", since it carries pagefind and wrangler itself.
const RunnerEnv = "OBSIDIAN_PUBLISH_TOOLS_RUNNER"

// Load reads a config file, expands paths relative to the file's directory,
// applies defaults (and $OBSIDIAN_PUBLISH_TOOLS_RUNNER) and validates.
func Load(file string) (*Config, error) {
	f, err := os.Open(file) // #nosec G304 -- the config file the user named
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var c Config
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	abs, err := filepath.Abs(file)
	if err != nil {
		return nil, err
	}
	dir := filepath.Dir(abs)
	c.Vault = expandPath(c.Vault, dir)
	c.HTML.TemplateDir = expandPath(c.HTML.TemplateDir, dir)
	c.ApplyDefaults()
	if v := os.Getenv(RunnerEnv); v != "" {
		c.Tools.Runner = v
	}
	c.Output = expandPath(c.Output, dir)
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return &c, nil
}

// ApplyDefaults fills unset fields.
func (c *Config) ApplyDefaults() {
	if c.Target == "" {
		c.Target = "html"
	}
	if c.Output == "" {
		c.Output = "site"
	}
	if c.Search == "" {
		c.Search = "pagefind"
	}
	if c.Dataview == "" {
		c.Dataview = "node"
	}
	if c.DataviewScope == "" {
		c.DataviewScope = "published"
	}
	if c.Tools.Runner == "" {
		c.Tools.Runner = "docker"
	}
	if cf := c.Deploy.CloudflarePages; cf != nil {
		if cf.Branch == "" {
			cf.Branch = "main"
		}
		if cf.AccountID == "" {
			cf.AccountID = "env:CLOUDFLARE_ACCOUNT_ID"
		}
		if cf.APIToken == "" {
			cf.APIToken = "env:CLOUDFLARE_API_TOKEN"
		}
	}
	if r := c.Deploy.R2; r != nil {
		if r.AccountID == "" && r.Endpoint == "" {
			r.AccountID = "env:CLOUDFLARE_ACCOUNT_ID"
		}
		if r.AccessKeyID == "" {
			r.AccessKeyID = "env:R2_ACCESS_KEY_ID"
		}
		if r.SecretAccessKey == "" {
			r.SecretAccessKey = "env:R2_SECRET_ACCESS_KEY"
		}
		if r.Concurrency == 0 {
			r.Concurrency = 64
		}
		if r.MaxDeleteFraction == 0 {
			r.MaxDeleteFraction = 0.25
		}
		if p := r.Purge; p != nil {
			if p.APIToken == "" {
				p.APIToken = "env:CLOUDFLARE_PURGE_TOKEN"
			}
			if p.BatchSize == 0 {
				p.BatchSize = 100
			}
			if p.URLsPerSecond == 0 {
				p.URLsPerSecond = 800
			}
			if p.EverythingOver == 0 {
				p.EverythingOver = 5000
			}
		}
	}
	if sp := c.Site.Support; sp != nil {
		if len(sp.Amounts) == 0 {
			sp.Amounts = DefaultSupportAmounts
		}
		if sp.Currency == "" {
			sp.Currency = "USD"
		}
	}
	c.Root = strings.Trim(c.Root, "/")
	if c.Hugo.Section == "" {
		c.Hugo.Section = simpleSlug(path.Base(c.Root))
	}
	if c.Jekyll.Collection == "" {
		c.Jekyll.Collection = simpleSlug(path.Base(c.Root))
	}
}

func (c *Config) Validate() error {
	var errs []error
	if c.Vault == "" {
		errs = append(errs, errors.New("vault is required"))
	}
	if c.Root == "" {
		errs = append(errs, errors.New("root is required"))
	} else if strings.HasPrefix(c.Root, "/") || path.Clean(c.Root) != c.Root || strings.HasPrefix(c.Root, "..") {
		errs = append(errs, fmt.Errorf("root %q must be a clean path relative to the vault", c.Root))
	}
	switch c.Target {
	case "html", "hugo", "jekyll":
	default:
		errs = append(errs, fmt.Errorf("target %q must be html, hugo or jekyll", c.Target))
	}
	switch c.Search {
	case "pagefind", "none":
	default:
		errs = append(errs, fmt.Errorf("search %q must be pagefind or none", c.Search))
	}
	for name, v := range map[string]string{"hugo.section": c.Hugo.Section, "jekyll.collection": c.Jekyll.Collection} {
		if v != "" && (strings.ContainsAny(v, `/\`) || v == "." || v == "..") {
			errs = append(errs, fmt.Errorf("%s %q must be a single path segment", name, v))
		}
	}
	switch c.Dataview {
	case "node", "none", "":
	default:
		errs = append(errs, fmt.Errorf("dataview %q must be node or none", c.Dataview))
	}
	switch c.DataviewScope {
	case "published", "vault", "":
	default:
		errs = append(errs, fmt.Errorf("dataview_scope %q must be published or vault", c.DataviewScope))
	}
	switch c.Tools.Runner {
	case "docker", "host", "":
	default:
		errs = append(errs, fmt.Errorf("tools.runner %q must be docker or host", c.Tools.Runner))
	}
	if strings.HasPrefix(c.Tools.Image, "-") { // passed to docker as an argument
		errs = append(errs, fmt.Errorf("tools.image %q must be an image name", c.Tools.Image))
	}
	if cf := c.Deploy.CloudflarePages; cf != nil {
		if cf.Project == "" {
			errs = append(errs, errors.New("deploy.cloudflare_pages.project is required"))
		}
		if !strings.HasPrefix(cf.APIToken, "op://") && !strings.HasPrefix(cf.APIToken, "env:") {
			errs = append(errs, errors.New(`deploy.cloudflare_pages.api_token must be an "op://" or "env:" reference, not the token itself`))
		}
		if c.Target != "html" {
			errs = append(errs, errors.New("deploy.cloudflare_pages needs the html target"))
		}
	}
	if r := c.Deploy.R2; r != nil {
		errs = append(errs, r.validate(c)...)
	}
	notePaths := map[string]string{"site.about": c.Site.About}
	if sp := c.Site.Support; sp != nil {
		notePaths["site.support.page"] = sp.Page
	}
	for name, a := range notePaths {
		if a != "" && (path.IsAbs(a) || path.Clean(a) != a || strings.HasPrefix(a, "..")) {
			errs = append(errs, fmt.Errorf("%s %q must be a clean path relative to root", name, a))
		}
	}
	if sp := c.Site.Support; sp != nil {
		if !paypalMeName.MatchString(sp.PayPalMe) {
			errs = append(errs, fmt.Errorf("site.support.paypal_me %q must be a PayPal.me name (letters and digits)", sp.PayPalMe))
		}
		if !currencyCode.MatchString(sp.Currency) {
			errs = append(errs, fmt.Errorf("site.support.currency %q must be a 3-letter code like USD", sp.Currency))
		}
		for _, a := range sp.Amounts {
			if !validAmount(a) {
				errs = append(errs, fmt.Errorf("site.support.amounts: %v must be positive with at most two decimals", a))
			}
		}
		for _, m := range sp.Monthly {
			if !validAmount(m.Amount) {
				errs = append(errs, fmt.Errorf("site.support.monthly: amount %v must be positive with at most two decimals", m.Amount))
			}
			if !paypalPlanID.MatchString(m.PlanID) {
				errs = append(errs, fmt.Errorf("site.support.monthly: plan_id %q must be a PayPal plan ID like P-1AB23456CD789012E", m.PlanID))
			}
		}
	}
	for _, g := range c.Exclude {
		if !doublestar.ValidatePattern(g) {
			errs = append(errs, fmt.Errorf("exclude pattern %q is invalid", g))
		}
	}
	return errors.Join(errs...)
}

func expandPath(p, base string) string {
	if p == "" {
		return p
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p[1:], "/"))
		}
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(base, p)
	}
	return filepath.Clean(p)
}

func simpleSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

var (
	cloudflareIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)
	bucketRe       = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`)
)

// IsCloudflareID reports whether s looks like a Cloudflare account or zone
// ID: 32 lowercase hex digits.
func IsCloudflareID(s string) bool { return cloudflareIDRe.MatchString(s) }

func (r *R2) validate(c *Config) []error {
	var errs []error
	if !bucketRe.MatchString(r.Bucket) {
		errs = append(errs, errors.New("deploy.r2.bucket must be a bucket name (3-63 lowercase letters, digits, dots and hyphens)"))
	}
	switch {
	case r.Endpoint != "":
		if err := ValidateEndpoint(r.Endpoint); err != nil {
			errs = append(errs, fmt.Errorf("deploy.r2.endpoint: %w", err))
		}
	case r.AccountID == "":
		errs = append(errs, errors.New("deploy.r2 needs account_id or endpoint"))
	case !secret.IsReference(r.AccountID) && !IsCloudflareID(r.AccountID):
		errs = append(errs, errors.New("deploy.r2.account_id must be a 32-character lowercase hex account ID, or an op:// or env: reference"))
	}
	for _, k := range []struct{ name, v string }{{"access_key_id", r.AccessKeyID}, {"secret_access_key", r.SecretAccessKey}} {
		if !secret.IsReference(k.v) {
			errs = append(errs, fmt.Errorf(`deploy.r2.%s must be an "op://" or "env:" reference, not the value itself`, k.name))
		}
	}
	if r.Concurrency < 1 {
		errs = append(errs, errors.New("deploy.r2.concurrency must be at least 1"))
	}
	if r.MaxDeleteFraction <= 0 || r.MaxDeleteFraction > 1 {
		errs = append(errs, errors.New("deploy.r2.max_delete_fraction must be above 0 and at most 1"))
	}
	if c.Target != "html" {
		errs = append(errs, errors.New("deploy.r2 needs the html target"))
	}
	if p := r.Purge; p != nil {
		if !IsCloudflareID(p.ZoneID) {
			errs = append(errs, errors.New("deploy.r2.purge.zone_id must be a 32-character lowercase hex Cloudflare zone ID"))
		}
		if !secret.IsReference(p.APIToken) {
			errs = append(errs, errors.New(`deploy.r2.purge.api_token must be an "op://" or "env:" reference, not the token itself`))
		}
		if p.BatchSize < 1 || p.EverythingOver < 1 || p.URLsPerSecond < 1 {
			errs = append(errs, errors.New("deploy.r2.purge.batch_size, urls_per_second and everything_over must be at least 1"))
		}
		if !strings.HasPrefix(c.Site.BaseURL, "https://") && !strings.HasPrefix(c.Site.BaseURL, "http://") {
			errs = append(errs, errors.New("deploy.r2.purge needs an absolute site.base_url to name the URLs to purge"))
		}
	}
	return errs
}

// ValidateEndpoint accepts only an https URL naming a host: no path, query,
// fragment or credentials.
func ValidateEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	// The value is not echoed: it could carry credentials (user:pass@).
	switch {
	case err != nil:
		return errors.New("not a URL")
	case u.Scheme != "https":
		return errors.New("must be an https:// URL")
	case u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "":
		return errors.New("must be a scheme and host only, with no path, query or credentials")
	}
	return nil
}
