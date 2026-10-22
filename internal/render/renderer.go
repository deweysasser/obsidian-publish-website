// Package render turns Obsidian-flavored markdown into target-flavored
// markdown (CommonMark + GFM with raw-HTML islands) and HTML.
package render

import (
	"github.com/yuin/goldmark"

	"github.com/deweysasser/obsidian-publish-website/internal/index"
	"github.com/deweysasser/obsidian-publish-website/internal/resolve"
	"github.com/deweysasser/obsidian-publish-website/internal/selector"
	"github.com/deweysasser/obsidian-publish-website/internal/urls"
)

// Mode selects the output flavor.
type Mode int

const (
	// ModeHTML produces markdown destined for goldmark in this program.
	ModeHTML Mode = iota
	// ModeMarkdown produces markdown destined for another generator (Hugo, Jekyll).
	ModeMarkdown
)

// Context carries per-note state through recursive rendering (embeds, callouts).
type Context struct {
	Path  string   // note whose content is being rendered; links resolve relative to it
	Depth int      // embed depth
	Stack []string // embed stack, for cycle detection
}

// FenceHook renders the content of a fenced block with a given language.
type FenceHook func(code string, line int, ctx Context) (string, error)

// Renderer renders published notes.
type Renderer struct {
	Ix   *index.Index
	Pub  *selector.Published
	Res  *resolve.Resolver
	URLs urls.Scheme
	Mode Mode
	Warn func(path string, line int, msg string)

	MaxEmbedDepth int // 0 = DefaultMaxEmbedDepth
	// StrictLineBreaks mirrors Obsidian's setting: when false (the default),
	// single newlines render as line breaks (html target only).
	StrictLineBreaks bool
	// YouTubeEmbeddable reports whether a video's owner allows embedding
	// (nil: assume yes).
	YouTubeEmbeddable func(id string) bool

	fenceHooks map[string]FenceHook
	inlineHook func(code string, line int, ctx Context) (string, bool)
	embedHook  func(l index.Link, line int, ctx Context) string
	dv         dvState
	md         goldmark.Markdown
	mdStrict   bool
}

// NewRenderer builds a renderer. warn receives every non-fatal problem.
func NewRenderer(ix *index.Index, pub *selector.Published, u urls.Scheme, mode Mode, warn func(string, int, string)) *Renderer {
	if warn == nil {
		warn = func(string, int, string) {}
	}
	r := &Renderer{
		Ix: ix, Pub: pub, URLs: u, Mode: mode, Warn: warn,
		Res:        &resolve.Resolver{Ix: ix, Pub: pub, URLs: u},
		fenceHooks: map[string]FenceHook{},
	}
	r.embedHook = r.embed
	r.fenceHooks["folder-index-content"] = r.folderIndexHook
	r.fenceHooks["dataview"] = r.dvBlock
	r.fenceHooks["dataviewjs"] = r.dvJS
	r.fenceHooks["transcript"] = r.transcriptBlock
	r.inlineHook = r.dvInline
	return r
}
