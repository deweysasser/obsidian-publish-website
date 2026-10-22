// Command usmap generates internal/usmap's embedded data: projected SVG
// paths for every state and territory (from simplified Census boundaries,
// GeoJSON) and a place-name lookup (from Census gazetteer files). See
// build.sh.
package main

import (
	"bufio"
	"compress/gzip"
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
	"time"

	"github.com/deweysasser/obsidian-publish-website/internal/usmap"
)

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

// shape is one region's polygons (rings of lon/lat) before projection.
type shape [][][]pt

type feature struct {
	Properties struct{ STUSPS, NAME string }
	Geometry   struct {
		Type        string
		Coordinates json.RawMessage
	}
}

func main() {
	in := flag.String("in", "", "simplified GeoJSON files, comma-separated")
	out := flag.String("out", "internal/usmap/usmap.json", "map output")
	gaz := flag.String("gazetteer", "", "Census gazetteer files (place, cousubs, counties), comma-separated")
	placesOut := flag.String("places", "internal/usmap/places.tsv.gz", "place lookup output")
	flag.Parse()
	if *in != "" {
		writeMap(strings.Split(*in, ","), *out)
	}
	if *gaz != "" {
		writePlaces(strings.Split(*gaz, ","), *placesOut)
	}
}

func readShapes(files []string) (map[string]shape, map[string]string) {
	shapes, census := map[string]shape{}, map[string]string{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			log.Fatal(err)
		}
		var fc struct{ Features []feature }
		if err := json.Unmarshal(b, &fc); err != nil {
			log.Fatal(err)
		}
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
			census[c] = f.Properties.NAME
			for _, p := range mp {
				var rings [][]pt
				for _, r := range p {
					var ring []pt
					for _, ll := range r {
						ring = append(ring, pt{ll[0], ll[1]})
					}
					rings = append(rings, ring)
				}
				shapes[c] = append(shapes[c], rings)
			}
		}
	}
	return shapes, census
}

