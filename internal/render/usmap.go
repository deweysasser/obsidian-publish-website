package render

import (
	"fmt"
	"html"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/usmap"
)

// usmapBlock renders a ```usmap block: a map of U.S. states and territories
// where each one with a published folder or note (by name, alias or postal
// code) inside the named folder links to it.
func (r *Renderer) usmapBlock(code string, line int, ctx Context) (string, error) {
	arg := strings.TrimSpace(code)
	folder, ok := r.mapFolder(arg, ctx)
	if !ok {
		r.Warn(ctx.Path, line, fmt.Sprintf("usmap: no published folder %q", arg))
	}
	targets := r.children(folder)
	m := usmap.Load()
	var b strings.Builder
	b.WriteString(`<figure class="usmap"><svg viewBox="0 0 ` + num(m.W) + " " + num(m.H) +
		`" role="group" aria-label="Map of U.S. states and territories">` + "\n")
	for i := range m.Regions {
		reg := &m.Regions[i]
		open, end := regionLink(reg, targets)
		b.WriteString(open + `<path d="` + reg.D + `"/>`)
		if bx := reg.Box; bx != nil {
			cls, ty := "sq", bx.Y+bx.H/2
			if bx.Frame {
				cls, ty = "inset", bx.Y+bx.H-7
			}
			b.WriteString(`<rect class="` + cls + `" x="` + num(bx.X) + `" y="` + num(bx.Y) + `" width="` + num(bx.W) + `" height="` + num(bx.H) +
				`" rx="3"/><text x="` + num(bx.X+bx.W/2) + `" y="` + num(ty) + `">` + reg.Code + `</text>`)
		}
		b.WriteString(end + "\n")
	}
	b.WriteString(r.townDots(m, targets))
	b.WriteString("</svg></figure>\n")
	return "\n" + b.String(), nil
}

