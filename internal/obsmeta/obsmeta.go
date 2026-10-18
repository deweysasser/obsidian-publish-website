// Package obsmeta emulates the subset of Obsidian's CachedMetadata that
// Dataview's page parser reads, with Obsidian's position conventions:
// 0-based lines over the whole file and UTF-16 columns/offsets.
package obsmeta

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/mdscan"
	"github.com/deweysasser/obsidian-publish-website/internal/vault"
	"gopkg.in/yaml.v3"
)

type Loc struct {
	Line   int `json:"line"`
	Col    int `json:"col"`
	Offset int `json:"offset"`
}

type Pos struct {
	Start Loc `json:"start"`
	End   Loc `json:"end"`
}

type LinkCache struct {
	Link        string `json:"link"`
	Original    string `json:"original"`
	DisplayText string `json:"displayText"`
	Position    Pos    `json:"position"`
}

type TagCache struct {
	Tag      string `json:"tag"`
	Position Pos    `json:"position"`
}

type HeadingCache struct {
	Heading  string `json:"heading"`
	Level    int    `json:"level"`
	Position Pos    `json:"position"`
}

type SectionCache struct {
	Type     string `json:"type"`
	ID       string `json:"id,omitempty"`
	Position Pos    `json:"position"`
}

type ListItemCache struct {
	Parent   int     `json:"parent"`
	Task     *string `json:"task,omitempty"`
	ID       string  `json:"id,omitempty"`
	Position Pos     `json:"position"`
}

type FrontmatterLink struct {
	Key         string `json:"key"`
	Link        string `json:"link"`
	Original    string `json:"original"`
	DisplayText string `json:"displayText"`
}

type CachedMetadata struct {
	Frontmatter      json.RawMessage   `json:"frontmatter,omitempty"`
	FrontmatterLinks []FrontmatterLink `json:"frontmatterLinks,omitempty"`
	Links            []LinkCache       `json:"links,omitempty"`
	Embeds           []LinkCache       `json:"embeds,omitempty"`
	Tags             []TagCache        `json:"tags,omitempty"`
	Headings         []HeadingCache    `json:"headings,omitempty"`
	Sections         []SectionCache    `json:"sections,omitempty"`
	ListItems        []ListItemCache   `json:"listItems,omitempty"`
}

type doc struct {
	lines []string
	off   []int // UTF-16 offset of the start of each line
	m     *CachedMetadata
}

func (d *doc) loc(line, byteCol int) Loc {
	c := u16(d.lines[line][:byteCol])
	return Loc{Line: line, Col: c, Offset: d.off[line] + c}
}

func (d *doc) span(l1, c1, l2, c2 int) Pos { return Pos{Start: d.loc(l1, c1), End: d.loc(l2, c2)} }

func (d *doc) lineSpan(l1, l2 int) Pos { return d.span(l1, 0, l2, len(d.lines[l2])) }

func u16(s string) int {
	n := 0
	for _, r := range s {
		n += len(utf16.Encode([]rune{r}))
	}
	return n
}

// Build computes the metadata for one note.
func Build(n *vault.Note) *CachedMetadata {
	raw := strings.ReplaceAll(string(n.Raw), "\r\n", "\n")
	d := &doc{lines: strings.Split(raw, "\n"), m: &CachedMetadata{}}
	o := 0
	for _, l := range d.lines {
		d.off = append(d.off, o)
		o += u16(l) + 1
	}
	start := 0
	if n.BodyLine > 1 {
		end := n.BodyLine - 2
		d.m.Sections = append(d.m.Sections, SectionCache{Type: "yaml", Position: d.lineSpan(0, end)})
		fm, _, _ := vault.SplitFrontmatter(n.Raw)
		d.frontmatter(fm)
		start = end + 1
	}
	d.blocks(start)
	byOffset := func(a, b Pos) int { return a.Start.Offset - b.Start.Offset }
	slices.SortStableFunc(d.m.Links, func(a, b LinkCache) int { return byOffset(a.Position, b.Position) })
	slices.SortStableFunc(d.m.Embeds, func(a, b LinkCache) int { return byOffset(a.Position, b.Position) })
	slices.SortStableFunc(d.m.Tags, func(a, b TagCache) int { return byOffset(a.Position, b.Position) })
	slices.SortStableFunc(d.m.ListItems, func(a, b ListItemCache) int { return byOffset(a.Position, b.Position) })
	return d.m
}

