// Package resolve turns links into published URLs or plain text. Together with
// selector it is the single place that decides whether a link target leaks.
package resolve

import (
	"path"
	"regexp"
	"strings"
	"unicode"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
)

// Result is the outcome of resolving one link.
type Result struct {
	Published bool
	URL       string // "" unless Published
	Path      string // resolved vault path, "" if unresolved
	Text      string // display text
	Asset     bool
}

type Resolver struct {
	Ix   *index.Index
	Pub  *selector.Published
	URLs urls.Scheme
}

// Link resolves an index link (Path already resolved by the index).
func (r *Resolver) Link(l index.Link, from string) Result {
	res := Result{Path: l.Path, Text: DisplayText(l)}
	if l.Path == "" {
		return res
	}
	if r.Ix.Pages[l.Path] == nil {
		res.Asset = true
		if r.Pub.IsAssetAllowed(l.Path) {
			res.Published = true
			res.URL = r.URLs.AssetURL(r.Pub.MarkAsset(l.Path))
		}
		return res
	}
	slug, ok := r.Pub.Pages[l.Path]
	if !ok {
		return res
	}
	res.Published = true
	res.URL = r.URLs.PageURL(slug) + Anchor(l.Subpath)
	return res
}

// Note resolves a link to a note by vault path (used for dataview values).
func (r *Resolver) Note(p, subpath, display string) Result {
	l := index.Link{Target: strings.TrimSuffix(path.Base(p), ".md"), Subpath: subpath, Display: display, Path: p}
	if r.Ix.Pages[p] == nil && path.Ext(p) == ".md" {
		l.Path = "" // unresolved note
	}
	return r.Link(l, "")
}

// DisplayText is Obsidian's default text for a link.
func DisplayText(l index.Link) string {
	if l.Display != "" && !(l.Embed && isSize(l.Display)) {
		return l.Display
	}
	switch {
	case l.Target == "":
		return l.Subpath
	case l.Subpath != "":
		return l.Target + " > " + l.Subpath
	default:
		return l.Target
	}
}

var sizeRe = regexp.MustCompile(`^\d+(x\d+)?$`)

func isSize(s string) bool { return sizeRe.MatchString(s) }

// Anchor returns the URL fragment for a heading or block subpath ("" if none).
func Anchor(subpath string) string {
	if subpath == "" {
		return ""
	}
	if strings.HasPrefix(subpath, "^") {
		return "#" + BlockID(subpath[1:])
	}
	// nested heading paths "A#B" target the last heading
	if i := strings.LastIndex(subpath, "#"); i >= 0 {
		subpath = subpath[i+1:]
	}
	return "#" + HeadingID(subpath)
}

// BlockID is the HTML id for a ^block reference.
func BlockID(id string) string { return "block-" + id }

var (
	hidWiki = regexp.MustCompile(`\[\[([^\]|]*\|)?([^\]]*)\]\]`)
	hidMd   = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// HeadingID is the HTML id for a heading: lowercase, markdown syntax removed,
// runs of other characters collapsed to "-". Letters, digits, "_" kept.
func HeadingID(text string) string {
	text = hidWiki.ReplaceAllString(text, "$2")
	text = hidMd.ReplaceAllString(text, "$1")
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
			dash = false
		case r == '*' || r == '`' || r == '=' || r == '~':
			// formatting characters vanish
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.TrimRight(b.String(), "-")
}
