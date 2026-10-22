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

// Locate returns the coordinates of a city, town or county in a state
// (postal code), by name, ignoring case.
func Locate(state, name string) (lat, lon float64, ok bool) {
	p, ok := places()[strings.ToUpper(state)+"\t"+strings.ToLower(strings.TrimSpace(name))]
	return p.lat, p.lon, ok
}
