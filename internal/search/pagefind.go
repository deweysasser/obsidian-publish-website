// Package search builds the site search index with Pagefind.
package search

import (
	"context"
	_ "embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/timing"
	"github.com/deweysasser/obsidian-publish-website/internal/tools"
	"github.com/rs/zerolog"
)

//go:embed indexer.mjs
var indexerJS []byte

// Temporary files RunPagefind puts in the site for the indexer; removed
// before it returns, so they are never published.
const (
	indexerFile = ".pagefind-indexer.mjs"
	orderFile   = ".pagefind-order"
)

// apiUnavailable is what indexer.mjs prints when it can't load Pagefind's
// Node API (for example tools.runner: host with only the pagefind binary).
const apiUnavailable = "PAGEFIND_NODE_API_UNAVAILABLE"

// RunPagefind indexes siteDir in place (writes siteDir/pagefind/). It adds
// pages in the order of the previous index, read from stateFile ("" keeps
// no state), and saves the new order there after a successful index. See
// order.go for why.
func RunPagefind(ctx context.Context, r tools.Runner, siteDir, stateFile string, log zerolog.Logger) error {
	start := time.Now()
	pages, err := htmlPages(siteDir)
	if err != nil {
		return err
	}
	order, st := stableOrder(loadOrder(stateFile), pages)
	ordered, err := runOrdered(ctx, r, siteDir, order, log)
	if err != nil {
		return err
	}
	ev := log.Info().Int("pages", len(order)).Bool("stable_order", ordered)
	if ordered {
		ev = ev.Int("new", st.New).Int("removed", st.Removed)
		if stateFile != "" {
			if err := saveOrder(stateFile, order); err != nil {
				log.Warn().Err(err).Msg("Could not save the search page order; the next index renumbers every page")
			}
		}
	}
	ev.Func(timing.Elapsed(start)).Msg("Indexed site search")
	return nil
}

// runOrdered indexes with indexer.mjs in the given order. When the Node API
// isn't available it runs the pagefind CLI instead (path order) and
// reports ordered=false.
func runOrdered(ctx context.Context, r tools.Runner, siteDir string, order []string, log zerolog.Logger) (ordered bool, err error) {
	if err := r.Check("node"); err != nil {
		return false, runCLI(ctx, r, siteDir, "node is not available", log)
	}
	script, list := filepath.Join(siteDir, indexerFile), filepath.Join(siteDir, orderFile)
	defer os.Remove(script)
	defer os.Remove(list)
	if err := os.WriteFile(script, indexerJS, 0o644); err != nil { // #nosec G306 -- read by node in the container
		return false, err
	}
	if err := os.WriteFile(list, []byte(strings.Join(order, "\n")+"\n"), 0o644); err != nil { // #nosec G306 -- as above
		return false, err
	}
	out, err := r.Run(ctx, tools.Spec{Tool: "node", Site: siteDir, Args: []string{
		tools.SitePlaceholder + "/" + indexerFile, tools.SitePlaceholder, tools.SitePlaceholder + "/" + orderFile}})
	log.Debug().Str("output", out).Msg("pagefind (node)")
	if err == nil {
		return true, nil
	}
	if !strings.Contains(out, apiUnavailable) && !strings.Contains(err.Error(), apiUnavailable) {
		return false, err
	}
	return false, runCLI(ctx, r, siteDir, "Pagefind's Node API is not available", log)
}

// runCLI indexes with the pagefind CLI, in path order.
func runCLI(ctx context.Context, r tools.Runner, siteDir, why string, log zerolog.Logger) error {
	log.Warn().Str("reason", why).Msg("Indexing with the pagefind CLI, which renumbers every page on each build")
	out, err := r.Run(ctx, tools.Spec{Tool: "pagefind", Args: []string{"--site", tools.SitePlaceholder}, Site: siteDir})
	log.Debug().Str("output", out).Msg("pagefind")
	return err
}

// htmlPages lists the site's .html files as slash-separated paths relative
// to siteDir, sorted, skipping an existing pagefind/ directory.
func htmlPages(siteDir string) ([]string, error) {
	var pages []string
	err := filepath.WalkDir(siteDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(siteDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "pagefind" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(rel, ".html") {
			pages = append(pages, rel)
		}
		return nil
	})
	sort.Strings(pages)
	return pages, err
}
