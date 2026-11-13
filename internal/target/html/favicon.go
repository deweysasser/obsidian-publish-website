package html

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // decoders for a configured site.favicon
	_ "image/jpeg"
	"image/png"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/deweysasser/obsidian-publish-website/internal/target"
)

// icoSizes are the bitmaps put in /favicon.ico: tabs, taskbars and
// shortcuts pick the nearest.
var icoSizes = []int{16, 32, 48}

// rasterFavicon are the site.favicon formats the generator can decode, and
// so turn into /favicon.ico.
var rasterFavicon = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true}

// writeRootFavicons publishes /favicon.ico and /favicon.png, which browsers
// and crawlers request at the site root whatever the page's <link rel="icon">
// says. They come from the configured site.favicon when it is a raster image,
// or else from the built-in icon. A configured SVG or WebP gets neither: a
// generic icon there would be the wrong one.
func (t *Target) writeRootFavicons(cfg string, vault string) error {
	var src image.Image
	switch ext := strings.ToLower(path.Ext(cfg)); {
	case cfg == "":
		b, err := assetFS.ReadFile("assets/favicon.png")
		if err != nil {
			return err
		}
		if err := target.WriteFile(t.out, "favicon.png", b); err != nil {
			return err
		}
		if src, err = png.Decode(bytes.NewReader(b)); err != nil {
			return fmt.Errorf("built-in favicon.png: %w", err)
		}
	case ext == ".ico":
		return nil // already published as /favicon.ico
	case rasterFavicon[ext]:
		f, err := os.Open(filepath.Join(vault, cfg)) // #nosec G304 -- the configured favicon
		if err != nil {
			return fmt.Errorf("site.favicon: %w", err)
		}
		defer f.Close()
		if src, _, err = image.Decode(f); err != nil {
			t.Warn(fmt.Sprintf("site.favicon %s: not published as /favicon.ico: %v", cfg, err))
			return nil
		}
		if ext != ".png" {
			var buf bytes.Buffer
			if err := png.Encode(&buf, src); err != nil {
				return err
			}
			if err := target.WriteFile(t.out, "favicon.png", buf.Bytes()); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	ico, err := encodeICO(src, icoSizes)
	if err != nil {
		return err
	}
	return target.WriteFile(t.out, "favicon.ico", ico)
}

// encodeICO writes src, scaled to each size, as an ICO file of PNG images
// (supported by every browser since IE 11). Sizes larger than the source are
// left out, except the smallest, so a tiny source still gives one bitmap.
func encodeICO(src image.Image, sizes []int) ([]byte, error) {
	b := src.Bounds()
	longest := max(b.Dx(), b.Dy())
	if longest == 0 {
		return nil, errors.New("empty image")
	}
	var pngs [][]byte
	var dims []int
	for i, n := range sizes {
		if n > 256 {
			return nil, fmt.Errorf("ICO bitmaps are at most 256 pixels, not %d", n)
		}
		if n > longest && i > 0 {
			break
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, scaleSquare(src, n)); err != nil {
			return nil, err
		}
		pngs, dims = append(pngs, buf.Bytes()), append(dims, n)
	}
	// ICONDIR, then one 16-byte ICONDIRENTRY per image, then the images.
	var out bytes.Buffer
	le := binary.LittleEndian
	out.Write(le.AppendUint16(le.AppendUint16(le.AppendUint16(nil, 0), 1), uint16(len(pngs)))) // #nosec G115 -- at most len(sizes)
	offset := 6 + 16*len(pngs)
	for i, p := range pngs {
		side := byte(dims[i] % 256) // #nosec G115 -- sizes are checked ≤ 256 above; 256 is written as 0
		e := []byte{side, side, 0, 0}
		e = le.AppendUint16(e, 1)              // colour planes
		e = le.AppendUint16(e, 32)             // bits per pixel
		e = le.AppendUint32(e, uint32(len(p))) // #nosec G115 -- a small PNG
		e = le.AppendUint32(e, uint32(offset)) // #nosec G115 -- a small file
		out.Write(e)
		offset += len(p)
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes(), nil
}

// scaleSquare fits src into an n×n image, centred, keeping its aspect ratio
// and leaving the margin transparent. Each output pixel averages a grid of
// samples, so downscaling is smooth; upscaling is nearest-neighbour.
func scaleSquare(src image.Image, n int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := float64(max(w, h)) / float64(n) // source pixels per output pixel
	offX := (float64(n) - float64(w)/scale) / 2
	offY := (float64(n) - float64(h)/scale) / 2
	k := max(1, int(scale+0.999)) // samples per side of each output pixel
	dst := image.NewNRGBA(image.Rect(0, 0, n, n))
	for y := range n {
		for x := range n {
			var r, g, bl, a, count uint64
			for sy := range k {
				for sx := range k {
					count++
					fx := (float64(x) + (float64(sx)+0.5)/float64(k) - offX) * scale
					fy := (float64(y) + (float64(sy)+0.5)/float64(k) - offY) * scale
					if fx < 0 || fy < 0 || fx >= float64(w) || fy >= float64(h) {
						continue // the transparent margin
					}
					cr, cg, cb, ca := src.At(b.Min.X+int(fx), b.Min.Y+int(fy)).RGBA()
					r, g, bl, a = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca)
				}
			}
			// premultiplied averages; Set converts to NRGBA
			dst.Set(x, y, color.RGBA64{uint16(r / count), uint16(g / count), uint16(bl / count), uint16(a / count)}) // #nosec G115 -- averages of 16-bit values
		}
	}
	return dst
}
