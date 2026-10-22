// Package search builds the site search index with Pagefind.
package search

import (
	"context"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/tools"
	"github.com/rs/zerolog"
)

// RunPagefind indexes siteDir in place (writes siteDir/pagefind/).
func RunPagefind(ctx context.Context, r tools.Runner, siteDir string, log zerolog.Logger) error {
	start := time.Now()
	out, err := r.Run(ctx, tools.Spec{Tool: "pagefind", Args: []string{"--site", tools.SitePlaceholder}, Site: siteDir})
	log.Debug().Str("output", out).Msg("pagefind")
	if err != nil {
		return err
	}
	log.Info().Stringer("elapsed", time.Since(start).Round(time.Millisecond)).Msg("Indexed site search")
	return nil
}