// townDots draws a dot for each placeable town in each linked state's
// folder, as decoration: the state underneath stays the link.
func (r *Renderer) townDots(m *usmap.Map, targets map[string]child) string {
	var b strings.Builder
	for i := range m.Regions {
		reg := &m.Regions[i]
		for _, n := range reg.Names() {
			st, ok := targets[strings.ToLower(n)]
			if !ok {
				continue
			}
			for _, town := range r.children(st.folder) {
				if lat, lon, ok := r.townLocation(town, reg.Code); ok {
					x, y := reg.Proj.Point(lon, lat)
					b.WriteString(`<circle cx="` + num1(x) + `" cy="` + num1(y) + `" r="2"/>`)
				}
			}
			break
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return `<g class="towns" aria-hidden="true">` + b.String() + "</g>\n"
}

// regionLink opens a region's element: a link to its page if it has one,
// otherwise a greyed group. Both carry the name as a tooltip.
func regionLink(reg *usmap.Region, targets map[string]child) (open, end string) {
	name := html.EscapeString(reg.Name)
	for _, n := range reg.Names() {
		if c, ok := targets[strings.ToLower(n)]; ok {
			return `<a href="` + html.EscapeString(c.url) + `"><title>` + name + `</title>`, "</a>"
		}
	}
	return `<g class="off"><title>` + name + `</title>`, "</g>"
}

// statemapBlock renders a ```statemap block: one state (the note's folder,
// or the folder named in the block) with a dot for each town in it, placed
// by the town page's "coordinates" frontmatter or the Census gazetteer.
func (r *Renderer) statemapBlock(code string, line int, ctx Context) (string, error) {
	arg := strings.TrimSpace(code)
	folder, ok := r.mapFolder(arg, ctx)
	m := usmap.Load()
	var reg *usmap.Region
	if ok {
		reg = m.Find(path.Base(folder))
	}
	if reg == nil {
		r.Warn(ctx.Path, line, fmt.Sprintf("statemap: %q is not a published state folder", path.Join(path.Dir(ctx.Path), arg)))
		return "", nil
	}

	// Frame the state with a margin, and draw the neighbours that show.
	bb := reg.BBox
	pad := math.Max(math.Max(bb[2]-bb[0], bb[3]-bb[1])*0.06, 2)
	vb := [4]float64{bb[0] - pad, bb[1] - pad, bb[2] - bb[0] + 2*pad, bb[3] - bb[1] + 2*pad}
	k := math.Max(vb[2], vb[3]) / 100 // 1% of the map's larger side
	var b strings.Builder
	b.WriteString(`<figure class="statemap"><svg viewBox="` + num1(vb[0]) + " " + num1(vb[1]) + " " + num1(vb[2]) + " " + num1(vb[3]) +
		`" role="group" aria-label="` + html.EscapeString("Map of "+reg.Name) + `">` + "\n")
	siblings := r.children(path.Dir(folder))
	for i := range m.Regions {
		nb := &m.Regions[i]
		o := nb.BBox
		if nb == reg || nb.Group != reg.Group || o[2] < vb[0] || o[0] > vb[0]+vb[2] || o[3] < vb[1] || o[1] > vb[1]+vb[3] {
			continue
		}
		open, end := regionLink(nb, siblings)
		b.WriteString(open + `<path class="nb" d="` + nb.D + `"/>` + end + "\n")
	}
	b.WriteString(`<path class="st" d="` + reg.D + `"/>` + "\n")

	type dot struct {
		name, url string
		x, y      float64
	}
	var dots []dot
	for _, c := range r.children(folder) {
		lat, lon, ok := r.townLocation(c, reg.Code)
		if !ok {
			r.Warn(ctx.Path, line, fmt.Sprintf("statemap: no location for %q (add coordinates: [lat, lon] to its page)", c.name))
			continue
		}
		x, y := reg.Proj.Point(lon, lat)
		dots = append(dots, dot{c.name, c.url, x, y})
	}
	sort.Slice(dots, func(i, j int) bool { return dots[i].name < dots[j].name })

	// Label each dot where its label overlaps no other label or dot; a
	// crowded dot keeps only its tooltip.
	type rect struct{ x0, y0, x1, y1 float64 }
	rd, fs := 0.9*k, 2.4*k
	var taken []rect
	for _, d := range dots {
		taken = append(taken, rect{d.x - rd, d.y - rd, d.x + rd, d.y + rd})
	}
	inside := func(q rect) bool {
		return q.x0 >= vb[0] && q.y0 >= vb[1] && q.x1 <= vb[0]+vb[2] && q.y1 <= vb[1]+vb[3]
	}
	free := func(q rect, self int) bool {
		for i, t := range taken {
			if i != self && q.x0 < t.x1 && t.x0 < q.x1 && q.y0 < t.y1 && t.y0 < q.y1 {
				return false
			}
		}
		return inside(q)
	}
	for i, d := range dots {
		b.WriteString(`<a href="` + html.EscapeString(d.url) + `"><title>` + html.EscapeString(d.name) + `</title><circle cx="` +
			num1(d.x) + `" cy="` + num1(d.y) + `" r="` + num1(rd) + `"/>`)
		w := float64(len([]rune(d.name))) * fs * 0.56
		gap := rd + 0.4*k
		for _, c := range []struct {
			anchor string
			tx     float64
			q      rect
		}{
			{"start", d.x + gap, rect{d.x + gap, d.y - fs/2, d.x + gap + w, d.y + fs/2}},
			{"end", d.x - gap, rect{d.x - gap - w, d.y - fs/2, d.x - gap, d.y + fs/2}},
			{"middle", d.x, rect{d.x - w/2, d.y - gap - fs, d.x + w/2, d.y - gap}},
			{"middle", d.x, rect{d.x - w/2, d.y + gap, d.x + w/2, d.y + gap + fs}},
			{"start", d.x + rd, rect{d.x + rd, d.y - rd - fs, d.x + rd + w, d.y - rd}},
			{"start", d.x + rd, rect{d.x + rd, d.y + rd, d.x + rd + w, d.y + rd + fs}},
			{"end", d.x - rd, rect{d.x - rd - w, d.y - rd - fs, d.x - rd, d.y - rd}},
			{"end", d.x - rd, rect{d.x - rd - w, d.y + rd, d.x - rd, d.y + rd + fs}},
		} {
			if free(c.q, i) {
				taken = append(taken, c.q)
				b.WriteString(`<text x="` + num1(c.tx) + `" y="` + num1((c.q.y0+c.q.y1)/2) + `" text-anchor="` + c.anchor +
					`" font-size="` + num1(fs) + `">` + html.EscapeString(d.name) + `</text>`)
				break
			}
		}
		b.WriteString("</a>\n")
	}
	b.WriteString("</svg></figure>\n")
	return "\n" + b.String(), nil
}

// townLocation reads a town page's "coordinates" frontmatter ([lat, lon]
// or "lat, lon"), falling back to the gazetteer.
func (r *Renderer) townLocation(c child, state string) (lat, lon float64, ok bool) {
	if pg := r.Ix.Pages[c.note]; pg != nil {
		var parts []string
		switch v := pg.Note.Frontmatter["coordinates"].(type) {
		case []any:
			for _, p := range v {
				parts = append(parts, fmt.Sprint(p))
			}
		case string:
			parts = strings.Split(v, ",")
		}
		if len(parts) == 2 {
			la, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
			lo, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
			if err1 == nil && err2 == nil {
				return la, lo, true
			}
		}
	}
	return usmap.Locate(state, c.name)
}

// child is a published subfolder or note of a folder.
type child struct {
	name, url string
	note      string // its page's note (the folder note for a folder), if any
	folder    string // its vault path, if it is a folder
}

// children maps the lower-cased names of a published folder's subfolders
// and notes to their pages; a folder wins over a note of the same name.
func (r *Renderer) children(folder string) map[string]child {
	out := map[string]child{}
	if folder == "" {
		return out
	}
	for f, slug := range r.Pub.Folders {
		if path.Dir(f) == folder && f != folder {
			out[strings.ToLower(path.Base(f))] = child{path.Base(f), r.URLs.PageURL(slug), r.Pub.FolderNote(f), f}
		}
	}
	for p, slug := range r.Pub.Pages {
		if path.Dir(p) == folder && slug != "" && r.Pub.FolderNote(folder) != p {
			name := strings.TrimSuffix(path.Base(p), path.Ext(p))
			if _, isFolder := out[strings.ToLower(name)]; !isFolder {
				out[strings.ToLower(name)] = child{name, r.URLs.PageURL(slug), p, ""}
			}
		}
	}
	return out
}

// mapFolder finds the published folder an argument names: relative to the
// note's folder, then to the published root, then to the vault. Empty means
// the note's own folder.
func (r *Renderer) mapFolder(arg string, ctx Context) (string, bool) {
	for _, f := range []string{path.Join(path.Dir(ctx.Path), arg), path.Join(r.Pub.Root, arg), path.Clean(arg)} {
		if _, ok := r.Pub.Folders[f]; ok {
			return f, true
		}
	}
	return "", false
}

func num(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

// num1 rounds to one decimal.
func num1(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }
