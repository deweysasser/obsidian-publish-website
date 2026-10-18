package selector

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/index"
)

// Published is the set of notes and attachments that make up the site.
// It is the single authority on what may appear in output.
type Published struct {
	Root    string
	Pages   map[string]string // note path → slug path ("" = home)
	Assets  map[string]string // attachment path → slug path (filled by MarkAsset)
	Folders map[string]string // vault folder path (root and below) → slug path

	exclude     []string
	folderNotes map[string]string
}

// Select computes the published set for cfg.
func Select(ix *index.Index, cfg *config.Config) (*Published, error) {
	pub := &Published{
		Root: cfg.Root, Pages: map[string]string{}, Assets: map[string]string{},
		Folders: map[string]string{cfg.Root: ""}, exclude: cfg.Exclude, folderNotes: map[string]string{},
	}
	home := ""
	if cfg.Site.Home != "" {
		home = path.Join(cfg.Root, cfg.Site.Home)
	}
	owner := map[string]string{} // slug → note path, for collision detection
	for _, p := range ix.Paths {
		rel, ok := pub.rel(p)
		if !ok || pub.excluded(rel) || publishFalse(ix.Pages[p].Note.Frontmatter) {
			continue
		}
		dir := path.Dir(rel)
		base := strings.TrimSuffix(path.Base(rel), path.Ext(rel))
		var slug string
		isFolderNote := strings.EqualFold(base, "index") || dir != "." && base == path.Base(dir) || dir == "." && base == path.Base(cfg.Root)
		switch {
		case isFolderNote:
			slug = SlugPath(dir)
			pub.folderNotes[path.Join(cfg.Root, dir)] = p
		default:
			slug = SlugPath(strings.TrimSuffix(rel, path.Ext(rel)))
		}
		if home != "" && isFolderNote && dir == "." && p != home {
			slug = SlugPath(base) // root folder note is an ordinary page when site.home is set
		}
		if p == home {
			slug = ""
		}
		if prev, dup := owner[slug]; dup {
			a, b := sortedPair(prev, p)
			return nil, fmt.Errorf("slug collision: %q and %q both map to %q", a, b, slug)
		}
		owner[slug] = p
		pub.Pages[p] = slug
		for d := dir; d != "."; d = path.Dir(d) {
			pub.Folders[path.Join(cfg.Root, d)] = SlugPath(d)
		}
	}
	if len(pub.Pages) == 0 {
		return nil, fmt.Errorf("no notes to publish under %q", cfg.Root)
	}
	return pub, nil
}

func sortedPair(a, b string) (string, string) {
	s := []string{a, b}
	sort.Strings(s)
	return s[0], s[1]
}

func (p *Published) rel(vaultPath string) (string, bool) {
	if !strings.HasPrefix(vaultPath, p.Root+"/") {
		return "", false
	}
	return strings.TrimPrefix(vaultPath, p.Root+"/"), true
}

// excluded reports whether a root-relative path, or any of its parent
// directories, matches an exclude pattern.
func (p *Published) excluded(rel string) bool {
	for _, pat := range p.exclude {
		for cur := rel; cur != "."; cur = path.Dir(cur) {
			if ok, _ := doublestar.Match(pat, cur); ok {
				return true
			}
		}
	}
	return false
}

// publishFalse reports whether frontmatter opts a note out. It fails closed:
// every "publish" key (any case) must be an unambiguous true to publish.
func publishFalse(fm map[string]any) bool {
	for k, v := range fm {
		if strings.EqualFold(k, "publish") && !isTrue(v) {
			return true
		}
	}
	return false
}

func isTrue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		switch strings.ToLower(strings.TrimSpace(x)) {
		case "true", "yes":
			return true
		}
	}
	return false
}

// IsPublished reports whether a note path is part of the site.
func (p *Published) IsPublished(notePath string) bool {
	_, ok := p.Pages[notePath]
	return ok
}

// IsAssetAllowed reports whether an attachment may be copied into the site.
func (p *Published) IsAssetAllowed(assetPath string) bool {
	rel, ok := p.rel(assetPath)
	return ok && !p.excluded(rel)
}

// MarkAsset records an attachment as published and returns its slug path.
func (p *Published) MarkAsset(assetPath string) string {
	if s, ok := p.Assets[assetPath]; ok {
		return s
	}
	rel, _ := p.rel(assetPath)
	s := AssetSlug(rel)
	p.Assets[assetPath] = s
	return s
}

// FolderNote returns the vault path of a folder's own note, or "".
func (p *Published) FolderNote(folder string) string { return p.folderNotes[folder] }
