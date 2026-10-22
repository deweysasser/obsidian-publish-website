// Command usmap turns simplified Census state boundaries (GeoJSON, lon/lat)
// into the projected SVG paths the usmap block draws. See build.sh.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Region is one state or territory in the generated map.
type Region struct {
	Code    string   `json:"code"`
	Name    string   `json:"name"`
	Aliases []string `json:"aliases,omitempty"`
	D       string   `json:"d"`
	// Box is a clickable labelled square for regions too small to hit
	// (small Northeast states) or drawn in an inset (territories).
	Box *Box `json:"box,omitempty"`
}

// Box is a labelled square; X, Y is its top-left corner.
type Box struct {
	X, Y, W, H float64
	// Frame draws an inset frame around a territory shown at its own scale.
	Frame bool `json:"frame,omitempty"`
}

// Map is the generated file.
type Map struct {
	W, H    float64
	Regions []Region `json:"regions"`
}

const (
	width  = 960
	height = 600
)

// names gives the site name where it differs from the Census name, and the
// other folder names that should match.
var names = map[string][2]string{
	"VI": {"U.S. Virgin Islands", "United States Virgin Islands|US Virgin Islands|Virgin Islands"},
	"MP": {"Northern Mariana Islands", "Commonwealth of the Northern Mariana Islands"},
	"DC": {"District of Columbia", "Washington DC|Washington, D.C."},
}

// small states get a square off the Atlantic coast.
var small = []string{"VT", "NH", "MA", "RI", "CT", "NJ", "DE", "MD", "DC"}

var territories = []string{"PR", "VI", "GU", "MP", "AS"}

type pt struct{ x, y float64 }

type feature struct {
	Properties struct{ STUSPS, NAME string }
	Geometry   struct {
		Type        string
		Coordinates json.RawMessage
	}
}

