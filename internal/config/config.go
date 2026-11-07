// Package config loads and validates publish.yaml.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
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

// TagHidden reports whether tag (with or without '#') is in HiddenTags.
func (s Site) TagHidden(tag string) bool {
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

type Deploy struct {
	CloudflarePages *CloudflarePages `yaml:"cloudflare_pages"`
}

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
	f, err := os.Open(file)
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
