// Package build orchestrates a full site build.
package build

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/dataview"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/search"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
	"github.com/deweysasser/obsidian-publish-website/internal/target/html"
	"github.com/deweysasser/obsidian-publish-website/internal/target/hugo"
	"github.com/deweysasser/obsidian-publish-website/internal/target/jekyll"
	"github.com/deweysasser/obsidian-publish-website/internal/tools"
	"github.com/deweysasser/obsidian-publish-website/internal/transform"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
	"github.com/rs/zerolog"
)

// Report summarizes a build.
type Report struct {
	Pages    int
	Assets   int
	Warnings []string
}

// Run builds the site described by cfg.
func Run(cfg *config.Config, log zerolog.Logger) (*Report, error) {
	rep := &Report{}
	warn := func(msg string) {
		rep.Warnings = append(rep.Warnings, msg)
		log.Warn().Msg(msg)
	}
	if st, err := os.Stat(filepath.Join(cfg.Vault, filepath.FromSlash(cfg.Root))); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("root %q is not a folder in vault %s", cfg.Root, cfg.Vault)
	}
	transforms, err := transform.Chain(cfg.Transforms)
	if err != nil {
		return nil, err
	}
	tgt, err := newTarget(cfg, warn, log)
	if err != nil {
		return nil, err
	}
	if cfg.Target == "html" {
		if u, err := url.Parse(cfg.Site.BaseURL); err != nil || u.Scheme == "" || u.Host == "" {
			warn("site.base_url is not an absolute URL; sitemap.xml will not be written")
		}
	}
	var runner tools.Runner
	if cfg.Search == "pagefind" && cfg.Target == "html" {
		runner = tools.New(cfg.Tools.Runner, cfg.Tools.Image)
		if err := runner.Check("pagefind"); err != nil {
			return nil, err
		}
	}

	start := time.Now()
	v, err := vault.Load(cfg.Vault)
	if err != nil {
		return nil, err
	}
	for _, w := range v.Warnings {
		if strings.HasPrefix(w, cfg.Root+"/") { // problems outside the published tree are not ours to report
			warn(w)
		}
	}
	ix := index.Build(v)
	pub, err := selector.Select(ix, cfg)
	if err != nil {
		return nil, err
	}
	log.Info().Int("notes", len(v.Notes)).Int("published", len(pub.Pages)).Dur("elapsed", time.Since(start)).Msg("Indexed vault")

	r := render.NewRenderer(ix, pub, tgt.Scheme(), tgt.Mode(), func(p string, line int, msg string) {
		warn(fmt.Sprintf("%s:%d: %s", p, line, msg))
	})
	r.StrictLineBreaks = vault.StrictLineBreaks(cfg.Vault)
	if err := evaluateDataview(cfg, r, ix, log); err != nil {
		return nil, err
	}
	var paths []string
	for p := range pub.Pages {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	site := &target.Site{Config: cfg, Tags: map[string][]*render.Page{}, Build: time.Now(), URLs: tgt.Scheme()}
	for _, p := range paths {
		page, err := r.Render(p)
		if err != nil {
			return nil, err
		}
		if err := transform.Apply(transforms, page); err != nil {
			return nil, err
		}
		if err := r.Finish(page); err != nil {
			return nil, err
		}
		site.Pages = append(site.Pages, page)
		for _, tg := range page.Tags {
			k := strings.ToLower(tg) // #Project and #project share one tag page
			site.Tags[k] = append(site.Tags[k], page)
		}
	}
	sort.Slice(site.Pages, func(i, j int) bool { return site.Pages[i].Slug < site.Pages[j].Slug })
	site.Tree = target.BuildTree(pub, site.Pages, tgt.Scheme())
	for src, slug := range pub.Assets {
		site.Assets = append(site.Assets, target.Asset{Src: v.AbsPath(src), Slug: slug})
	}
	sort.Slice(site.Assets, func(i, j int) bool { return site.Assets[i].Slug < site.Assets[j].Slug })

	if cfg.Strict && len(rep.Warnings) > 0 {
		return rep, fmt.Errorf("--strict: %d warning(s):\n  %s", len(rep.Warnings), strings.Join(rep.Warnings, "\n  "))
	}
	if err := tgt.Prepare(); err != nil {
		return nil, err
	}
	for _, p := range site.Pages {
		if err := tgt.WritePage(site, p); err != nil {
			return nil, fmt.Errorf("%s: %w", p.SourcePath, err)
		}
	}
	for _, a := range site.Assets {
		if err := tgt.CopyAsset(a); err != nil {
			return nil, err
		}
	}
	if err := tgt.Finalize(site); err != nil {
		return nil, err
	}
	if runner != nil {
		if err := search.RunPagefind(context.Background(), runner, cfg.Output, log); err != nil {
			return nil, err
		}
	}
	rep.Pages, rep.Assets = len(site.Pages), len(site.Assets)
	log.Info().Int("pages", rep.Pages).Int("assets", rep.Assets).Int("warnings", len(rep.Warnings)).
		Str("output", cfg.Output).Dur("elapsed", time.Since(start)).Msg("Site built")
	return rep, nil
}

// evaluateDataview runs every query in the published pages through the real
// Dataview engine in one batch.
func evaluateDataview(cfg *config.Config, r *render.Renderer, ix *index.Index, log zerolog.Logger) error {
	if cfg.Dataview == "none" {
		r.DisableDataview()
		return nil
	}
	settings, err := dataview.LoadSettings(cfg.Vault)
	if err != nil {
		return err
	}
	if d, ok := settings["maxRecursiveRenderDepth"].(float64); ok && d > 0 {
		r.MaxEmbedDepth = int(d)
	}
	// settings (inline prefix, embed depth) must be in place before collection
	r.SetDataview(nil, settings)
	reqs, err := r.CollectQueries()
	if err != nil || len(reqs) == 0 {
		return err
	}
	node, err := dataview.FindNode()
	if err != nil {
		return err
	}
	start := time.Now()
	var include func(string) bool
	if cfg.DataviewScope != "vault" {
		include = r.Pub.IsPublished
	}
	res, err := dataview.NewEngine(node, settings, ix, include).RunIndex(reqs)
	if err != nil {
		return err
	}
	r.SetDataview(res, settings)
	log.Info().Int("queries", len(reqs)).Dur("elapsed", time.Since(start)).Msg("Evaluated dataview queries")
	return nil
}

func newTarget(cfg *config.Config, warn func(string), log zerolog.Logger) (target.Target, error) {
	switch cfg.Target {
	case "html":
		t, err := html.New(cfg)
		if err != nil {
			return nil, err
		}
		t.Warn = warn
		return t, nil
	case "jekyll":
		t := jekyll.New(cfg)
		t.Warn = func(m string) { log.Warn().Msg(m) }
		return t, nil
	case "hugo":
		t := hugo.New(cfg)
		t.Warn = func(m string) { log.Warn().Msg(m) }
		return t, nil
	}
	return nil, errors.New("target " + cfg.Target + " is not implemented yet")
}
