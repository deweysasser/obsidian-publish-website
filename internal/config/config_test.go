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
	t.Setenv(RunnerEnv, "")
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

func TestR2Defaults(t *testing.T) {
	t.Setenv(RunnerEnv, "")
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {base_url: https://example.org/}\ndeploy:\n  r2:\n    bucket: site\n    purge: {zone_id: 0123456789abcdef0123456789abcdef}\n"))
	if err != nil {
		t.Fatal(err)
	}
	r := c.Deploy.R2
	if r == nil || r.AccountID != "env:CLOUDFLARE_ACCOUNT_ID" || r.AccessKeyID != "env:R2_ACCESS_KEY_ID" ||
		r.SecretAccessKey != "env:R2_SECRET_ACCESS_KEY" || r.Concurrency != 64 || r.MaxDeleteFraction != 0.25 || r.Endpoint != "" {
		t.Errorf("r2 %+v", r)
	}
	if p := r.Purge; p.APIToken != "env:CLOUDFLARE_PURGE_TOKEN" || p.BatchSize != 100 || p.URLsPerSecond != 800 || p.EverythingOver != 5000 {
		t.Errorf("purge %+v", p)
	}
	if !c.Deploy.Any() || c.Deploy.CloudflarePages != nil || c.Deploy.ForceUpload || c.Deploy.AllowMassDelete {
		t.Errorf("deploy %+v", c.Deploy)
	}
	// An explicit endpoint needs no account ID.
	c, err = Load(writeConfig(t, "vault: /v\nroot: R\ndeploy: {r2: {bucket: site, endpoint: \"https://s3.example.com\"}}\n"))
	if err != nil || c.Deploy.R2.AccountID != "" {
		t.Errorf("endpoint config: %+v %v", c.Deploy.R2, err)
	}
	// The command-line-only switches can't be set from the file.
	c, _ = Load(writeConfig(t, "vault: /v\nroot: R\ndeploy: {r2: {bucket: site}, force_upload: true, allow_mass_delete: true}\n"))
	if c != nil && (c.Deploy.ForceUpload || c.Deploy.AllowMassDelete) {
		t.Error("force_upload / allow_mass_delete read from the config file")
	}
	c, _ = Load(writeConfig(t, "vault: /v\nroot: R\n"))
	if c.Deploy.Any() {
		t.Error("no deploy section should mean no deploy")
	}
}

func TestValidateR2(t *testing.T) {
	t.Setenv(RunnerEnv, "")
	const id = "0123456789abcdef0123456789abcdef"
	const site = "site: {base_url: https://x/}\n"
	for body, want := range map[string]string{
		"deploy: {r2: {}}\n":                                                                                                    "deploy.r2.bucket",
		"deploy: {r2: {bucket: b}}\n":                                                                                           "deploy.r2.bucket",
		"deploy: {r2: {bucket: My_Bucket}}\n":                                                                                   "deploy.r2.bucket",
		"deploy: {r2: {bucket: site, access_key_id: AKIA123}}\n":                                                                "access_key_id must be",
		"deploy: {r2: {bucket: site, secret_access_key: s3cr3t}}\n":                                                             "secret_access_key must be",
		"deploy: {r2: {bucket: site, account_id: nothex}}\n":                                                                    "account_id must be",
		"deploy: {r2: {bucket: site, account_id: 0123456789ABCDEF0123456789abcdef}}\n":                                          "account_id must be",
		"deploy: {r2: {bucket: site, endpoint: \"http://x\"}}\n":                                                                "https://",
		"deploy: {r2: {bucket: site, endpoint: \"https://x/path\"}}\n":                                                          "scheme and host only",
		"deploy: {r2: {bucket: site, endpoint: \"https://u:s3cr3t@x\"}}\n":                                                      "scheme and host only",
		"deploy: {r2: {bucket: site, concurrency: -1}}\n":                                                                       "concurrency",
		"deploy: {r2: {bucket: site, max_delete_fraction: 1.5}}\n":                                                              "max_delete_fraction",
		"deploy: {r2: {bucket: site, max_delete_fraction: -0.1}}\n":                                                             "max_delete_fraction",
		"target: hugo\ndeploy: {r2: {bucket: site}}\n":                                                                          "html target",
		"deploy: {r2: {bucket: site, purge: {zone_id: z}}}\n" + site:                                                            "zone_id",
		"deploy: {r2: {bucket: site, purge: {zone_id: " + id + ", api_token: abc}}}\n" + site:                                   "api_token must be",
		"deploy: {r2: {bucket: site, purge: {zone_id: " + id + ", batch_size: -1}}}\n" + site:                                   "batch_size",
		"deploy: {r2: {bucket: site, purge: {zone_id: " + id + ", urls_per_second: -1}}}\n" + site:                              "urls_per_second",
		"deploy: {r2: {bucket: site, purge: {zone_id: " + id + "}}}\nsite: {base_url: /}\n":                                     "absolute site.base_url",
		"deploy: {r2: {bucket: site, account_id: " + id + ", access_key_id: op://V/I/u}}\nsearch: none\n":                       "",
		"deploy: {r2: {bucket: my.site-1, max_delete_fraction: 1}, cloudflare_pages: {project: p}}\n" + site + "search: none\n": "",
	} {
		_, err := Load(writeConfig(t, "vault: /v\nroot: R\n"+body))
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want)) {
			t.Errorf("%q: err %v, want %q", body, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "s3cr3t") {
			t.Errorf("%q: error echoes the secret: %v", body, err)
		}
	}
	// Neither account_id nor endpoint: ApplyDefaults fills account_id, so
	// build the config directly.
	c := &Config{Target: "html", Deploy: Deploy{R2: &R2{Bucket: "site", AccessKeyID: "env:A", SecretAccessKey: "env:B", Concurrency: 1, MaxDeleteFraction: 0.5}}}
	if errs := c.Deploy.R2.validate(c); len(errs) != 1 || !strings.Contains(errs[0].Error(), "account_id or endpoint") {
		t.Errorf("errs %v", errs)
	}
	// Errors come out in a fixed order.
	c.Deploy.R2.AccessKeyID, c.Deploy.R2.SecretAccessKey = "x", "y"
	errs := c.Deploy.R2.validate(c)
	if len(errs) != 3 || !strings.Contains(errs[1].Error(), "access_key_id") || !strings.Contains(errs[2].Error(), "secret_access_key") {
		t.Errorf("errs %v", errs)
	}
}

