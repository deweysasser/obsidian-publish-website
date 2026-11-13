package html

import (
	"bytes"
	"encoding/binary"
	"flag"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/deweysasser/obsidian-publish-website/internal/config"
	"github.com/deweysasser/obsidian-publish-website/internal/render"
	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

var update = flag.Bool("update", false, "rewrite assets/favicon.png from drawDefaultFavicon")

// defaultFaviconPNG is the side of the committed assets/favicon.png.
const defaultFaviconPNG = 192

// drawDefaultFavicon rasterizes assets/favicon.svg at size×size: a rounded
// purple page with three white text lines. The shapes are copied from the
// SVG; TestDefaultFaviconMatchesSVG fails when the SVG changes without this.
func drawDefaultFavicon(size int) *image.NRGBA {
	const ss = 8 // samples per side of a pixel
	purple := [3]float64{0x6f, 0x42, 0xc1}
	lines := [][4]float64{{9, 10, 23, 10}, {9, 15, 23, 15}, {9, 20, 18, 20}} // stroke-width 2.5, round caps
	inLine := func(x, y float64) bool {
		for _, l := range lines {
			cx := math.Max(l[0], math.Min(l[2], x))
			if math.Hypot(x-cx, y-l[1]) <= 1.25 {
				return true
			}
		}
		return false
	}
	inPage := func(x, y float64) bool { // rect x=4 y=2 w=24 h=28 rx=3
		dx := math.Max(math.Max(4+3-x, x-(28-3)), 0)
		dy := math.Max(math.Max(2+3-y, y-(30-3)), 0)
		return x >= 4 && x <= 28 && y >= 2 && y <= 30 && math.Hypot(dx, dy) <= 3
	}
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	unit := 32 / float64(size)
	for py := range size {
		for px := range size {
			var r, g, b, a float64 // premultiplied sums
			for sy := range ss {
				for sx := range ss {
					x := (float64(px) + (float64(sx)+0.5)/ss) * unit
					y := (float64(py) + (float64(sy)+0.5)/ss) * unit
					switch {
					case !inPage(x, y):
					case inLine(x, y):
						r, g, b, a = r+255, g+255, b+255, a+1
					default:
						r, g, b, a = r+purple[0], g+purple[1], b+purple[2], a+1
					}
				}
			}
			if a == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{uint8(math.Round(r / a)), uint8(math.Round(g / a)), uint8(math.Round(b / a)), uint8(math.Round(a / ss / ss * 255))})
		}
	}
	return img
}