func main() {
	in := flag.String("in", "", "simplified GeoJSON files, comma-separated")
	out := flag.String("out", "internal/render/usmap.json", "output")
	flag.Parse()
	var fc struct{ Features []feature }
	for _, f := range strings.Split(*in, ",") {
		b, err := os.ReadFile(f)
		if err != nil {
			log.Fatal(err)
		}
		var part struct{ Features []feature }
		if err := json.Unmarshal(b, &part); err != nil {
			log.Fatal(err)
		}
		fc.Features = append(fc.Features, part.Features...)
	}
	polys := map[string][][][]pt{} // code -> polygons -> rings
	names2 := map[string]string{}
	for _, f := range fc.Features {
		var mp [][][][2]float64
		switch f.Geometry.Type {
		case "Polygon":
			var p [][][2]float64
			if err := json.Unmarshal(f.Geometry.Coordinates, &p); err != nil {
				log.Fatal(err)
			}
			mp = [][][][2]float64{p}
		case "MultiPolygon":
			if err := json.Unmarshal(f.Geometry.Coordinates, &mp); err != nil {
				log.Fatal(err)
			}
		default:
			log.Fatalf("%s: %s", f.Properties.STUSPS, f.Geometry.Type)
		}
		c := f.Properties.STUSPS
		names2[c] = f.Properties.NAME
		for _, p := range mp {
			var rings [][]pt
			for _, r := range p {
				var ring []pt
				for _, ll := range r {
					lon := ll[0]
					if lon > 0 && c != "GU" && c != "MP" { // Aleutians across the antimeridian
						lon -= 360
					}
					ring = append(ring, pt{lon, ll[1]})
				}
				rings = append(rings, ring)
			}
			polys[c] = append(polys[c], rings)
		}
	}

	m := Map{W: width, H: height}
	lower48 := albers(-96, 37.5, 29.5, 45.5)
	// Fit the lower 48 into the left part of the frame, leaving room for the
	// small-state squares on the right and the insets below.
	var proj = map[string][][][]pt{}
	for c, ps := range polys {
		if c == "AK" || c == "HI" || isTerritory(c) {
			continue
		}
		proj[c] = project(ps, lower48)
	}
	minx, miny, maxx, maxy := bounds(proj)
	s := math.Min(860/(maxx-minx), 470/(maxy-miny))
	place(proj, s, 10-minx*s, 10-miny*s)
	mainScale := s

	// Alaska at 35% of true scale, Hawaii at true scale, bottom left.
	inset := func(c string, p func(pt) pt, scale, left, bottom float64) {
		r := map[string][][][]pt{c: project(polys[c], p)}
		x0, y0, x1, y1 := bounds(r)
		_ = x1
		place(r, scale, left-x0*scale, bottom-(y1-y0)*scale-y0*scale)
		proj[c] = r[c]
	}
	inset("AK", albers(-154, 50, 55, 65), mainScale*0.35, 10, 590)
	inset("HI", albers(-157, 3, 8, 18), mainScale, 210, 585)

	for c, ps := range proj {
		m.Regions = append(m.Regions, Region{Code: c, D: pathD(ps)})
	}

	// Territories: each fitted into its own framed box along the bottom.
	const tw, th, gap = 70.0, 52.0, 8.0
	for i, c := range territories {
		bx := Box{X: 960 - 10 - float64(len(territories)-i)*(tw+gap) + gap, Y: height - 10 - th, W: tw, H: th, Frame: true}
		// Fit the main islands; outliers (Swains, the northern Marianas)
		// would shrink them to specks.
		r := map[string][][][]pt{c: project(mainIslands(polys[c]), mercator)}
		x0, y0, x1, y1 := bounds(r)
		sc := math.Min((tw-10)/(x1-x0), (th-22)/(y1-y0))
		place(r, sc, bx.X+(tw-(x1-x0)*sc)/2-x0*sc, bx.Y+4+(th-22-(y1-y0)*sc)/2-y0*sc)
		m.Regions = append(m.Regions, Region{Code: c, D: pathD(r[c]), Box: &bx})
	}

	// Small states: squares in a column east of the coast, at the state's
	// latitude where possible, without overlapping.
	byCode := map[string]*Region{}
	for i := range m.Regions {
		byCode[m.Regions[i].Code] = &m.Regions[i]
	}
	const sq, sgap = 20.0, 3.0
	type cy struct {
		c string
		y float64
	}
	var col []cy
	for _, c := range small {
		_, y0, _, y1 := bounds(map[string][][][]pt{c: proj[c]})
		col = append(col, cy{c, (y0+y1)/2 - sq/2})
	}
	sort.Slice(col, func(i, j int) bool { return col[i].y < col[j].y })
	for i := 1; i < len(col); i++ {
		if col[i].y < col[i-1].y+sq+sgap {
			col[i].y = col[i-1].y + sq + sgap
		}
	}
	for _, e := range col {
		byCode[e.c].Box = &Box{X: 790, Y: e.y, W: 36, H: sq}
	}

	for i := range m.Regions {
		r := &m.Regions[i]
		r.Name = names2[r.Code]
		if n, ok := names[r.Code]; ok {
			r.Name = n[0]
			r.Aliases = strings.Split(n[1], "|")
		}
	}
	sort.Slice(m.Regions, func(i, j int) bool { return m.Regions[i].Name < m.Regions[j].Name })
	if len(m.Regions) != 56 {
		log.Fatalf("%d regions", len(m.Regions))
	}
	j, err := json.Marshal(m)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, append(j, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", *out, len(j))
}

func isTerritory(c string) bool { return slices.Contains(territories, c) }

// mainIslands keeps the polygons near the largest one (lon/lat input), so
// outliers (Swains, the northern Marianas) don't shrink the inset to specks.
func mainIslands(ps [][][]pt) [][][]pt {
	area := func(r []pt) float64 {
		a := 0.0
		for i := range r {
			j := (i + 1) % len(r)
			a += r[i].x*r[j].y - r[j].x*r[i].y
		}
		return math.Abs(a / 2)
	}
	center := func(r []pt) pt {
		x0, y0, x1, y1 := bounds(map[string][][][]pt{"": {{r}}})
		return pt{(x0 + x1) / 2, (y0 + y1) / 2}
	}
	var big []pt
	for _, p := range ps {
		if big == nil || area(p[0]) > area(big) {
			big = p[0]
		}
	}
	c := center(big)
	var out [][][]pt
	for _, p := range ps {
		if q := center(p[0]); math.Hypot(q.x-c.x, q.y-c.y) < 0.8 {
			out = append(out, p)
		}
	}
	return out
}

// albers returns a conic equal-area projection (unscaled, y up).
func albers(lon0, lat0, lat1, lat2 float64) func(pt) pt {
	rad := math.Pi / 180
	n := (math.Sin(lat1*rad) + math.Sin(lat2*rad)) / 2
	c := math.Cos(lat1*rad)*math.Cos(lat1*rad) + 2*n*math.Sin(lat1*rad)
	rho0 := math.Sqrt(c-2*n*math.Sin(lat0*rad)) / n
	return func(p pt) pt {
		rho := math.Sqrt(c-2*n*math.Sin(p.y*rad)) / n
		th := n * (p.x - lon0) * rad
		return pt{rho * math.Sin(th), rho0 - rho*math.Cos(th)}
	}
}

func mercator(p pt) pt {
	rad := math.Pi / 180
	return pt{p.x * rad, math.Log(math.Tan(math.Pi/4 + p.y*rad/2))}
}

// project applies f and flips y so it grows downward, as in SVG.
func project(ps [][][]pt, f func(pt) pt) [][][]pt {
	var out [][][]pt
	for _, p := range ps {
		var rings [][]pt
		for _, r := range p {
			var ring []pt
			for _, q := range r {
				q = f(q)
				ring = append(ring, pt{q.x, -q.y})
			}
			rings = append(rings, ring)
		}
		out = append(out, rings)
	}
	return out
}

func bounds(m map[string][][][]pt) (minx, miny, maxx, maxy float64) {
	minx, miny, maxx, maxy = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, ps := range m {
		for _, p := range ps {
			for _, r := range p {
				for _, q := range r {
					minx, maxx = math.Min(minx, q.x), math.Max(maxx, q.x)
					miny, maxy = math.Min(miny, q.y), math.Max(maxy, q.y)
				}
			}
		}
	}
	return
}

func place(m map[string][][][]pt, s, dx, dy float64) {
	for _, ps := range m {
		for _, p := range ps {
			for _, r := range p {
				for i, q := range r {
					r[i] = pt{q.x*s + dx, q.y*s + dy}
				}
			}
		}
	}
}

// pathD writes rings as compact SVG path data: absolute start, relative
// steps between coordinates rounded to 0.1, dropping repeated points.
func pathD(ps [][][]pt) string {
	var b strings.Builder
	for _, p := range ps {
		for _, r := range p {
			var px, py int
			first := true
			n := 0
			for _, q := range r {
				x, y := int(math.Round(q.x*10)), int(math.Round(q.y*10))
				if first {
					b.WriteString("M" + num(x) + " " + num(y))
					first = false
				} else if x != px || y != py {
					if n == 0 {
						b.WriteString("l")
					} else {
						b.WriteString(" ")
					}
					b.WriteString(num(x-px) + " " + num(y-py))
					n++
				} else {
					continue
				}
				px, py = x, y
			}
			b.WriteString("z")
		}
	}
	return b.String()
}

// num formats tenths compactly: 12 -> "1.2", -5 -> "-.5", 30 -> "3".
func num(t int) string {
	neg := t < 0
	if neg {
		t = -t
	}
	s := strconv.Itoa(t / 10)
	if f := t % 10; f != 0 {
		if s == "0" {
			s = ""
		}
		s += "." + strconv.Itoa(f)
	}
	if neg {
		s = "-" + s
	}
	return s
}
