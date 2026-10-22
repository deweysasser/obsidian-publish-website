package usmap

import "testing"

func TestTownsProjectInsideTheirState(t *testing.T) {
	m := Load()
	for _, c := range []struct{ state, town string }{
		{"Massachusetts", "Auburn"}, {"Massachusetts", "Worcester"}, {"Kentucky", "Lexington-Fayette"},
		{"Kentucky", "Louisville"}, {"Tennessee", "Nashville"}, {"Hawaii", "Honolulu"}, {"Alaska", "Anchorage"},
		{"North Carolina", "Alamance County"}, {"District of Columbia", "Washington"}, {"Texas", "El Paso"},
		{"Maine", "Portland"}, {"Puerto Rico", "San Juan"},
	} {
		r := m.Find(c.state)
		if r == nil {
			t.Fatalf("no region %q", c.state)
		}
		lat, lon, ok := Locate(r.Code, c.town)
		if !ok {
			t.Errorf("%s, %s: not found", c.town, r.Code)
			continue
		}
		x, y := r.Proj.Point(lon, lat)
		if b := r.BBox; x < b[0] || x > b[2] || y < b[1] || y > b[3] {
			t.Errorf("%s, %s at (%.1f, %.1f) is outside %v", c.town, r.Code, x, y, b)
		}
	}
}

func TestLocatePrefersGovernments(t *testing.T) {
	// Kentucky's "Louisville city" is a defunct place; the metro government
	// is what a town page means.
	lat, _, _ := Locate("KY", "louisville")
	if lat < 38.1 || lat > 38.2 {
		t.Errorf("Louisville, KY latitude %v", lat)
	}
	if _, _, ok := Locate("MA", "Nowhere Special"); ok {
		t.Error("unknown place found")
	}
}

func TestFindByNameCodeOrAlias(t *testing.T) {
	m := Load()
	for _, n := range []string{"massachusetts", "MA", "United States Virgin Islands", "U.S. Virgin Islands"} {
		if m.Find(n) == nil {
			t.Errorf("%q not found", n)
		}
	}
	if len(m.Regions) != 56 {
		t.Errorf("%d regions", len(m.Regions))
	}
}

func TestLocateTownCitiesAndIslandAreas(t *testing.T) {
	m := Load()
	for _, c := range []struct{ state, town string }{
		{"MA", "Southbridge"}, // "Southbridge Town city"
		{"AS", "Pago Pago"}, {"AS", "Tafuna"}, {"GU", "Dededo"}, {"GU", "Hagåtña"}, {"GU", "Hagatna"},
		{"MP", "Saipan"}, {"VI", "Charlotte Amalie"}, {"VI", "Christiansted"},
	} {
		lat, lon, ok := Locate(c.state, c.town)
		if !ok {
			t.Errorf("%s, %s: not found", c.town, c.state)
			continue
		}
		r := m.Find(c.state)
		x, y := r.Proj.Point(lon, lat)
		if b := r.BBox; x < b[0]-1 || x > b[2]+1 || y < b[1]-1 || y > b[3]+1 {
			t.Errorf("%s, %s at (%.1f, %.1f) is outside %v", c.town, c.state, x, y, b)
		}
	}
}