func TestRunnerEnvOverridesConfig(t *testing.T) {
	t.Setenv(RunnerEnv, "host")
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\ntools: {runner: docker}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Tools.Runner != "host" {
		t.Errorf("runner %q, want host from $%s", c.Tools.Runner, RunnerEnv)
	}
	t.Setenv(RunnerEnv, "podman")
	if _, err := Load(writeConfig(t, "vault: /v\nroot: R\n")); err == nil || !strings.Contains(err.Error(), "tools.runner") {
		t.Errorf("bad $%s value: err %v", RunnerEnv, err)
	}
}

func TestValidateToolsAndDeploy(t *testing.T) {
	t.Setenv(RunnerEnv, "")
	for body, want := range map[string]string{
		"tools: {runner: podman}\n":                                                        "tools.runner",
		"tools: {image: --privileged}\n":                                                   "tools.image",
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

func TestSupportDefaultsAndValidation(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {support: {paypal_me: ExampleName}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	sp := c.Site.Support
	if len(sp.Monthly) != 0 || len(sp.Amounts) != 6 || sp.Amounts[5] != 10 || sp.Currency != "USD" {
		t.Errorf("support defaults %+v", sp)
	}
	for body, want := range map[string]string{
		"{support: {}}":                               "paypal_me",
		"{support: {paypal_me: a/b}}":                 "paypal_me",
		"{support: {paypal_me: a, currency: usd}}":    "currency",
		"{support: {paypal_me: a, amounts: [0]}}":     "amounts",
		"{support: {paypal_me: a, amounts: [1.005]}}": "amounts",
		"{support: {paypal_me: a, paypal_client_id: \"bad id!\", monthly: [{amount: 2, plan_id: P-2AB}]}}": "paypal_client_id",
		"{support: {paypal_me: a, paypal_client_id: AbC-123_xyzAbC-123_xyz}}":                              "needs site.support.monthly",
	} {
		if _, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: "+body+"\n")); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", body, err, want)
		}
	}
}

func TestSupportMonthly(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {support: {paypal_me: a, monthly: [{amount: 5, plan_id: P-1AB23456CD789012E}]}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m := c.Site.Support.Monthly; len(m) != 1 || m[0].Amount != 5 || m[0].PlanID != "P-1AB23456CD789012E" {
		t.Errorf("monthly %+v", m)
	}
}

func TestSuggestDefaultsAndValidation(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {suggest: {endpoint: /api/suggest}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k := c.Site.Suggest.Kinds; len(k) != 1 || k[0].ID != "correction" || k[0].Link != "Suggest a change" ||
		k[0].Title != "Suggest a change" || k[0].Message != "required" || k[0].MessageLabel != "Message" {
		t.Errorf("default kinds %+v", k)
	}
	c, err = Load(writeConfig(t, "vault: /v\nroot: R\nsite: {suggest: {endpoint: /api/suggest, kinds: [{id: other, link: L, fields: [{name: f, label: F}]}]}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if k := c.Site.Suggest.Kinds; len(k) != 1 || k[0].ID != "other" || k[0].Fields[0].MaxLength != 200 || k[0].Title != "L" {
		t.Errorf("given kinds replace the default, with defaults filled: %+v", k)
	}

	for body, want := range map[string]string{
		"{endpoint: api/suggest}":                                     "site.suggest.endpoint",
		"{endpoint: \"https://evil.example/x\"}":                      "site.suggest.endpoint",
		"{endpoint: \"//evil.example/x\"}":                            "site.suggest.endpoint",
		"{endpoint: \"/api/suggest?x=1\"}":                            "site.suggest.endpoint",
		"{}":                                                          "site.suggest.endpoint",
		"{endpoint: /s, kinds: [{id: Bad_ID, link: L}]}":              "kinds[0].id",
		"{endpoint: /s, kinds: [{id: a, link: L}, {id: a, link: M}]}": "used twice",
		"{endpoint: /s, kinds: [{id: a}]}":                            "kinds[0].link",
		"{endpoint: /s, kinds: [{id: a, link: L, message: maybe}]}":   "kinds[0].message",
		"{endpoint: /s, kinds: [{id: a, link: L, fields: [{name: page, label: P}]}]}":                   "built into every kind",
		"{endpoint: /s, kinds: [{id: a, link: L, fields: [{name: contact, label: P}]}]}":                "built into every kind",
		"{endpoint: /s, kinds: [{id: a, link: L, fields: [{name: 9x, label: P}]}]}":                     "fields[0].name",
		"{endpoint: /s, kinds: [{id: a, link: L, fields: [{name: x, label: P}, {name: x, label: Q}]}]}": "used twice",
		"{endpoint: /s, kinds: [{id: a, link: L, fields: [{name: x}]}]}":                                "fields[0].label",
		"{endpoint: /s, kinds: [{id: a, link: L, fields: [{name: x, label: X, maxlength: 5001}]}]}":     "maxlength",
	} {
		if _, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {suggest: "+body+"}\n")); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", body, err, want)
		}
	}
	if _, err := Load(writeConfig(t, "vault: /v\nroot: R\ntarget: hugo\nsite: {suggest: {endpoint: /s}}\n")); err == nil || !strings.Contains(err.Error(), "html target") {
		t.Errorf("hugo target: %v", err)
	}
}

func TestMapsPlaceTag(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite:\n  maps:\n    place_tag: municipality\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Site.Maps == nil || c.Site.Maps.PlaceTag != "municipality" {
		t.Errorf("maps %+v", c.Site.Maps)
	}
	for tag, ok := range map[string]bool{"municipality": true, "#place/town": true, "": true, "two words": false, "a,b": false, "#": false, "x/": false, "/x": false} {
		c := &Config{Vault: "/v", Root: "R", Target: "html", Search: "none", Site: Site{Maps: &Maps{PlaceTag: tag}}}
		if err := c.Validate(); (err == nil) != ok {
			t.Errorf("place_tag %q: err %v", tag, err)
		}
	}
}

func TestSecurityTxtDefaultsAndValidation(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {base_url: \"https://x.io/\", security_txt: {contact: [\"https://x.io/suggest/\", \"mailto:a@x.io\", \"tel:+1-555-0100\"]}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if st := c.Site.SecurityTxt; st.ExpiresDays != DefaultSecurityTxtDays || len(st.Contact) != 3 {
		t.Errorf("defaults %+v", st)
	}
	for body, want := range map[string]string{
		"{}":                                                      "needs at least one",
		"{contact: [\"/suggest/\"]}":                              "must start with https:",
		"{contact: [\"http://x.io/\"]}":                           "must start with https:",
		"{contact: [\"https:///x\"]}":                             "not a complete https: URI",
		"{contact: [\"mailto:\"]}":                                "not a complete mailto: URI",
		"{contact: [\"https://x.io/\\nExpires: 1\"]}":             "site.security_txt.contact",
		"{contact: [\"tel:1\"], policy: \"mailto:x\"}":            "site.security_txt.policy",
		"{contact: [\"tel:1\"], expires_days: 400}":               "expires_days",
		"{contact: [\"tel:1\"], expires_days: -1}":                "expires_days",
		"{contact: [\"tel:1\"], preferred_languages: \"en; es\"}": "preferred_languages",
	} {
		if _, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {security_txt: "+body+"}\n")); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", body, err, want)
		}
	}
	if _, err := Load(writeConfig(t, "vault: /v\nroot: R\nsite: {base_url: \"https://x.io/sub/\", security_txt: {contact: [\"tel:1\"]}}\n")); err == nil || !strings.Contains(err.Error(), "root of its host") {
		t.Errorf("sub-path site: %v", err)
	}
	if _, err := Load(writeConfig(t, "vault: /v\nroot: R\ntarget: jekyll\nsite: {security_txt: {contact: [\"tel:1\"]}}\n")); err == nil || !strings.Contains(err.Error(), "html target") {
		t.Errorf("jekyll target: %v", err)
	}
}

