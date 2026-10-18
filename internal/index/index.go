package index

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

// Index is the whole-vault metadata index.
type Index struct {
	Vault *vault.Vault
	Pages map[string]*Page
	Paths []string // sorted note paths

	byName  map[string][]string // lowercase name → paths (notes: basename w/o .md; attachments: filename)
	byLower map[string]string   // lowercase path → path
	aliases map[string]string   // lowercase alias → note path
}

// Build extracts every note and resolves all links.
func Build(v *vault.Vault) *Index {
	ix := &Index{Vault: v, Pages: map[string]*Page{}, byName: map[string][]string{}}
	for _, n := range v.Notes {
		p := Extract(n)
		ix.Pages[n.Path] = p
		ix.Paths = append(ix.Paths, n.Path)
		ix.addPath(n.Path)
	}
	for _, a := range v.Attachments {
		ix.addPath(a)
	}
	ix.indexAliases()
	inlinks := map[string]map[string]bool{}
	for _, src := range ix.Paths {
		p := ix.Pages[src]
		ix.resolveLinks(p.Links, src)
		for i := range p.Lists {
			ix.resolveLinks(p.Lists[i].Links, src)
		}
		for _, l := range p.Links {
			if l.Path != "" && l.Path != src && ix.Pages[l.Path] != nil {
				if inlinks[l.Path] == nil {
					inlinks[l.Path] = map[string]bool{}
				}
				inlinks[l.Path][src] = true
			}
		}
	}
	for dst, srcs := range inlinks {
		for s := range srcs {
			ix.Pages[dst].Inlinks = append(ix.Pages[dst].Inlinks, s)
		}
		sort.Strings(ix.Pages[dst].Inlinks)
	}
	return ix
}

func (ix *Index) resolveLinks(ls []Link, from string) {
	for i := range ls {
		if ls[i].Target == "" {
			ls[i].Path = from // [[#Heading]] is a self link
			continue
		}
		ls[i].Path, _ = ix.Resolve(ls[i].Target, from)
	}
}

func (ix *Index) addPath(p string) {
	if ix.byLower == nil {
		ix.byLower = map[string]string{}
	}
	if ix.byName == nil {
		ix.byName = map[string][]string{}
	}
	lp := strings.ToLower(p)
	ix.byLower[lp] = p
	ix.byName[nameKey(p)] = append(ix.byName[nameKey(p)], p)
}

func (ix *Index) indexAliases() {
	ix.aliases = map[string]string{}
	for _, p := range ix.Paths {
		for _, a := range ix.Pages[p].Aliases {
			k := strings.ToLower(a)
			if _, dup := ix.aliases[k]; !dup {
				ix.aliases[k] = p
			}
		}
	}
}

func nameKey(p string) string {
	b := strings.ToLower(path.Base(p))
	return strings.TrimSuffix(b, ".md")
}

var attachExtRe = regexp.MustCompile(`(?i)\.[a-z0-9]{1,5}$`)

// Resolve finds the vault path a link target refers to, following Obsidian's
// rules: exact path, path relative to the linking note, then basename match
// (case-insensitive) preferring the same folder, then the shallowest path.
// Note aliases are consulted last.
func (ix *Index) Resolve(target, from string) (string, bool) {
	target = strings.TrimSpace(strings.TrimPrefix(target, "/"))
	if target == "" {
		return "", false
	}
	lt := strings.ToLower(target)
	var forms []string // lowercase candidate full paths (without dir), in priority order
	if strings.HasSuffix(lt, ".md") {
		forms = []string{lt}
	} else if attachExtRe.MatchString(lt) {
		forms = []string{lt, lt + ".md"}
	} else {
		forms = []string{lt + ".md"}
	}
	dir := strings.ToLower(path.Dir(from))
	for _, f := range forms {
		if p, ok := ix.byLower[f]; ok {
			return p, true
		}
		if dir != "." {
			if p, ok := ix.byLower[path.Clean(dir+"/"+f)]; ok {
				return p, true
			}
		}
	}
	for _, f := range forms {
		cands := ix.byName[nameKey(f)]
		if strings.Contains(f, "/") {
			var keep []string
			for _, c := range cands {
				if strings.HasSuffix(strings.ToLower(c), "/"+strings.TrimPrefix(path.Clean(f), "../")) {
					keep = append(keep, c)
				}
			}
			cands = keep
		}
		if p := pick(cands, from); p != "" {
			return p, true
		}
	}
	if p, ok := ix.aliases[lt]; ok {
		return p, true
	}
	return "", false
}

func pick(cands []string, from string) string {
	if len(cands) == 0 {
		return ""
	}
	dir := path.Dir(from)
	for _, c := range cands {
		if path.Dir(c) == dir {
			return c
		}
	}
	best := cands[0]
	for _, c := range cands[1:] {
		dc, db := strings.Count(c, "/"), strings.Count(best, "/")
		if dc < db || dc == db && (len(c) < len(best) || len(c) == len(best) && c < best) {
			best = c
		}
	}
	return best
}
