// Package deploy publishes a built site to a hosting provider.
package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/secret"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/deweysasser/obsidian-publish-website/internal/timing"
	"github.com/deweysasser/obsidian-publish-website/internal/tools"
	"github.com/rs/zerolog"
)

var deploymentURLRe = regexp.MustCompile(`https://\S+\.pages\.dev\S*`)

// Run deploys cfg.Output as configured in cfg.Deploy and returns the
// deployment URL reported by the provider.
func Run(ctx context.Context, cfg *config.Config, r tools.Runner, log zerolog.Logger) (string, error) {
	cf := cfg.Deploy.CloudflarePages
	if cf == nil {
		return "", errors.New("no deploy section in the config (deploy.cloudflare_pages)")
	}
	if _, err := os.Stat(filepath.Join(cfg.Output, target.Marker)); err != nil {
		return "", fmt.Errorf("%s was not built by this program (no %s marker); build it first", cfg.Output, target.Marker)
	}
	account, err := secret.Resolve(ctx, cf.AccountID)
	if err != nil {
		return "", fmt.Errorf("deploy.cloudflare_pages.account_id: %w", err)
	}
	token, err := secret.Resolve(ctx, cf.APIToken)
	if err != nil {
		return "", fmt.Errorf("deploy.cloudflare_pages.api_token: %w", err)
	}
	log.Info().Str("project", cf.Project).Str("branch", cf.Branch).Msg("Deploying to Cloudflare Pages")
	start := time.Now()
	out, err := r.Run(ctx, tools.Spec{
		Tool: "wrangler",
		Args: []string{"pages", "deploy", tools.SitePlaceholder, "--project-name", cf.Project, "--branch", cf.Branch, "--commit-dirty=true"},
		Site: cfg.Output, SiteReadOnly: true, Network: true,
		Env:     map[string]string{"CLOUDFLARE_ACCOUNT_ID": account},
		Secrets: map[string]string{"CLOUDFLARE_API_TOKEN": token},
	})
	log.Debug().Str("output", out).Msg("wrangler")
	if err != nil {
		return "", err
	}
	url := deploymentURLRe.FindString(out)
	log.Info().Str("url", url).Func(timing.Elapsed(start)).Msg("Deployed")
	return url, nil
}
