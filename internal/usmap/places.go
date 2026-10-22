package usmap

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strconv"
	"strings"
	"sync"
)

// places.tsv.gz has one line per state and place name: postal code, the
// lower-cased name as a note would be titled ("auburn", "alamance county"),
// latitude, longitude. Each name keeps the most likely government with it
// (see tools/usmap).
//
//go:embed places.tsv.gz
var placesGz []byte

type latLon struct{ lat, lon float64 }

var places = sync.OnceValue(func() map[string]latLon {
	zr, err := gzip.NewReader(bytes.NewReader(placesGz))
	if err != nil {
		panic("places.tsv.gz: " + err.Error())
	}
	m := map[string]latLon{}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 4 {
			continue
		}
		lat, _ := strconv.ParseFloat(f[2], 64)
		lon, _ := strconv.ParseFloat(f[3], 64)
		m[f[0]+"\t"+f[1]] = latLon{lat, lon}
	}
	if err := sc.Err(); err != nil {
		panic("places.tsv.gz: " + err.Error())
	}
	return m
})

// islandAreas covers the main towns of the territories the Census
// gazetteer leaves out (it has Puerto Rico only). Approximate town centres.
var islandAreas = map[string]latLon{
	"AS\tpago pago": {-14.278, -170.703}, "AS\tfagatogo": {-14.279, -170.691}, "AS\ttafuna": {-14.336, -170.720},
	"AS\tleone": {-14.339, -170.785}, "AS\tutulei": {-14.286, -170.687},
	"GU\thagåtña": {13.475, 144.750}, "GU\thagatna": {13.475, 144.750}, "GU\tagana": {13.475, 144.750},
	"GU\tdededo": {13.518, 144.839}, "GU\ttamuning": {13.488, 144.781}, "GU\tyigo": {13.536, 144.889},
	"GU\tbarrigada": {13.468, 144.800}, "GU\tmangilao": {13.448, 144.801},
	"MP\tsaipan": {15.185, 145.747}, "MP\tgarapan": {15.212, 145.720}, "MP\tsusupe": {15.153, 145.708},
	"MP\ttinian":           {15.000, 145.633},
	"VI\tcharlotte amalie": {18.342, -64.931}, "VI\tchristiansted": {17.747, -64.703}, "VI\tfrederiksted": {17.713, -64.882},
	"VI\tcruz bay": {18.331, -64.794}, "VI\tst. thomas": {18.338, -64.894}, "VI\tst. croix": {17.729, -64.734},
	"VI\tst. john": {18.337, -64.728},
}

// Locate returns the coordinates of a city, town or county in a state
// (postal code), by name, ignoring case.
func Locate(state, name string) (lat, lon float64, ok bool) {
	k := strings.ToUpper(state) + "\t" + strings.ToLower(strings.TrimSpace(name))
	p, ok := places()[k]
	if !ok {
		p, ok = islandAreas[k]
	}
	return p.lat, p.lon, ok
}