func TestInclude(t *testing.T) {
	c, err := Load(writeConfig(t, "vault: /v\nroot: R\nexclude: [Team/**]\ninclude:\n  - path: Team/**\n    tags: [profile, \"#shared/public\"]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Include) != 1 || c.Include[0].Path != "Team/**" || len(c.Include[0].Tags) != 2 {
		t.Errorf("include %+v", c.Include)
	}
	if _, err := Load(writeConfig(t, "vault: /v\nroot: R\ninclude: [{path: a, tags: [b], tag: c}]\n")); err == nil {
		t.Error("unknown key in an include rule accepted")
	}
	for _, tc := range []struct {
		rule Include
		want string
	}{
		{Include{Tags: []string{"a"}}, "include[0].path is required"},
		{Include{Path: "[a", Tags: []string{"a"}}, "include[0].path \"[a\" is not a valid glob"},
		{Include{Path: "/Team/**", Tags: []string{"a"}}, "include[0].path \"/Team/**\" must be relative"},
		{Include{Path: "../Other/**", Tags: []string{"a"}}, "include[0].path \"../Other/**\" must be relative"},
		{Include{Path: "Team/**"}, "include[0].tags needs at least one tag"},
		{Include{Path: "Team/**", Tags: []string{"two words"}}, "include[0].tags \"two words\" must be a tag"},
		{Include{Path: "Team/**", Tags: []string{"ok", "#"}}, "include[0].tags \"#\" must be a tag"},
		{Include{Path: "Team/**", Tags: []string{"x/"}}, "include[0].tags \"x/\" must be a tag"},
	} {
		c := &Config{Vault: "/v", Root: "R", Target: "html", Search: "none", Include: []Include{tc.rule}}
		if err := c.Validate(); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: want error containing %q, got %v", tc.rule, tc.want, err)
		}
	}
	c = &Config{Vault: "/v", Root: "R", Target: "html", Search: "none",
		Include: []Include{{Path: "Team/**", Tags: []string{"a"}}, {Path: "x", Tags: []string{"b"}}, {Path: "y", Tags: []string{"b c"}}}}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "include[2].tags") || strings.Contains(err.Error(), "include[0]") {
		t.Errorf("index in message: %v", err)
	}
}