func TestDefaultFaviconMatchesSVG(t *testing.T) {
	svg, _ := assetFS.ReadFile("assets/favicon.svg")
	for _, shape := range []string{`<rect x="4" y="2" width="24" height="28" rx="3" fill="#6f42c1"/>`,
		`<path d="M9 10h14M9 15h14M9 20h9" stroke="#fff" stroke-width="2.5" stroke-linecap="round"/>`} {
		if !strings.Contains(string(svg), shape) {
			t.Fatalf("favicon.svg changed: update drawDefaultFavicon to match, then go test -run %s -update", t.Name())
		}
	}
	want := drawDefaultFavicon(defaultFaviconPNG)
	if *update {
		var buf bytes.Buffer
		if err := png.Encode(&buf, want); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile("assets/favicon.png", buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile("assets/favicon.png") // not assetFS: -update has just written it
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds() != want.Bounds() {
		t.Fatalf("favicon.png is %v, want %v", got.Bounds(), want.Bounds())
	}
	for y := range defaultFaviconPNG {
		for x := range defaultFaviconPNG {
			if color.NRGBAModel.Convert(got.At(x, y)) != want.At(x, y) {
				t.Fatalf("favicon.png differs from favicon.svg at (%d,%d): go test -run %s -update", x, y, t.Name())
			}
		}
	}
}

// icoImages parses an ICO of PNG images, returning each one's directory size
// and decoded image.
func icoImages(t *testing.T, b []byte) (sizes []int, imgs []image.Image) {
	t.Helper()
	le := binary.LittleEndian
	if len(b) < 6 || le.Uint16(b[0:]) != 0 || le.Uint16(b[2:]) != 1 {
		t.Fatalf("not an ICO header: % x", b[:min(6, len(b))])
	}
	for i := range int(le.Uint16(b[4:])) {
		e := b[6+16*i:]
		side := int(e[0])
		if side == 0 {
			side = 256
		}
		if int(e[1]) != int(e[0]) || le.Uint16(e[6:]) != 32 {
			t.Fatalf("entry %d: % x", i, e[:16])
		}
		n, off := le.Uint32(e[8:]), le.Uint32(e[12:])
		img, err := png.Decode(bytes.NewReader(b[off : off+n]))
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if img.Bounds().Dx() != side || img.Bounds().Dy() != side {
			t.Fatalf("entry %d says %d px, holds %v", i, side, img.Bounds())
		}
		sizes, imgs = append(sizes, side), append(imgs, img)
	}
	return sizes, imgs
}

func finalizeFavicon(t *testing.T, vault, favicon string) (out string, warnings []string) {
	t.Helper()
	out = t.TempDir()
	cfg := &config.Config{Vault: vault, Output: out, Search: "none", Site: config.Site{Title: "S", Favicon: favicon}}
	tg, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tg.Warn = func(w string) { warnings = append(warnings, w) }
	if err := tg.Finalize(&target.Site{Config: cfg, Tree: navTree(), Tags: map[string][]*render.Page{}}); err != nil {
		t.Fatal(err)
	}
	return out, warnings
}

func TestDefaultRootFavicons(t *testing.T) {
	out, _ := finalizeFavicon(t, t.TempDir(), "")
	builtin, _ := assetFS.ReadFile("assets/favicon.png")
	if b, err := os.ReadFile(filepath.Join(out, "favicon.png")); err != nil || !bytes.Equal(b, builtin) {
		t.Errorf("/favicon.png is not the built-in icon (err %v)", err)
	}
	b, err := os.ReadFile(filepath.Join(out, "favicon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	sizes, imgs := icoImages(t, b)
	if len(sizes) != 3 || sizes[0] != 16 || sizes[1] != 32 || sizes[2] != 48 {
		t.Errorf("favicon.ico sizes %v", sizes)
	}
	// the page's purple at its centre, transparency in the side margins
	if c := color.NRGBAModel.Convert(imgs[1].At(16, 28)).(color.NRGBA); c.A != 255 || c.B < 0xa0 || c.R > 0x80 {
		t.Errorf("32px centre-bottom = %v, want opaque purple", c)
	}
	if _, _, _, a := imgs[1].At(1, 16).RGBA(); a != 0 {
		t.Errorf("32px left margin alpha %d, want transparent", a)
	}
}

func TestConfiguredRasterFaviconMakesICO(t *testing.T) {
	vault := t.TempDir()
	// a wide red JPEG: letterboxed into a square
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := range 200 {
			src.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(vault, "logo.jpg"), buf.Bytes(), 0o644)
	out, warnings := finalizeFavicon(t, vault, "logo.jpg")
	if len(warnings) > 0 {
		t.Fatal(warnings)
	}
	b, err := os.ReadFile(filepath.Join(out, "favicon.ico"))
	if err != nil {
		t.Fatal(err)
	}
	_, imgs := icoImages(t, b)
	img := imgs[2] // 48px
	if r, _, _, a := img.At(24, 24).RGBA(); a != 0xffff || r < 0xf000 {
		t.Errorf("centre is not red: %v", img.At(24, 24))
	}
	if _, _, _, a := img.At(24, 2).RGBA(); a != 0 {
		t.Errorf("top margin alpha %d, want transparent (letterboxed)", a)
	}
	// a JPEG favicon also gets a /favicon.png, made from it
	pb, err := os.ReadFile(filepath.Join(out, "favicon.png"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg, err := png.DecodeConfig(bytes.NewReader(pb)); err != nil || cfg.Width != 200 {
		t.Errorf("favicon.png: %+v %v", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(out, "favicon.jpg")); err != nil {
		t.Error("the configured favicon itself must still be published")
	}
}

func TestTinyFaviconKeepsOneBitmap(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	b, err := encodeICO(src, icoSizes)
	if err != nil {
		t.Fatal(err)
	}
	if sizes, _ := icoImages(t, b); len(sizes) != 1 || sizes[0] != 16 {
		t.Errorf("sizes %v, want just 16", sizes)
	}
}

func TestFaviconWithoutRootFallbacks(t *testing.T) {
	for name, file := range map[string]string{"svg": "icon.svg", "ico": "icon.ico"} {
		t.Run(name, func(t *testing.T) {
			vault := t.TempDir()
			os.WriteFile(filepath.Join(vault, file), []byte("mine"), 0o644)
			out, warnings := finalizeFavicon(t, vault, file)
			if len(warnings) > 0 {
				t.Fatal(warnings)
			}
			if _, err := os.Stat(filepath.Join(out, "favicon.png")); err == nil {
				t.Error("a generic /favicon.png stands in for the site's own icon")
			}
			ico, err := os.ReadFile(filepath.Join(out, "favicon.ico"))
			if name == "ico" && string(ico) != "mine" {
				t.Errorf("configured favicon.ico replaced: %q", ico)
			}
			if name == "svg" && err == nil {
				t.Error("a generic /favicon.ico stands in for the site's own icon")
			}
		})
	}
}

func TestUndecodableFaviconWarns(t *testing.T) {
	vault := t.TempDir()
	os.WriteFile(filepath.Join(vault, "Icon.PNG"), []byte("png"), 0o644)
	out, warnings := finalizeFavicon(t, vault, "Icon.PNG")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Icon.PNG") {
		t.Errorf("warnings %q", warnings)
	}
	if _, err := os.Stat(filepath.Join(out, "favicon.ico")); err == nil {
		t.Error("favicon.ico written from an undecodable image")
	}
}
