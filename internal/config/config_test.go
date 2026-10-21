package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "publish.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadDefaultsAndTilde(t *testing.T) {
	p := writeConfig(t, "vault: ~/v\nroot: Town Notes\nexclude: [People/**]\nsite: {title: T, base_url: https://x.io/c/}\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if c.Vault != filepath.Join(home, "v") {
		t.Errorf("vault %q", c.Vault)
	}
	if c.Target != "html" || c.Search != "pagefind" || c.Dataview != "node" || c.DataviewScope != "published" {
		t.Errorf("defaults %+v", c)
	}
	if c.Output != filepath.Join(filepath.Dir(p), "site") {
		t.Errorf("output %q", c.Output)
	}
	if c.Hugo.Section != "town-notes" || c.Jekyll.Collection != "town-notes" {
		t.Errorf("section %q collection %q", c.Hugo.Section, c.Jekyll.Collection)
	}
	if c.Site.BaseURL != "https://x.io/c/" || len(c.Exclude) != 1 {
		t.Errorf("site %+v exclude %v", c.Site, c.Exclude)
	}
}

func TestLoadRelativeVault(t *testing.T) {
	p := writeConfig(t, "vault: vault\nroot: R\n")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.Vault != filepath.Join(filepath.Dir(p), "vault") {
		t.Errorf("vault %q", c.Vault)
	}
}

func TestLoadRejectsUnknownKey(t *testing.T) {
	p := writeConfig(t, "vault: /v\nroot: R\nexlcude: [x]\n")
	_, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "exlcude") {
		t.Fatalf("want unknown field error, got %v", err)
	}
}

func TestValidate(t *testing.T) {
	ok := func() *Config {
		return &Config{Vault: "/v", Root: "R", Target: "html", Search: "pagefind", Output: "/o"}
	}
	cases := []struct {
		name string
		mod  func(*Config)
		want string
	}{
		{"missing vault", func(c *Config) { c.Vault = "" }, "vault"},
		{"missing root", func(c *Config) { c.Root = "" }, "root"},
		{"bad target", func(c *Config) { c.Target = "pdf" }, "target"},
		{"bad search", func(c *Config) { c.Search = "lunr" }, "search"},
		{"bad dataview", func(c *Config) { c.Dataview = "go" }, "dataview"},
		{"bad scope", func(c *Config) { c.DataviewScope = "all" }, "dataview_scope"},
		{"absolute root", func(c *Config) { c.Root = "/x" }, "root"},
		{"dotdot root", func(c *Config) { c.Root = "a/../../b" }, "root"},
		{"bad glob", func(c *Config) { c.Exclude = []string{"[a"} }, "exclude"},
	}
	if err := ok().Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := ok()
			tc.mod(c)
			err := c.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidateSectionAndCollection(t *testing.T) {
	for _, bad := range []string{"../x", "a/b", "", "."} {
		c := &Config{Vault: "/v", Root: "R", Target: "hugo", Search: "none", Hugo: Hugo{Section: bad}, Jekyll: Jekyll{Collection: "ok"}}
		if bad == "" {
			continue
		}
		if err := c.Validate(); err == nil {
			t.Errorf("section %q accepted", bad)
		}
		c.Hugo.Section, c.Jekyll.Collection = "ok", bad
		if err := c.Validate(); err == nil {
			t.Errorf("collection %q accepted", bad)
		}
	}
}

func TestToolsAndDeployDefaults(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\ndeploy:\n  cloudflare_pages:\n    project: p\n"))
	if err != nil {
		t.Fatal(err)
	}
	cf := c.Deploy.CloudflarePages
	if c.Tools.Runner != "docker" || cf == nil || cf.Branch != "main" || cf.APIToken != "env:CLOUDFLARE_API_TOKEN" || cf.AccountID != "env:CLOUDFLARE_ACCOUNT_ID" {
		t.Errorf("tools %+v deploy %+v", c.Tools, cf)
	}
	c, _ = Load(writeConfig(t, "vault: /v\nroot: R\n"))
	if c.Deploy.CloudflarePages != nil {
		t.Error("no deploy section should mean no deploy")
	}
}

func TestValidateToolsAndDeploy(t *testing.T) {
	for body, want := range map[string]string{
		"tools: {runner: podman}\n":                                                        "tools.runner",
		"deploy: {cloudflare_pages: {}}\n":                                                 "deploy.cloudflare_pages.project",
		"deploy: {cloudflare_pages: {project: p, api_token: abc123}}\n":                    "api_token must be",
		"target: hugo\ndeploy: {cloudflare_pages: {project: p}}\n":                         "html target",
		"deploy: {cloudflare_pages: {project: p, account_id: op://V/I/f}}\nsearch: none\n": "",
	} {
		_, err := Load(writeConfig(t, "vault: /v\nroot: R\n"+body))
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%q: err %v, want %q", body, err, want)
		}
	}
}
