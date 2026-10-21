// Package tools runs the external programs the publisher depends on
// (pagefind, wrangler), either in a pinned container image built from the
// embedded Dockerfile ("docker", the default) or from PATH ("host").
package tools

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

//go:embed Dockerfile
var dockerfile []byte

// SitePlaceholder in Spec.Args is replaced by the site directory's path as
// the tool sees it.
const SitePlaceholder = "{site}"

// Spec describes one tool invocation.
type Spec struct {
	Tool         string
	Args         []string
	Site         string // host directory made available to the tool ("" = none)
	SiteReadOnly bool
	Network      bool              // docker: the tool needs network access
	Env          map[string]string // non-secret environment
	// Secrets are environment variables that must not appear in process
	// arguments or container configuration. Docker receives them on stdin.
	Secrets map[string]string
}

// Runner runs tools.
type Runner interface {
	// Check reports early whether tool can be run at all.
	Check(tool string) error
	// Run executes spec and returns its combined output.
	Run(ctx context.Context, spec Spec) (string, error)
}

// New returns the runner for mode ("docker" or "host"). image overrides
// the docker image; "" uses the embedded Dockerfile.
func New(mode, image string) Runner {
	if mode == "host" {
		return host{}
	}
	return &docker{image: image}
}

// ImageTag is the tag of the image built from the embedded Dockerfile; it
// changes whenever the Dockerfile does.
func ImageTag() string {
	sum := sha256.Sum256(dockerfile)
	return "obsidian-publish-tools:" + hex.EncodeToString(sum[:])[:12]
}

type host struct{}

func (host) Check(tool string) error {
	if _, err := exec.LookPath(tool); err != nil {
		return fmt.Errorf("%s not found on PATH (tools.runner: host); install it or use tools.runner: docker", tool)
	}
	return nil
}

func (host) Run(ctx context.Context, s Spec) (string, error) {
	args := make([]string, len(s.Args))
	for i, a := range s.Args {
		args[i] = strings.ReplaceAll(a, SitePlaceholder, s.Site)
	}
	cmd := exec.CommandContext(ctx, s.Tool, args...)
	cmd.Env = append(os.Environ(), envList(s.Env)...)
	cmd.Env = append(cmd.Env, envList(s.Secrets)...)
	return run(cmd, s.Tool)
}

type docker struct {
	image string
	ready bool
}

func (*docker) Check(string) error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker not found on PATH (tools.runner: docker runs pagefind and wrangler in a container); install docker or use tools.runner: host")
	}
	return nil
}

// secretsShim exports NAME=value lines from stdin, then runs the tool.
const secretsShim = `while IFS= read -r l; do export "$l"; done; exec "$@"`

func (d *docker) Run(ctx context.Context, s Spec) (string, error) {
	image, err := d.ensureImage(ctx)
	if err != nil {
		return "", err
	}
	args := []string{"run", "--rm", "-i"}
	if s.Network {
		args = append(args, "--network", "host")
	} else {
		args = append(args, "--network", "none")
	}
	if s.Site != "" && !s.SiteReadOnly {
		args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()), "-e", "HOME=/tmp")
	}
	for _, e := range envList(s.Env) {
		args = append(args, "-e", e)
	}
	if s.Site != "" {
		mount := s.Site + ":/site"
		if s.SiteReadOnly {
			mount += ":ro"
		}
		args = append(args, "-v", mount)
	}
	args = append(args, image, "sh", "-c", secretsShim, "run", s.Tool)
	for _, a := range s.Args {
		args = append(args, strings.ReplaceAll(a, SitePlaceholder, "/site"))
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	var stdin strings.Builder
	for _, e := range envList(s.Secrets) {
		if strings.ContainsAny(e, "\r\n") {
			return "", fmt.Errorf("secret for %s contains a newline", strings.SplitN(e, "=", 2)[0])
		}
		stdin.WriteString(e + "\n")
	}
	cmd.Stdin = strings.NewReader(stdin.String())
	return run(cmd, s.Tool)
}

func (d *docker) ensureImage(ctx context.Context) (string, error) {
	image := d.image
	if image == "" {
		image = ImageTag()
	}
	if d.ready || exec.CommandContext(ctx, "docker", "image", "inspect", image).Run() == nil {
		d.ready = true
		return image, nil
	}
	if d.image != "" {
		return "", fmt.Errorf("docker image %s not found (tools.image)", image)
	}
	cmd := exec.CommandContext(ctx, "docker", "build", "--network", "host", "-t", image, "-")
	cmd.Stdin = bytes.NewReader(dockerfile)
	if _, err := run(cmd, "docker build"); err != nil {
		return "", err
	}
	d.ready = true
	return image, nil
}

func run(cmd *exec.Cmd, name string) (string, error) {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s failed: %v\n%s", name, err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

func envList(m map[string]string) []string {
	var l []string
	for k, v := range m {
		l = append(l, k+"="+v)
	}
	sort.Strings(l)
	return l
}