var (
	headingRe  = regexp.MustCompile(`^(#{1,6})(?:[ \t]+(.*?))?(?:[ \t]+#+)?[ \t]*$`)
	thematicRe = regexp.MustCompile(`^ {0,3}(?:(?:-[ \t]*){3,}|(?:\*[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	quoteRe    = regexp.MustCompile(`^ {0,3}>`)
	calloutRe  = regexp.MustCompile(`^ {0,3}>\s*\[!`)
	quotePfxRe = regexp.MustCompile(`^(?: {0,3}>[ ]?)+`)
	fenceRe    = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	listRe     = regexp.MustCompile(`^([ \t]*)([-*+]|\d{1,9}[.)])([ \t]+|$)`)
	taskRe     = regexp.MustCompile(`^\[(.)\][ \t]`)
	tableSepRe = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
	htmlRe     = regexp.MustCompile(`(?i)^ {0,3}<(/?)(div|details|summary|table|p|pre|section|span|iframe|img|center|figure|ul|ol|blockquote|h[1-6]|br|hr|video|audio|!--)`)
	footRe     = regexp.MustCompile(`^\[\^[^\]]+\]:`)
	setextRe   = regexp.MustCompile(`^ {0,3}(=+|-+)[ \t]*$`)
	blockIDRe  = regexp.MustCompile(`(?:^|\s)\^([A-Za-z0-9-]+)\s*$`)
)

func blank(s string) bool { return strings.TrimSpace(s) == "" }

func isCommentStart(s string) bool { return strings.HasPrefix(strings.TrimSpace(s), "%%") }

// startsBlock reports whether line can interrupt a paragraph.
func startsBlock(s string) bool {
	return headingRe.MatchString(s) && strings.HasPrefix(strings.TrimLeft(s, " "), "#") ||
		fenceRe.MatchString(s) || quoteRe.MatchString(s) || strings.TrimSpace(s) == "$$" ||
		isBullet(s) || thematicRe.MatchString(s) && !setextRe.MatchString(s) || htmlRe.MatchString(s)
}

func isBullet(s string) bool {
	m := listRe.FindStringSubmatch(s)
	return m != nil && len(m[1]) == 0 && (m[2] == "-" || m[2] == "*" || m[2] == "+" || m[2] == "1." || m[2] == "1)")
}

func listFamily(marker string) string {
	if marker == "-" || marker == "*" || marker == "+" {
		return "bullet"
	}
	return "ordered" + marker[len(marker)-1:]
}

func (d *doc) section(typ string, l1, l2 int) {
	d.m.Sections = append(d.m.Sections, SectionCache{Type: typ, Position: d.lineSpan(l1, l2)})
}

func (d *doc) blocks(i int) {
	L := d.lines
	n := len(L)
	for i < n {
		s := L[i]
		switch {
		case blank(s):
			i++
		case fenceRe.MatchString(s):
			fence := strings.TrimLeft(fenceRe.FindString(s), " ")
			j := i + 1
			for j < n {
				t := strings.TrimLeft(L[j], " ")
				if strings.HasPrefix(t, fence[:3]) && strings.Trim(strings.TrimSpace(t), string(fence[0])) == "" && len(strings.TrimSpace(t)) >= len(fence) {
					break
				}
				j++
			}
			j = min(j, n-1)
			d.section("code", i, j)
			i = j + 1
		case strings.TrimSpace(s) == "$$":
			j := i + 1
			for j < n && strings.TrimSpace(L[j]) != "$$" {
				j++
			}
			j = min(j, n-1)
			d.section("math", i, j)
			i = j + 1
		case isCommentStart(s):
			rest := strings.TrimSpace(s)[2:]
			j := i
			if !strings.Contains(rest, "%%") {
				j = i + 1
				for j < n && !strings.Contains(L[j], "%%") {
					j++
				}
				j = min(j, n-1)
			}
			d.section("comment", i, j)
			i = j + 1
		case strings.HasPrefix(strings.TrimLeft(s, " "), "#") && headingRe.MatchString(strings.TrimLeft(s, " ")):
			d.heading(i, i, s)
			d.section("heading", i, i)
			d.inline(i, i)
			i++
		case thematicRe.MatchString(s):
			d.section("thematicBreak", i, i)
			i++
		case quoteRe.MatchString(s):
			j := i
			for j+1 < n && quoteRe.MatchString(L[j+1]) {
				j++
			}
			typ := "blockquote"
			if calloutRe.MatchString(s) {
				typ = "callout"
			}
			d.section(typ, i, j)
			d.quoteLists(i, j)
			d.inline(i, j)
			i = j + 1
		case strings.Contains(s, "|") && i+1 < n && tableSepRe.MatchString(L[i+1]) && strings.Contains(L[i+1], "-"):
			j := i + 1
			for j+1 < n && !blank(L[j+1]) && strings.Contains(L[j+1], "|") {
				j++
			}
			d.section("table", i, j)
			d.inline(i, j)
			i = j + 1
		case listRe.MatchString(s):
			j := d.list(i, n, 0, "")
			d.section("list", i, j)
			d.inline(i, j)
			i = j + 1
		case htmlRe.MatchString(s):
			j := i
			for j+1 < n && !blank(L[j+1]) {
				j++
			}
			d.section("html", i, j)
			i = j + 1
		case footRe.MatchString(s):
			j := i
			for j+1 < n && !blank(L[j+1]) && (strings.HasPrefix(L[j+1], " ") || strings.HasPrefix(L[j+1], "\t") || !startsBlock(L[j+1])) {
				j++
			}
			d.section("footnoteDefinition", i, j)
			d.inline(i, j)
			i = j + 1
		default:
			j := i
			for j+1 < n && !blank(L[j+1]) && !startsBlock(L[j+1]) && !setextRe.MatchString(L[j+1]) && !isCommentStart(L[j+1]) {
				j++
			}
			if j+1 < n && setextRe.MatchString(L[j+1]) {
				level := 1
				if strings.Contains(L[j+1], "-") {
					level = 2
				}
				text := strings.TrimSpace(strings.Join(L[i:j+1], " "))
				d.m.Headings = append(d.m.Headings, HeadingCache{Heading: text, Level: level, Position: d.lineSpan(i, j+1)})
				d.section("heading", i, j+1)
				d.inline(i, j)
				i = j + 2
				continue
			}
			sec := SectionCache{Type: "paragraph", Position: d.lineSpan(i, j)}
			if m := blockIDRe.FindStringSubmatch(L[j]); m != nil {
				sec.ID = m[1]
			}
			d.m.Sections = append(d.m.Sections, sec)
			d.inline(i, j)
			i = j + 1
		}
	}
}

func (d *doc) heading(l1, l2 int, s string) {
	t := strings.TrimLeft(s, " ")
	m := headingRe.FindStringSubmatch(t)
	d.m.Headings = append(d.m.Headings, HeadingCache{Heading: strings.TrimSpace(m[2]), Level: len(m[1]), Position: d.lineSpan(l1, l2)})
}

// list parses a list starting at line i (lines may carry a quote prefix of
// prefixLen bytes) and returns its last line. Items are appended to ListItems.
func (d *doc) list(i, n, prefixLen int, quote string) int {
	L := d.lines
	body := func(k int) string {
		if quote == "" {
			return L[k]
		}
		return strings.TrimPrefix(L[k], quotePfxRe.FindString(L[k]))
	}
	pfx := func(k int) int { return len(L[k]) - len(body(k)) }
	start := i
	family := listFamily(listRe.FindStringSubmatch(body(i))[2])
	type open struct{ indent, line, idx int }
	var stack []open
	last := i
	cur := -1 // index into ListItems of the item receiving continuation lines
	for k := i; k < n; k++ {
		if quote != "" && !quoteRe.MatchString(L[k]) {
			break
		}
		s := body(k)
		if blank(s) {
			// a blank line continues the list only if more list content follows
			nx := k + 1
			for nx < n && blank(body(nx)) && (quote == "" || quoteRe.MatchString(L[nx])) {
				nx++
			}
			if nx >= n || quote != "" && !quoteRe.MatchString(L[nx]) {
				break
			}
			ns := body(nx)
			if m := listRe.FindStringSubmatch(ns); m != nil && (len(m[1]) > 0 || listFamily(m[2]) == family) {
				continue
			}
			if strings.HasPrefix(ns, " ") || strings.HasPrefix(ns, "\t") {
				continue
			}
			break
		}
		if m := listRe.FindStringSubmatch(s); m != nil {
			indent := width(m[1])
			if indent == 0 && listFamily(m[2]) != family {
				break
			}
			for len(stack) > 0 && stack[len(stack)-1].indent >= indent {
				stack = stack[:len(stack)-1]
			}
			parent := -start
			if start == 0 {
				parent = -1
			}
			if len(stack) > 0 {
				parent = stack[len(stack)-1].line
			}
			item := ListItemCache{Parent: parent, Position: d.span(k, pfx(k)+len(m[1]), k, len(L[k]))}
			if t := taskRe.FindStringSubmatch(s[len(m[0]):]); t != nil {
				st := t[1]
				item.Task = &st
			}
			if b := blockIDRe.FindStringSubmatch(s); b != nil {
				item.ID = b[1]
			}
			d.m.ListItems = append(d.m.ListItems, item)
			cur = len(d.m.ListItems) - 1
			stack = append(stack, open{indent, k, cur})
			last = k
			continue
		}
		if k > i && (strings.HasPrefix(s, " ") || strings.HasPrefix(s, "\t") || !blank(body(k-1)) && !startsBlock(s)) {
			// continuation of the current item
			if cur >= 0 {
				it := &d.m.ListItems[cur]
				it.Position.End = d.loc(k, len(L[k]))
				if b := blockIDRe.FindStringSubmatch(s); b != nil && it.ID == "" {
					it.ID = b[1]
				}
			}
			last = k
			continue
		}
		break
	}
	return last
}

// quoteLists finds lists inside a blockquote/callout section.
func (d *doc) quoteLists(i, j int) {
	for k := i; k <= j; k++ {
		inner := strings.TrimPrefix(d.lines[k], quotePfxRe.FindString(d.lines[k]))
		if listRe.MatchString(inner) {
			k = d.list(k, j+1, 0, ">")
		}
	}
}

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

// inline extracts links, embeds and tags from lines l1..l2, skipping code
// spans, math and %% comments.
func (d *doc) inline(l1, l2 int) {
	inComment := false
	for k := l1; k <= l2; k++ {
		masked := d.mask(d.lines[k], &inComment)
		for _, m := range index.WikiRe.FindAllStringSubmatchIndex(masked, -1) {
			inner := masked[m[4]:m[5]]
			l := index.ParseWikilink(inner, m[3] > m[2])
			link := l.Target
			if l.Subpath != "" {
				link += "#" + l.Subpath
			}
			disp := l.Display
			if disp == "" {
				switch {
				case l.Target == "":
					disp = l.Subpath
				case l.Subpath != "":
					disp = l.Target + " > " + l.Subpath
				default:
					disp = l.Target
				}
			}
			c := LinkCache{Link: link, Original: masked[m[0]:m[1]], DisplayText: disp, Position: d.span(k, m[0], k, m[1])}
			d.addLink(c, l.Embed)
		}
		for _, m := range index.MdLinkRe.FindAllStringSubmatchIndex(masked, -1) {
			if m[0] > 0 && masked[m[0]-1] == '[' {
				continue
			}
			tgt := strings.TrimSuffix(strings.TrimPrefix(masked[m[6]:m[7]], "<"), ">")
			if index.SchemeRe.MatchString(tgt) || tgt == "" || strings.HasPrefix(tgt, "#") {
				continue
			}
			if dec, err := url.PathUnescape(tgt); err == nil {
				tgt = dec
			}
			c := LinkCache{Link: tgt, Original: masked[m[0]:m[1]], DisplayText: masked[m[4]:m[5]], Position: d.span(k, m[0], k, m[1])}
			d.addLink(c, m[3] > m[2])
		}
		for _, m := range index.TagRe.FindAllStringSubmatchIndex(masked, -1) {
			tag := masked[m[2]:m[3]]
			if t := index.TagsInLine("#" + tag); len(t) == 1 {
				startCol := m[2] - 1
				d.m.Tags = append(d.m.Tags, TagCache{Tag: t[0], Position: d.span(k, startCol, k, startCol+len(t[0]))})
			}
		}
	}
}

func (d *doc) addLink(c LinkCache, embed bool) {
	if embed {
		d.m.Embeds = append(d.m.Embeds, c)
	} else {
		d.m.Links = append(d.m.Links, c)
	}
}

// mask blanks code spans, math and %% comments (byte-length preserving).
func (d *doc) mask(line string, inComment *bool) string {
	b := []byte(line)
	pos := 0
	for _, seg := range mdscan.Inline(line) {
		end := pos + len(seg.Text)
		if seg.Code {
			blankOut(b[pos:end])
		} else {
			for p := pos; p < end; {
				if strings.HasPrefix(line[p:end], "%%") {
					blankOut(b[p : p+2])
					*inComment = !*inComment
					p += 2
					continue
				}
				if *inComment {
					_, size := utf8.DecodeRuneInString(line[p:])
					blankOut(b[p : p+size])
					p += size
					continue
				}
				p++
			}
		}
		pos = end
	}
	return string(b)
}

func blankOut(b []byte) {
	for i := range b {
		b[i] = ' '
	}
}

// frontmatter converts YAML to JSON with Obsidian's scalar typing (timestamps
// stay strings) and key order preserved, and collects frontmatter links.
func (d *doc) frontmatter(src []byte) {
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil || len(root.Content) == 0 {
		d.m.Frontmatter = json.RawMessage("{}")
		return
	}
	var buf bytes.Buffer
	d.yamlJSON(&buf, root.Content[0], "")
	d.m.Frontmatter = buf.Bytes()
}

var fmLinkRe = regexp.MustCompile(`^\s*\[\[([^\[\]]+)\]\]\s*$`)

func (d *doc) yamlJSON(buf *bytes.Buffer, n *yaml.Node, key string) {
	if n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	switch n.Kind {
	case yaml.MappingNode:
		buf.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				buf.WriteByte(',')
			}
			k := n.Content[i].Value
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			d.yamlJSON(buf, n.Content[i+1], join(key, k))
		}
		buf.WriteByte('}')
	case yaml.SequenceNode:
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			d.yamlJSON(buf, c, join(key, strconv.Itoa(i)))
		}
		buf.WriteByte(']')
	case yaml.ScalarNode:
		switch n.ShortTag() {
		case "!!null":
			buf.WriteString("null")
		case "!!bool":
			var b bool
			_ = n.Decode(&b)
			fmt.Fprint(buf, b)
		case "!!int", "!!float":
			var f float64
			if err := n.Decode(&f); err == nil {
				b, _ := json.Marshal(f)
				buf.Write(b)
			} else {
				b, _ := json.Marshal(n.Value)
				buf.Write(b)
			}
		default:
			b, _ := json.Marshal(n.Value)
			buf.Write(b)
			if m := fmLinkRe.FindStringSubmatch(n.Value); m != nil {
				l := index.ParseWikilink(m[1], false)
				link := l.Target
				if l.Subpath != "" {
					link += "#" + l.Subpath
				}
				disp := l.Display
				if disp == "" {
					disp = l.Target
				}
				d.m.FrontmatterLinks = append(d.m.FrontmatterLinks, FrontmatterLink{Key: key, Link: link, Original: n.Value, DisplayText: disp})
			}
		}
	default:
		buf.WriteString("null")
	}
}

func join(a, b string) string {
	if a == "" {
		return b
	}
	return a + "." + b
}
