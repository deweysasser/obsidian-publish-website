package deploy

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/deweysasser/obsidian-publish-website/internal/tools"
	"github.com/rs/zerolog"
)

type fakeRunner struct {
	got tools.Spec
	out string
	err error
}

func (f *fakeRunner) Check(string) error { return nil }
func (f *fakeRunner) Run(_ context.Context, s tools.Spec) (string, error) {
	f.got = s
	return f.out, f.err
}

func builtSite(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, target.Marker), nil, 0o644)
	return dir
}

func cfgFor(site string) *config.Config {
	return &config.Config{Output: site, Target: "html", Deploy: config.Deploy{CloudflarePages: &config.CloudflarePages{
		Project: "proj", Branch: "main", AccountID: "acct123", APIToken: "env:TEST_CF_TOKEN"}}}
}

func TestCloudflarePages(t *testing.T) {
	t.Setenv("TEST_CF_TOKEN", "tok")
	site := builtSite(t)
	r := &fakeRunner{out: "Uploading...\n✨ Deployment complete! Take a peek over at https://abc.proj.pages.dev\n"}
	url, err := Run(context.Background(), cfgFor(site), r, zerolog.Nop())
	if err != nil || url != "https://abc.proj.pages.dev" {
		t.Fatalf("url %q err %v", url, err)
	}
	g := r.got
	if g.Tool != "wrangler" || strings.Join(g.Args, " ") != "pages deploy {site} --project-name proj --branch main --commit-dirty=true" ||
		g.Site != site || !g.SiteReadOnly || !g.Network || g.Env["CLOUDFLARE_ACCOUNT_ID"] != "acct123" || g.Secrets["CLOUDFLARE_API_TOKEN"] != "tok" {
		t.Errorf("spec %+v", g)
	}
	if _, ok := g.Env["CLOUDFLARE_API_TOKEN"]; ok {
		t.Error("token must be a secret, not plain env")
	}
}

func TestRefusesUnbuiltDirectory(t *testing.T) {
	t.Setenv("TEST_CF_TOKEN", "tok")
	r := &fakeRunner{}
	if _, err := Run(context.Background(), cfgFor(t.TempDir()), r, zerolog.Nop()); err == nil || r.got.Tool != "" {
		t.Errorf("deployed a directory this tool did not build: %v", err)
	}
}

func TestNoDeployConfigured(t *testing.T) {
	c := &config.Config{Output: builtSite(t)}
	if _, err := Run(context.Background(), c, &fakeRunner{}, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "deploy") {
		t.Errorf("err %v", err)
	}
}

func TestSecretAndToolErrors(t *testing.T) {
	site := builtSite(t)
	if _, err := Run(context.Background(), cfgFor(site), &fakeRunner{}, zerolog.Nop()); err == nil || !strings.Contains(err.Error(), "TEST_CF_TOKEN") {
		t.Errorf("missing token: %v", err)
	}
	t.Setenv("TEST_CF_TOKEN", "tok")
	if _, err := Run(context.Background(), cfgFor(site), &fakeRunner{err: errors.New("wrangler failed")}, zerolog.Nop()); err == nil {
		t.Error("tool failure should be an error")
	}
}

func TestLogsElapsed(t *testing.T) {
	t.Setenv("TEST_CF_TOKEN", "tok")
	site := builtSite(t)
	var buf bytes.Buffer
	r := &fakeRunner{out: "https://abc.proj.pages.dev\n"}
	if _, err := Run(context.Background(), cfgFor(site), r, zerolog.New(&buf)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"message":"Deployed"`) || !strings.Contains(buf.String(), `"elapsed":"`) {
		t.Errorf("log: %s", buf.String())
	}
}
