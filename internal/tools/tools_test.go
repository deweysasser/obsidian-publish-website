package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeBin installs an executable shell script named name in a fresh PATH dir.
func fakeBin(t *testing.T, dir, name, script string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func fakePath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	return dir
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, _ := os.ReadFile(p)
	return string(b)
}

func TestDockerSecretsTravelOnStdinOnly(t *testing.T) {
	bin := fakePath(t)
	log := filepath.Join(t.TempDir(), "log")
	fakeBin(t, bin, "docker", `case "$1" in
image) exit 0;;
run) echo "ARGS $*" >> `+log+`; cat >> `+log+`; echo "ok output";;
esac
`)
	site := t.TempDir()
	r := New("docker", "")
	out, err := r.Run(context.Background(), Spec{
		Tool: "wrangler", Args: []string{"pages", "deploy", SitePlaceholder},
		Site: site, SiteReadOnly: true, Network: true,
		Env:     map[string]string{"CLOUDFLARE_ACCOUNT_ID": "acct"},
		Secrets: map[string]string{"CLOUDFLARE_API_TOKEN": "s3cret"},
	})
	if err != nil || !strings.Contains(out, "ok output") {
		t.Fatalf("out %q err %v", out, err)
	}
	got := read(t, log)
	args, stdin, _ := strings.Cut(got, "\n")
	if strings.Contains(args, "s3cret") || !strings.Contains(stdin, "CLOUDFLARE_API_TOKEN=s3cret") {
		t.Errorf("secret must be on stdin only:\nargs: %s\nstdin: %s", args, stdin)
	}
	for _, want := range []string{"--rm", "-i", "--network host", "-e CLOUDFLARE_ACCOUNT_ID=acct", "-v " + site + ":/site:ro", ImageTag(), "wrangler pages deploy /site"} {
		if !strings.Contains(args, want) {
			t.Errorf("args missing %q: %s", want, args)
		}
	}
}

func TestDockerNoNetworkAndWritableSite(t *testing.T) {
	bin := fakePath(t)
	log := filepath.Join(t.TempDir(), "log")
	fakeBin(t, bin, "docker", `case "$1" in image) exit 0;; run) echo "$*" > `+log+`;; esac`)
	site := t.TempDir()
	if _, err := New("docker", "").Run(context.Background(), Spec{Tool: "pagefind", Args: []string{"--site", SitePlaceholder}, Site: site}); err != nil {
		t.Fatal(err)
	}
	args := read(t, log)
	if !strings.Contains(args, "--network none") || !strings.Contains(args, "-v "+site+":/site ") || !strings.Contains(args, "--user ") {
		t.Errorf("args %s", args)
	}
}

func TestDockerBuildsMissingImage(t *testing.T) {
	bin := fakePath(t)
	log := filepath.Join(t.TempDir(), "log")
	fakeBin(t, bin, "docker", `case "$1" in
image) exit 1;;
build) echo "BUILD $*" >> `+log+`; cat >> `+log+`;;
run) echo "RUN" >> `+log+`;;
esac
`)
	if _, err := New("docker", "").Run(context.Background(), Spec{Tool: "pagefind"}); err != nil {
		t.Fatal(err)
	}
	got := read(t, log)
	if !strings.Contains(got, "BUILD build") || !strings.Contains(got, "-t "+ImageTag()) || !strings.Contains(got, "pagefind@") || !strings.HasSuffix(strings.TrimSpace(got), "RUN") {
		t.Errorf("log:\n%s", got)
	}
}

func TestCustomImageIsNotBuilt(t *testing.T) {
	bin := fakePath(t)
	log := filepath.Join(t.TempDir(), "log")
	fakeBin(t, bin, "docker", `echo "$1" >> `+log+`; [ "$1" != image ]`)
	_, err := New("docker", "my/tools:1").Run(context.Background(), Spec{Tool: "pagefind"})
	if err == nil || !strings.Contains(err.Error(), "my/tools:1") || strings.Contains(read(t, log), "build") {
		t.Errorf("err %v log %s", err, read(t, log))
	}
}

func TestHostRunnerPassesSecretsInEnv(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "wrangler", `echo "args=$* token=$CLOUDFLARE_API_TOKEN acct=$CLOUDFLARE_ACCOUNT_ID"`)
	site := t.TempDir()
	out, err := New("host", "").Run(context.Background(), Spec{
		Tool: "wrangler", Args: []string{"deploy", SitePlaceholder},
		Site: site, Env: map[string]string{"CLOUDFLARE_ACCOUNT_ID": "a"}, Secrets: map[string]string{"CLOUDFLARE_API_TOKEN": "t"},
	})
	if err != nil || strings.TrimSpace(out) != "args=deploy "+site+" token=t acct=a" {
		t.Errorf("out %q err %v", out, err)
	}
}

func TestCheck(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if err := New("docker", "").Check("pagefind"); err == nil || !strings.Contains(err.Error(), "docker") {
		t.Errorf("docker check: %v", err)
	}
	if err := New("host", "").Check("pagefind"); err == nil || !strings.Contains(err.Error(), "pagefind") {
		t.Errorf("host check: %v", err)
	}
}

func TestFailureIncludesOutput(t *testing.T) {
	bin := fakePath(t)
	fakeBin(t, bin, "pagefind", `echo boom; exit 3`)
	_, err := New("host", "").Run(context.Background(), Spec{Tool: "pagefind"})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("err %v", err)
	}
}

// The published image (../../Dockerfile) runs the same tools in host mode;
// its Node, wrangler and pagefind must match the embedded tools image.
func TestPublishedImagePinsMatchToolsImage(t *testing.T) {
	pins := func(dockerfile string) []string {
		var l []string
		for _, ln := range strings.Split(dockerfile, "\n") {
			for _, f := range strings.Fields(ln) {
				if strings.HasPrefix(f, "node:") || strings.HasPrefix(f, "wrangler@") || strings.HasPrefix(f, "pagefind@") {
					l = append(l, f)
				}
			}
		}
		return l
	}
	want := pins(string(dockerfile))
	got := pins(read(t, "../../Dockerfile"))
	if len(want) != 3 || strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("Dockerfile pins %v, internal/tools/Dockerfile pins %v", got, want)
	}
}
