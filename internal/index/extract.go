// Package index extracts Obsidian/Dataview metadata from every note in a vault.
package index

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
)

// Link is a wikilink, embed or markdown link to a note or attachment.
type Link struct {
	Raw      string // text inside [[ ]] or the markdown link target as written
	Target   string // note/file part (before #), URL-decoded for markdown links
	Subpath  string // heading text or "^block", without the leading '#'
	Display  string // alias / link text, "" if none
	Embed    bool
	Markdown bool   // [text](target) form
	Path     string // resolved vault path, "" if unresolved
	Line     int    // 1-based file line (0 for frontmatter)
}

// Field is an inline `key:: value` field.
type Field struct {
	Key, Raw string
	Line     int
}

type Heading struct {
	Level int
	Text  string
	Line  int
}

// ListItem is a list item or task.
type ListItem struct {
	Line, LineCount int
	Indent          int
	Text            string
	Task            bool
	Status          string
	Parent          int
	Children        []int
	BlockID         string
	Section         string
	Fields          []Field
	Tags            []string
	Links           []Link
}

// Page is the extracted metadata of one note.
type Page struct {
	Path     string
	Note     *vault.Note
	Tags     []string // explicit tags plus all parents of nested tags
	ETags    []string // explicit tags as written
	Aliases  []string
	Links    []Link // frontmatter links (sorted by key) then body links in order
	Fields   []Field
	Headings []Heading
	Blocks   map[string]int
	Lists    []ListItem
	Inlinks  []string
}

