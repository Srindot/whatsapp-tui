package termimg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"time"

	"golang.org/x/image/webp"
)

// Frame is one frame of an animation, fully composited.
type Frame struct {
	Img   image.Image
	Delay time.Duration
}

// maxFrames caps decoded frames so a huge animation can't exhaust memory.
const maxFrames = 150

// DecodeFrames decodes an image that may be animated. Animated WebP (as used
// by WhatsApp stickers) yields every frame composited onto its canvas and
// scaled to fit maxSide pixels; anything else yields a single frame.
func DecodeFrames(data []byte, maxSide int) ([]Frame, error) {
	if isAnimatedWebP(data) {
		return decodeAnimatedWebP(data, maxSide)
	}
	img, err := Decode(data)
	if err != nil {
		return nil, err
	}
	return []Frame{{Img: Shrink(img, maxSide)}}, nil
}

type riffChunk struct {
	fourCC string
	data   []byte
}

// riffChunks splits a chunk list; sizes are little-endian and chunks are
// padded to even length.
func riffChunks(b []byte) ([]riffChunk, error) {
	var out []riffChunk
	for len(b) >= 8 {
		n := binary.LittleEndian.Uint32(b[4:8])
		if uint64(n) > uint64(len(b)-8) {
			return nil, errors.New("webp: truncated chunk")
		}
		out = append(out, riffChunk{string(b[:4]), b[8 : 8+n]})
		n += n & 1
		if uint64(n) >= uint64(len(b)-8) {
			break
		}
		b = b[8+n:]
	}
	return out, nil
}

func isAnimatedWebP(b []byte) bool {
	return len(b) >= 21 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" &&
		string(b[12:16]) == "VP8X" && b[20]&0x02 != 0
}

func u24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

func putU24(b []byte, v int) { b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16) }

func chunkBytes(fourCC string, data []byte) []byte {
	out := make([]byte, 8, 8+len(data)+1)
	copy(out, fourCC)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
	out = append(out, data...)
	if len(data)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

// frameWebP wraps one ANMF frame's bitstream as a standalone still WebP
// that golang.org/x/image/webp can decode.
func frameWebP(sub []riffChunk, w, h int) []byte {
	var body []byte
	hasAlpha := false
	for _, c := range sub {
		if c.fourCC == "ALPH" {
			hasAlpha = true
		}
	}
	if hasAlpha {
		vp8x := make([]byte, 10)
		vp8x[0] = 0x10 // alpha
		putU24(vp8x[4:], w-1)
		putU24(vp8x[7:], h-1)
		body = append(body, chunkBytes("VP8X", vp8x)...)
	}
	for _, c := range sub {
		switch c.fourCC {
		case "ALPH", "VP8 ", "VP8L":
			body = append(body, chunkBytes(c.fourCC, c.data)...)
		}
	}
	out := make([]byte, 12, 12+len(body))
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(4+len(body)))
	copy(out[8:], "WEBP")
	return append(out, body...)
}

func decodeAnimatedWebP(data []byte, maxSide int) ([]Frame, error) {
	chunks, err := riffChunks(data[12:])
	if err != nil {
		return nil, err
	}
	if len(chunks) == 0 || chunks[0].fourCC != "VP8X" || len(chunks[0].data) < 10 {
		return nil, errors.New("webp: missing VP8X header")
	}
	cw, ch := u24(chunks[0].data[4:])+1, u24(chunks[0].data[7:])+1
	if cw > 4096 || ch > 4096 {
		return nil, fmt.Errorf("webp: canvas %dx%d too large", cw, ch)
	}
	canvas := image.NewNRGBA(image.Rect(0, 0, cw, ch))

	var frames []Frame
	var prevRect image.Rectangle
	prevDispose := false
	for _, c := range chunks[1:] {
		if c.fourCC != "ANMF" || len(c.data) < 16 {
			continue
		}
		if len(frames) >= maxFrames {
			break
		}
		h := c.data[:16]
		x, y := u24(h[0:])*2, u24(h[3:])*2
		fw, fh := u24(h[6:])+1, u24(h[9:])+1
		delay := time.Duration(u24(h[12:])) * time.Millisecond
		noBlend, dispose := h[15]&0x02 != 0, h[15]&0x01 != 0

		sub, err := riffChunks(c.data[16:])
		if err != nil {
			return nil, err
		}
		img, err := decodeWebP(frameWebP(sub, fw, fh))
		if err != nil {
			return nil, fmt.Errorf("webp frame %d: %w", len(frames)+1, err)
		}

		// The previous frame's area is cleared before drawing this one if it
		// asked to be disposed to the (transparent) background.
		if prevDispose {
			draw.Draw(canvas, prevRect, image.Transparent, image.Point{}, draw.Src)
		}
		rect := image.Rect(x, y, x+fw, y+fh).Intersect(canvas.Bounds())
		op := draw.Over
		if noBlend {
			op = draw.Src
		}
		draw.Draw(canvas, rect, img, img.Bounds().Min, op)
		prevRect, prevDispose = rect, dispose

		// Browsers treat tiny delays as 100ms; so do we.
		if delay <= 10*time.Millisecond {
			delay = 100 * time.Millisecond
		}
		snap := Shrink(canvas, maxSide)
		if snap == image.Image(canvas) { // Shrink returned the canvas itself
			snap = cloneNRGBA(canvas)
		}
		frames = append(frames, Frame{Img: snap, Delay: delay})
	}
	if len(frames) == 0 {
		return nil, errors.New("webp: animation has no frames")
	}
	return frames, nil
}

func cloneNRGBA(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	copy(dst.Pix, src.Pix)
	return dst
}

// decodeWebP decodes a still WebP. Lossy WebP stores limited-range BT.601
// YUV (Y in 16..235), but x/image/webp returns it as a full-range YCbCr
// image, which looks washed out; convert it the way libwebp does.
func decodeWebP(data []byte) (image.Image, error) {
	img, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	switch m := img.(type) {
	case *image.YCbCr:
		return yuvToNRGBA(m, nil), nil
	case *image.NYCbCrA:
		return yuvToNRGBA(&m.YCbCr, m), nil
	}
	return img, nil
}

func yuvToNRGBA(m *image.YCbCr, alpha *image.NYCbCrA) *image.NRGBA {
	b := m.Rect
	out := image.NewNRGBA(b)
	clip := func(v int) uint8 {
		if v < 0 {
			return 0
		}
		if v > 255 {
			return 255
		}
		return uint8(v)
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			yy := (int(m.Y[m.YOffset(x, y)]) - 16) * 76309 // 1.164 * 2^16
			ci := m.COffset(x, y)
			cb, cr := int(m.Cb[ci])-128, int(m.Cr[ci])-128
			o := out.PixOffset(x, y)
			out.Pix[o+0] = clip((yy + 104597*cr + 1<<15) >> 16)           // 1.596
			out.Pix[o+1] = clip((yy - 25675*cb - 53279*cr + 1<<15) >> 16) // 0.391, 0.813
			out.Pix[o+2] = clip((yy + 132201*cb + 1<<15) >> 16)           // 2.018
			out.Pix[o+3] = 255
			if alpha != nil {
				out.Pix[o+3] = alpha.A[alpha.AOffset(x, y)]
			}
		}
	}
	return out
}
