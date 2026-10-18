package render

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/resolve"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	ghtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// TOCEntry is one heading in a page's table of contents.
type TOCEntry struct {
	Level int
	Text  string
	ID    string
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM, extension.Footnote),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(
		ghtml.WithUnsafe(),
		renderer.WithNodeRenderers(util.Prioritized(&codeRenderer{}, 100)),
	),
)

// MarkdownToHTML renders preprocessed markdown and returns the HTML and TOC.
func (r *Renderer) MarkdownToHTML(src string) (string, []TOCEntry, error) {
	source := []byte(src)
	pctx := parser.NewContext(parser.WithIDs(&headingIDs{seen: map[string]bool{}}))
	doc := md.Parser().Parse(text.NewReader(source), parser.WithContext(pctx))
	var toc []TOCEntry
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if h, ok := n.(*ast.Heading); ok && entering {
			id, _ := h.AttributeString("id")
			idb, _ := id.([]byte)
			toc = append(toc, TOCEntry{Level: h.Level, Text: plainText(h, source), ID: string(idb)})
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, source, doc); err != nil {
		return "", nil, err
	}
	return buf.String(), toc, nil
}

func plainText(n ast.Node, src []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(src))
			if t.SoftLineBreak() {
				b.WriteByte(' ')
			}
		case *ast.String:
			b.Write(t.Value)
		case *ast.RawHTML:
			return ast.WalkSkipChildren, nil
		}
		return ast.WalkContinue, nil
	})
	return strings.TrimSpace(b.String())
}

// headingIDs generates heading ids with resolve.HeadingID so link anchors
// and rendered ids agree.
type headingIDs struct{ seen map[string]bool }

func (h *headingIDs) Generate(value []byte, kind ast.NodeKind) []byte {
	base := resolve.HeadingID(string(value))
	if base == "" {
		base = "heading"
	}
	id := base
	for i := 1; h.seen[id]; i++ {
		id = base + "-" + strconv.Itoa(i)
	}
	h.seen[id] = true
	return []byte(id)
}

func (h *headingIDs) Put(value []byte) { h.seen[string(value)] = true }

// codeRenderer renders fenced code; mermaid blocks become <pre class="mermaid">.
type codeRenderer struct{}

func (c *codeRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, c.render)
}

func (c *codeRenderer) render(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.FencedCodeBlock)
	lang := string(n.Language(src))
	if lang == "mermaid" {
		w.WriteString(`<pre class="mermaid">`)
	} else if lang != "" {
		w.WriteString(`<pre><code class="language-` + string(util.EscapeHTML([]byte(lang))) + `">`)
	} else {
		w.WriteString("<pre><code>")
	}
	for i := 0; i < n.Lines().Len(); i++ {
		seg := n.Lines().At(i)
		w.Write(util.EscapeHTML(seg.Value(src)))
	}
	if lang == "mermaid" {
		w.WriteString("</pre>\n")
	} else {
		w.WriteString("</code></pre>\n")
	}
	return ast.WalkSkipChildren, nil
}