// Exported patterns are shared with the renderer so extraction and rewriting agree.
var (
	WikiRe         = regexp.MustCompile(`(!?)\[\[([^\[\]]+?)\]\]`)
	MdLinkRe       = regexp.MustCompile(`(!?)\[([^\[\]]*)\]\((<[^>]+>|[^)\s]+)(?:\s+"[^"]*")?\)`)
	SchemeRe       = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
	TagRe          = regexp.MustCompile(`(?:^|\s)#([\p{L}\p{N}_/-]+)`)
	digitsRe       = regexp.MustCompile(`^[0-9/]+$`)
	LineFieldRe    = regexp.MustCompile(`^\s*(?:[-*+]\s+(?:\[.\]\s+)?)?(\*\*|__|\*|_)?([^:*_\[\]()\s][^:*\[\]()]*?)(?:\*\*|__|\*|_)?::\s*(.*)$`)
	BracketFieldRe = regexp.MustCompile(`\[([^\[\]:]+?)::\s*([^\]]*)\]`)
	ParenFieldRe   = regexp.MustCompile(`\(([^()\[\]:]+?)::\s*([^)]*)\)`)
	listRe         = regexp.MustCompile(`^([ \t]*)([-*+]|\d+[.)])[ \t]+(?:\[(.)\][ \t]+)?(.*)$`)
	headingRe      = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*$`)
	BlockIDRe      = regexp.MustCompile(`(?:^|\s)\^([A-Za-z0-9-]+)\s*$`)
	fmLinkRe       = regexp.MustCompile(`^\s*\[\[([^\[\]]+)\]\]\s*$`)
	splitTagRe     = regexp.MustCompile(`[,\s]+`)
)

// Mask replaces inline code and math spans with spaces so regexes only see prose.
func Mask(line string) string {
	segs := mdscan.Inline(line)
	if len(segs) == 1 && !segs[0].Code {
		return line
	}
	var b strings.Builder
	for _, s := range segs {
		if s.Code {
			b.WriteString(strings.Repeat(" ", len(s.Text)))
		} else {
			b.WriteString(s.Text)
		}
	}
	return b.String()
}

// ParseWikilink splits the inside of [[...]].
func ParseWikilink(inner string, embed bool) Link {
	l := Link{Raw: inner, Embed: embed}
	t := inner
	if i := strings.Index(t, "|"); i >= 0 {
		l.Display = strings.TrimSpace(t[i+1:])
		t = t[:i]
	}
	if i := strings.Index(t, "#"); i >= 0 {
		l.Subpath = strings.TrimSpace(t[i+1:])
		t = t[:i]
	}
	l.Target = strings.TrimSpace(t)
	return l
}

// LinksInLine returns wikilinks and markdown links in a masked prose line, in order.
func LinksInLine(masked string, line int) []Link {
	type hit struct {
		pos int
		l   Link
	}
	var hits []hit
	for _, m := range WikiRe.FindAllStringSubmatchIndex(masked, -1) {
		l := ParseWikilink(masked[m[4]:m[5]], m[3] > m[2])
		l.Line = line
		hits = append(hits, hit{m[0], l})
	}
	for _, m := range MdLinkRe.FindAllStringSubmatchIndex(masked, -1) {
		// skip matches that are part of a wikilink
		if m[0] > 0 && masked[m[0]-1] == '[' {
			continue
		}
		raw := masked[m[6]:m[7]]
		tgt := strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">")
		if SchemeRe.MatchString(tgt) || strings.HasPrefix(tgt, "#") || tgt == "" {
			continue
		}
		l := Link{Raw: raw, Display: masked[m[4]:m[5]], Embed: m[3] > m[2], Markdown: true, Line: line}
		if i := strings.Index(tgt, "#"); i >= 0 {
			l.Subpath, _ = url.PathUnescape(tgt[i+1:])
			tgt = tgt[:i]
		}
		if dec, err := url.PathUnescape(tgt); err == nil {
			tgt = dec
		}
		l.Target = tgt
		hits = append(hits, hit{m[0], l})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].pos < hits[j].pos })
	out := make([]Link, len(hits))
	for i, h := range hits {
		out[i] = h.l
	}
	return out
}

// TagsInLine returns explicit tags (with '#') in a masked prose line.
func TagsInLine(masked string) []string {
	var out []string
	for _, m := range TagRe.FindAllStringSubmatch(masked, -1) {
		t := strings.TrimRight(m[1], "/")
		if t == "" || digitsRe.MatchString(t) {
			continue
		}
		out = append(out, "#"+t)
	}
	return out
}

// FieldsInLine returns inline fields in a masked prose line.
func FieldsInLine(masked string, line int) []Field {
	var out []Field
	for _, re := range []*regexp.Regexp{BracketFieldRe, ParenFieldRe} {
		for _, m := range re.FindAllStringSubmatch(masked, -1) {
			out = append(out, Field{Key: strings.TrimSpace(m[1]), Raw: strings.TrimSpace(m[2]), Line: line})
		}
	}
	if len(out) == 0 {
		if m := LineFieldRe.FindStringSubmatch(masked); m != nil {
			out = append(out, Field{Key: strings.TrimSpace(m[2]), Raw: strings.TrimSpace(m[3]), Line: line})
		}
	}
	return out
}

// Extract parses one note's metadata. Link paths are not resolved.
func Extract(n *vault.Note) *Page {
	p := &Page{Path: n.Path, Note: n, Blocks: map[string]int{}}
	var etags []string
	p.Links, etags, p.Aliases = frontmatterMeta(n.Frontmatter)

	section := ""
	type open struct{ indent, idx int }
	var stack []open
	lines := mdscan.Lines(n.Body, n.BodyLine)
	for _, ln := range lines {
		if ln.InFence || ln.FenceOpen || ln.FenceClose {
			if ln.FenceOpen && width(leadingWS(ln.Text)) == 0 {
				stack = nil
			}
			continue
		}
		text := ln.Text
		masked := Mask(text)
		if m := headingRe.FindStringSubmatch(masked); m != nil {
			section = strings.TrimSpace(strings.TrimRight(text[len(m[1]):], "# "))
			if section == "" {
				section = strings.TrimSpace(text[len(m[1]):])
			}
			p.Headings = append(p.Headings, Heading{Level: len(m[1]), Text: section, Line: ln.Num})
			stack = nil
		}
		links := LinksInLine(masked, ln.Num)
		tags := TagsInLine(masked)
		fields := FieldsInLine(masked, ln.Num)
		p.Links = append(p.Links, links...)
		etags = append(etags, tags...)
		p.Fields = append(p.Fields, fields...)
		blockID := ""
		if m := BlockIDRe.FindStringSubmatch(masked); m != nil {
			blockID = m[1]
			p.Blocks[blockID] = ln.Num
		}

		if m := listRe.FindStringSubmatch(text); m != nil {
			indent := width(m[1])
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			item := ListItem{Line: ln.Num, LineCount: 1, Indent: indent, Parent: -1, Section: section,
				Fields: fields, Tags: tags, Links: links, BlockID: blockID}
			body := m[4]
			if m[3] != "" {
				item.Task = true
				item.Status = m[3]
			}
			if blockID != "" {
				body = strings.TrimSpace(BlockIDRe.ReplaceAllString(body, ""))
			}
			item.Text = body
			idx := len(p.Lists)
			if len(stack) > 0 {
				item.Parent = stack[len(stack)-1].idx
				p.Lists[item.Parent].Children = append(p.Lists[item.Parent].Children, idx)
			}
			p.Lists = append(p.Lists, item)
			stack = append(stack, open{indent, idx})
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		// continuation line: belongs to the deepest open item indented less than it
		ind := width(leadingWS(text))
		for len(stack) > 0 && stack[len(stack)-1].indent >= ind {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			it := &p.Lists[stack[len(stack)-1].idx]
			it.LineCount = ln.Num - it.Line + 1
			it.Fields = append(it.Fields, fields...)
			it.Tags = append(it.Tags, tags...)
			it.Links = append(it.Links, links...)
		}
	}
	p.ETags = dedupe(etags)
	p.Tags = expandTags(p.ETags)
	return p
}

func leadingWS(s string) string { return s[:len(s)-len(strings.TrimLeft(s, " \t"))] }

func width(ws string) int {
	w := 0
	for _, c := range ws {
		if c == '\t' {
			w += 4
		} else {
			w++
		}
	}
	return w
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func expandTags(etags []string) []string {
	var out []string
	for _, t := range etags {
		parts := strings.Split(strings.TrimPrefix(t, "#"), "/")
		for i := 1; i < len(parts); i++ {
			out = append(out, "#"+strings.Join(parts[:i], "/"))
		}
		out = append(out, t)
	}
	return dedupe(out)
}

func frontmatterMeta(fm map[string]any) (links []Link, tags []string, aliases []string) {
	keys := make([]string, 0, len(fm))
	for k := range fm {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := fm[k]
		switch strings.ToLower(k) {
		case "tags", "tag":
			for _, t := range stringList(v, splitTagRe) {
				tags = append(tags, "#"+strings.TrimPrefix(t, "#"))
			}
		case "aliases", "alias":
			aliases = append(aliases, stringList(v, regexp.MustCompile(`\s*,\s*`))...)
		}
		walkStrings(v, func(s string) {
			if m := fmLinkRe.FindStringSubmatch(s); m != nil {
				links = append(links, ParseWikilink(m[1], false))
			}
		})
	}
	return
}

func stringList(v any, sep *regexp.Regexp) []string {
	var out []string
	switch x := v.(type) {
	case string:
		for _, s := range sep.Split(x, -1) {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	case []any:
		for _, e := range x {
			if e != nil {
				out = append(out, strings.TrimSpace(fmt.Sprint(e)))
			}
		}
	}
	return out
}

func walkStrings(v any, f func(string)) {
	switch x := v.(type) {
	case string:
		f(x)
	case []any:
		for _, e := range x {
			walkStrings(e, f)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkStrings(x[k], f)
		}
	}
}
