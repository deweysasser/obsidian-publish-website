package render

import (
	"net/url"

	"github.com/deweysasser/obsidian-publish-website/internal/youtube"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/util"
)

// imageRenderer renders images; a YouTube URL in image syntax (Obsidian's
// video embed) becomes a click-to-play player, or a thumbnail linking to
// YouTube when the owner does not allow embedding.
type imageRenderer struct{ r *Renderer }

func (ir imageRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindImage, ir.render)
}

func (ir imageRenderer) render(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	alt := util.EscapeHTML([]byte(plainText(n, src)))
	if u, err := url.Parse(string(n.Destination)); err == nil {
		if id := youtube.ID(u); id != "" {
			class := "yt"
			if !ir.r.embeddable(id) {
				class = "yt noembed"
			}
			w.WriteString(`<span class="` + class + `" data-yt="` + id + `"><a href="`)
			w.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
			w.WriteString(`"><img src="https://i.ytimg.com/vi/` + id + `/hqdefault.jpg" alt="`)
			w.Write(alt)
			w.WriteString(`" loading="lazy"><span class="yt-play" aria-hidden="true"></span></a></span>`)
			return ast.WalkSkipChildren, nil
		}
	}
	w.WriteString(`<img src="`)
	w.Write(util.EscapeHTML(util.URLEscape(n.Destination, true)))
	w.WriteString(`" alt="`)
	w.Write(alt)
	w.WriteByte('"')
	if n.Title != nil {
		w.WriteString(` title="`)
		w.Write(util.EscapeHTML(n.Title))
		w.WriteByte('"')
	}
	w.WriteByte('>')
	return ast.WalkSkipChildren, nil
}

func (r *Renderer) embeddable(id string) bool {
	return r.YouTubeEmbeddable == nil || r.YouTubeEmbeddable(id)
}
