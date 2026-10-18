// Package config loads and validates publish.yaml.
package config

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/bmatcuk/doublestar/v4"
	"gopkg.in/yaml.v3"
)

type Site struct {
	Title   string `yaml:"title"`
	BaseURL string `yaml:"base_url"`
	Home    string `yaml:"home"`
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

type Config struct {
	Vault      string   `yaml:"vault"`
	Root       string   `yaml:"root"`
	Exclude    []string `yaml:"exclude"`
	Target     string   `yaml:"target"`
	Output     string   `yaml:"output"`
	Search     string   `yaml:"search"`
	Site       Site     `yaml:"site"`
	Hugo       Hugo     `yaml:"hugo"`
	Jekyll     Jekyll   `yaml:"jekyll"`
	HTML       HTML     `yaml:"html"`
	Transforms []string `yaml:"transforms"`

	Strict bool `yaml:"-"`
}

// Load reads a config file, expands paths relative to the file's directory,
// applies defaults and validates.
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