func writeMap(files []string, out string) {
	shapes, census := readShapes(files)
	m := usmap.Map{W: width, H: height}
	add := func(c, group string, p usmap.Projection, s shape) *usmap.Region {
		m.Regions = append(m.Regions, usmap.Region{Code: c, Group: group, Proj: p})
		r := &m.Regions[len(m.Regions)-1]
		r.D, r.BBox = pathD(s, p)
		return r
	}

	// The lower 48 fit the left part of the frame, leaving room for the
	// small-state squares on the right and the insets below.
	lower48 := usmap.Projection{Kind: "albers", Lon0: -96, Lat0: 37.5, Lat1: 29.5, Lat2: 45.5}
	var main []string
	for c := range shapes {
		if c != "AK" && c != "HI" && !slices.Contains(territories, c) {
			main = append(main, c)
		}
	}
	sort.Strings(main)
	x0, y0, x1, y1 := rawBounds(lower48, shapes, main...)
	s := math.Min(860/(x1-x0), 470/(y1-y0))
	lower48.S, lower48.DX, lower48.DY = s, 10-x0*s, 10+y1*s
	for _, c := range main {
		add(c, "lower48", lower48, shapes[c])
	}

	// Alaska at 35% of true scale, Hawaii at true scale, bottom left.
	inset := func(c string, p usmap.Projection, scale, left, bottom float64) {
		x0, y0, _, _ := rawBounds(p, shapes, c)
		p.S, p.DX, p.DY = scale, left-x0*scale, bottom+y0*scale
		add(c, c, p, shapes[c])
	}
	inset("AK", usmap.Projection{Kind: "albers", Lon0: -154, Lat0: 50, Lat1: 55, Lat2: 65}, s*0.35, 10, 590)
	inset("HI", usmap.Projection{Kind: "albers", Lon0: -157, Lat0: 3, Lat1: 8, Lat2: 18}, s, 210, 585)

	// Territories: each fitted into its own framed box along the bottom.
	const tw, th, gap = 70.0, 52.0, 8.0
	for i, c := range territories {
		bx := usmap.Box{X: width - 10 - float64(len(territories)-i)*(tw+gap) + gap, Y: height - 10 - th, W: tw, H: th, Frame: true}
		sh := mainIslands(shapes[c])
		p := usmap.Projection{Kind: "mercator"}
		x0, y0, x1, y1 := rawBounds(p, map[string]shape{c: sh}, c)
		p.S = math.Min((tw-10)/(x1-x0), (th-22)/(y1-y0))
		p.DX = bx.X + (tw-(x1-x0)*p.S)/2 - x0*p.S
		p.DY = bx.Y + 4 + (th-22-(y1-y0)*p.S)/2 + y1*p.S
		add(c, c, p, sh).Box = &bx
	}

	// Small states: squares in a column east of the coast, at the state's
	// latitude where possible, without overlapping.
	const sq, sgap = 20.0, 3.0
	type cy struct {
		r *usmap.Region
		y float64
	}
	var col []cy
	for i := range m.Regions {
		r := &m.Regions[i]
		if slices.Contains(small, r.Code) {
			col = append(col, cy{r, (r.BBox[1]+r.BBox[3])/2 - sq/2})
		}
	}
	sort.Slice(col, func(i, j int) bool { return col[i].y < col[j].y })
	for i := 1; i < len(col); i++ {
		col[i].y = math.Max(col[i].y, col[i-1].y+sq+sgap)
	}
	for _, e := range col {
		e.r.Box = &usmap.Box{X: 790, Y: e.y, W: 36, H: sq}
	}

	for i := range m.Regions {
		r := &m.Regions[i]
		r.Name = census[r.Code]
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
	if err := os.WriteFile(out, append(j, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", out, len(j))
}

// rawBounds is the extent of the named shapes under p's raw projection,
// y up.
func rawBounds(p usmap.Projection, shapes map[string]shape, codes ...string) (x0, y0, x1, y1 float64) {
	x0, y0, x1, y1 = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, c := range codes {
		for _, poly := range shapes[c] {
			for _, r := range poly {
				for _, q := range r {
					x, y := p.Raw(q.x, q.y)
					x0, x1 = math.Min(x0, x), math.Max(x1, x)
					y0, y1 = math.Min(y0, y), math.Max(y1, y)
				}
			}
		}
	}
	return
}

// mainIslands keeps the polygons near the largest one, so outliers (Swains,
// the northern Marianas) don't shrink an inset to specks.
func mainIslands(s shape) shape {
	area := func(r []pt) float64 {
		a := 0.0
		for i := range r {
			j := (i + 1) % len(r)
			a += r[i].x*r[j].y - r[j].x*r[i].y
		}
		return math.Abs(a / 2)
	}
	center := func(r []pt) pt {
		x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for _, q := range r {
			x0, x1 = math.Min(x0, q.x), math.Max(x1, q.x)
			y0, y1 = math.Min(y0, q.y), math.Max(y1, q.y)
		}
		return pt{(x0 + x1) / 2, (y0 + y1) / 2}
	}
	var big []pt
	for _, p := range s {
		if big == nil || area(p[0]) > area(big) {
			big = p[0]
		}
	}
	c := center(big)
	var out shape
	for _, p := range s {
		if q := center(p[0]); math.Hypot(q.x-c.x, q.y-c.y) < 0.8 {
			out = append(out, p)
		}
	}
	return out
}

// pathD projects rings into compact SVG path data (absolute start, relative
// steps between coordinates rounded to 0.1, repeated points dropped) and
// returns the extent.
func pathD(s shape, p usmap.Projection) (string, [4]float64) {
	var b strings.Builder
	bb := [4]float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, poly := range s {
		for _, r := range poly {
			var px, py int
			first, n := true, 0
			for _, q := range r {
				fx, fy := p.Point(q.x, q.y)
				bb = [4]float64{math.Min(bb[0], fx), math.Min(bb[1], fy), math.Max(bb[2], fx), math.Max(bb[3], fy)}
				x, y := int(math.Round(fx*10)), int(math.Round(fy*10))
				switch {
				case first:
					b.WriteString("M" + num(x) + " " + num(y))
					first = false
				case x == px && y == py:
					continue
				case n == 0:
					b.WriteString("l" + num(x-px) + " " + num(y-py))
					n++
				default:
					b.WriteString(" " + num(x-px) + " " + num(y-py))
				}
				px, py = x, y
			}
			b.WriteString("z")
		}
	}
	for i := range bb {
		bb[i] = math.Round(bb[i]*10) / 10
	}
	return b.String(), bb
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

// newEngland states govern through towns (county subdivisions), not places.
var newEngland = []string{"CT", "ME", "MA", "NH", "RI", "VT"}

type place struct {
	rank     int
	lat, lon string
}

// writePlaces reduces the gazetteer files to one entry per state and
// lower-cased name, keeping the likeliest local government: New England
// towns, then incorporated places, counties, other towns, statistical places
// (CDPs) and defunct ones.
func writePlaces(files []string, out string) {
	best := map[string]place{}
	put := func(st, name string, rank int, lat, lon string) {
		k := st + "\t" + strings.ToLower(name)
		if p, ok := best[k]; !ok || rank < p.rank {
			best[k] = place{rank, lat, lon}
		}
	}
	for _, file := range files {
		f, err := os.Open(file)
		if err != nil {
			log.Fatal(err)
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(nil, 1<<20)
		var col map[string]int
		for sc.Scan() {
			fs := strings.Split(sc.Text(), "\t")
			for i := range fs {
				fs[i] = strings.TrimSpace(fs[i])
			}
			if col == nil {
				col = map[string]int{}
				for i, h := range fs {
					col[h] = i
				}
				continue
			}
			get := func(h string) string {
				if i, ok := col[h]; ok && i < len(fs) {
					return fs[i]
				}
				return ""
			}
			st, full, status := get("USPS"), get("NAME"), get("FUNCSTAT")
			lat, lon := trim(get("INTPTLAT")), trim(get("INTPTLONG"))
			_, isPlace := col["LSAD"]
			_, hasStatus := col["FUNCSTAT"]
			isCousub := hasStatus && !isPlace
			if !isPlace && !isCousub { // counties: "Alamance County"
				put(st, full, 2, lat, lon)
				continue
			}
			active := status == "A" || status == "F" || status == "B" || status == "C"
			var rank int
			switch {
			case isCousub && active && slices.Contains(newEngland, st):
				rank = 0
			case isPlace && active:
				rank = 1
			case isCousub && active:
				rank = 3
			case status == "S":
				rank = 4
			default:
				rank = 5
			}
			name := baseName(full)
			put(st, name, rank, lat, lon)
			// Consolidated governments go by their city's name:
			// "Louisville/Jefferson County metro government (balance)",
			// "Nashville-Davidson metropolitan government (balance)".
			if status == "F" {
				for _, sep := range []string{"/", "-"} {
					if i := strings.Index(name, sep); i > 0 {
						put(st, name[:i], rank, lat, lon)
					}
				}
			}
			// "Urban Honolulu CDP".
			if n, ok := strings.CutPrefix(name, "Urban "); ok {
				put(st, n, rank, lat, lon)
			}
			// "Boise City city", "Southbridge Town city".
			for _, suffix := range []string{" City", " Town"} {
				if n, ok := strings.CutSuffix(name, suffix); ok {
					put(st, n, rank+1, lat, lon)
				}
			}
		}
		if err := sc.Err(); err != nil {
			log.Fatal(err)
		}
		f.Close()
	}
	keys := make([]string, 0, len(best))
	for k := range best {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fh, err := os.Create(out)
	if err != nil {
		log.Fatal(err)
	}
	zw, _ := gzip.NewWriterLevel(fh, gzip.BestCompression)
	zw.ModTime = time.Time{} // reproducible output
	w := bufio.NewWriter(zw)
	for _, k := range keys {
		p := best[k]
		fmt.Fprintf(w, "%s\t%s\t%s\n", k, p.lat, p.lon)
	}
	if err := w.Flush(); err != nil {
		log.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		log.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		log.Fatal(err)
	}
	st, _ := os.Stat(out)
	fmt.Printf("wrote %s (%d places, %d bytes)\n", out, len(keys), st.Size())
}

// baseName strips Census's legal/statistical description from a place or
// county subdivision name: "Auburn town" -> "Auburn", "Indianapolis city
// (balance)" -> "Indianapolis", "Abanda CDP" -> "Abanda".
func baseName(full string) string {
	w := strings.Fields(full)
	for len(w) > 1 {
		last := w[len(w)-1]
		if last == "CDP" || last == "CCD" || last == "UT" || last == "(balance)" || last == strings.ToLower(last) {
			w = w[:len(w)-1]
			continue
		}
		break
	}
	return strings.Join(w, " ")
}

// trim keeps three decimals (about 100 m).
func trim(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', 3, 64)
}
