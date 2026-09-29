package termimg

import (
	"image"
	"image/color"
	"math"
	"sync"
	"unicode"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

var (
	boldOnce sync.Once
	boldFont *opentype.Font
)

func bold() *opentype.Font {
	boldOnce.Do(func() {
		boldFont, _ = opentype.Parse(gobold.TTF)
	})
	return boldFont
}

// AvatarInitial picks the letter shown on a chat's fallback avatar: the first
// letter of name, upper-cased. ok is false when name has no letter (phone
// numbers, emoji-only names), which get a generic person icon instead.
func AvatarInitial(name string) (r rune, ok bool) {
	for _, c := range name {
		if unicode.IsLetter(c) {
			return unicode.ToUpper(c), true
		}
	}
	return 0, false
}

// LetterAvatar draws a round avatar of size×size pixels: letter centred on a
// bg-coloured disc in fg, or a person silhouette when the letter is 0 or
// missing from the font.
func LetterAvatar(letter rune, bg, fg color.Color, size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	r := float64(size) / 2
	disc(img, r, r, r, bg)

	if letter == 0 || !drawGlyph(img, letter, fg, size) {
		person(img, size, fg)
	}
	return img
}

// drawGlyph centres letter on img using its ink bounds. It reports false when
// the font has no glyph for it.
func drawGlyph(img *image.NRGBA, letter rune, fg color.Color, size int) bool {
	f := bold()
	if f == nil {
		return false
	}
	var buf sfnt.Buffer
	if idx, err := f.GlyphIndex(&buf, letter); err != nil || idx == 0 {
		return false
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: float64(size) * 0.5, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		return false
	}
	defer face.Close()
	bounds, _, ok := face.GlyphBounds(letter)
	if !ok {
		return false
	}
	w := (bounds.Max.X - bounds.Min.X).Ceil()
	h := (bounds.Max.Y - bounds.Min.Y).Ceil()
	dot := fixed.Point26_6{
		X: fixed.I((size-w)/2) - bounds.Min.X,
		Y: fixed.I((size-h)/2) - bounds.Min.Y,
	}
	d := font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: face, Dot: dot}
	d.DrawString(string(letter))
	return true
}

// person draws a simple head-and-shoulders silhouette, clipped to the disc.
func person(img *image.NRGBA, size int, fg color.Color) {
	s := float64(size)
	disc(img, s/2, s*0.38, s*0.18, fg)
	// shoulders: the top of a large disc, cut off by the avatar circle
	cx, cy, rr := s/2, s*0.98, s*0.34
	R := s / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			if math.Hypot(px-cx, py-cy) <= rr && math.Hypot(px-R, py-R) <= R-1 {
				img.Set(x, y, fg)
			}
		}
	}
}

// disc fills an anti-aliased circle.
func disc(img *image.NRGBA, cx, cy, r float64, c color.Color) {
	cr, cg, cb, ca := c.RGBA()
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			d := math.Hypot(float64(x)+0.5-cx, float64(y)+0.5-cy)
			if d > r {
				continue
			}
			cov := 1.0
			if d > r-1 {
				cov = r - d
			}
			a := float64(ca>>8) * cov
			old := img.NRGBAAt(x, y)
			// "over" compositing onto whatever is already there
			oa := float64(old.A) * (1 - a/255)
			na := a + oa
			if na <= 0 {
				continue
			}
			mix := func(src uint32, dst uint8) uint8 {
				return uint8((float64(src>>8)*a + float64(dst)*oa) / na)
			}
			img.SetNRGBA(x, y, color.NRGBA{mix(cr, old.R), mix(cg, old.G), mix(cb, old.B), uint8(na)})
		}
	}
}
