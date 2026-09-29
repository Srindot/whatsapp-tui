// Package termimg renders images in the terminal, either with kitty's
// graphics protocol (Unicode placeholders, so images are ordinary text cells
// that a TUI can lay out and scroll) or with coloured half-block characters.
package termimg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // register decoders
	_ "image/jpeg"
	"image/png"
	"io"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"
	"golang.org/x/sys/unix"
)

// Mode selects how images are drawn.
type Mode int

const (
	ModeOff    Mode = iota // no images, text placeholders only
	ModeBlocks             // half-block characters (any true-colour terminal)
	ModeKitty              // kitty graphics protocol with Unicode placeholders
)

func (m Mode) String() string {
	switch m {
	case ModeKitty:
		return "kitty"
	case ModeBlocks:
		return "blocks"
	}
	return "off"
}

// Detect picks a mode from a config value ("auto", "kitty", "blocks", "off")
// and the environment.
func Detect(setting string) Mode {
	switch strings.ToLower(strings.TrimSpace(setting)) {
	case "off", "none", "false":
		return ModeOff
	case "blocks", "halfblocks":
		return ModeBlocks
	case "kitty":
		return ModeKitty
	}
	// Unicode placeholders need direct kitty-protocol support; tmux would
	// need passthrough, so fall back to blocks there.
	if os.Getenv("TMUX") == "" {
		term := os.Getenv("TERM")
		if os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" ||
			os.Getenv("TERM_PROGRAM") == "ghostty" || term == "xterm-ghostty" {
			return ModeKitty
		}
	}
	return ModeBlocks
}

// CellSize returns the terminal cell size in pixels, falling back to a
// typical 1:2 cell when the terminal doesn't report pixel sizes.
func CellSize() (w, h int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err == nil && ws.Col > 0 && ws.Row > 0 && ws.Xpixel > 0 && ws.Ypixel > 0 {
		return int(ws.Xpixel) / int(ws.Col), int(ws.Ypixel) / int(ws.Row)
	}
	return 8, 16
}

// FitCells returns the number of columns and rows that show an image of
// imgW×imgH pixels as large as possible within maxCols×maxRows cells, keeping
// its aspect ratio for the given cell size.
func FitCells(imgW, imgH, maxCols, maxRows, cellW, cellH int) (cols, rows int) {
	if imgW <= 0 || imgH <= 0 {
		imgW, imgH = 4, 3
	}
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 8, 16
	}
	scale := math.Min(float64(maxCols*cellW)/float64(imgW), float64(maxRows*cellH)/float64(imgH))
	cols = int(math.Round(float64(imgW) * scale / float64(cellW)))
	rows = int(math.Round(float64(imgH) * scale / float64(cellH)))
	return clamp(cols, 1, maxCols), clamp(rows, 1, maxRows)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Decode decodes JPEG, PNG, GIF or (static) WebP data.
func Decode(data []byte) (image.Image, error) {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		img, err := decodeWebP(data)
		if err != nil {
			return nil, fmt.Errorf("decode image: %w", err)
		}
		return img, nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	return img, nil
}

// Scale resizes img to exactly w×h pixels.
func Scale(img image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
	return dst
}

// Shrink scales img down so neither side exceeds max pixels.
func Shrink(img image.Image, max int) image.Image {
	b := img.Bounds()
	if b.Dx() <= max && b.Dy() <= max {
		return img
	}
	s := float64(max) / float64(b.Dx())
	if b.Dy() > b.Dx() {
		s = float64(max) / float64(b.Dy())
	}
	return Scale(img, int(float64(b.Dx())*s+0.5), int(float64(b.Dy())*s+0.5))
}

// Circle crops img to its centred square and makes everything outside the
// inscribed circle transparent (WhatsApp-style avatars).
func Circle(img image.Image, size int) *image.NRGBA {
	b := img.Bounds()
	side := b.Dx()
	if b.Dy() < side {
		side = b.Dy()
	}
	x0, y0 := b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2
	sq := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(sq, sq.Bounds(), img, image.Rect(x0, y0, x0+side, y0+side), draw.Src, nil)
	r := float64(size) / 2
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			d := math.Hypot(float64(x)+0.5-r, float64(y)+0.5-r)
			switch {
			case d > r:
				sq.SetNRGBA(x, y, color.NRGBA{})
			case d > r-1: // anti-alias the edge
				c := sq.NRGBAAt(x, y)
				c.A = uint8(float64(c.A) * (r - d))
				sq.SetNRGBA(x, y, c)
			}
		}
	}
	return sq
}

// ---------- kitty ----------

// Kitty transmits images and builds placeholder text. Images are written to
// out (the terminal); each escape sequence goes out in a single Write so it
// can't interleave with the TUI's own output on the same *os.File.
type Kitty struct {
	out    io.Writer
	dir    string // temp dir holding PNGs until kitty reads (and deletes) them
	nextID atomic.Uint32

	mu  sync.Mutex
	ids []uint32
}

// NewKitty returns a kitty renderer writing to out. Images are handed to
// kitty as temporary files, so this only works when kitty runs on the same
// machine (not over SSH).
func NewKitty(out io.Writer) (*Kitty, error) {
	// kitty only deletes temp files in a temp dir whose path contains
	// "tty-graphics-protocol".
	dir, err := os.MkdirTemp("", "whatsapp-tui-tty-graphics-protocol-")
	if err != nil {
		return nil, fmt.Errorf("create image dir: %w", err)
	}
	k := &Kitty{out: out, dir: dir}
	// Image IDs share one namespace per kitty window; start at a random
	// point so two running instances are unlikely to collide.
	k.nextID.Store(uint32(rand.Intn(1<<22)) + 1<<16)
	return k, nil
}

// Show transmits img, sized to cols×rows cells, and returns the placeholder
// text (rows lines joined by "\n", each exactly cols cells wide).
func (k *Kitty) Show(img image.Image, cols, rows int) (string, error) {
	return k.ShowFrames([]Frame{{Img: img}}, cols, rows)
}

// writeTemp saves img as a PNG in the temp dir and returns the base64 path
// payload for a t=t transmission.
func (k *Kitty) writeTemp(img image.Image, name string) (string, error) {
	path := filepath.Join(k.dir, name+".png")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return "", fmt.Errorf("encode image: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("write image: %w", err)
	}
	return base64.StdEncoding.EncodeToString([]byte(path)), nil
}

// ShowFrames is Show for animations: kitty receives every frame with its
// delay and loops them by itself, so the UI doesn't redraw anything.
func (k *Kitty) ShowFrames(frames []Frame, cols, rows int) (string, error) {
	if len(frames) == 0 {
		return "", fmt.Errorf("no frames")
	}
	id := k.nextID.Add(1) & 0xFFFFFF
	var seq strings.Builder
	for i, fr := range frames {
		payload, err := k.writeTemp(fr.Img, fmt.Sprintf("%d-%d", id, i))
		if err != nil {
			return "", err
		}
		if i == 0 {
			// a=T transmit+place, t=t temp file (kitty deletes it after
			// reading), f=100 PNG, U=1 virtual placement for Unicode
			// placeholders, q=2 quiet.
			fmt.Fprintf(&seq, "\x1b_Ga=T,t=t,f=100,U=1,q=2,i=%d,c=%d,r=%d;%s\x1b\\", id, cols, rows, payload)
		} else {
			// a=f adds a frame; X=1 replaces the canvas (frames are
			// pre-composited); z is the gap before the next frame.
			fmt.Fprintf(&seq, "\x1b_Ga=f,t=t,f=100,X=1,q=2,i=%d,z=%d;%s\x1b\\", id, ms(fr.Delay), payload)
		}
	}
	if len(frames) > 1 {
		// The root frame's gap is set separately; then loop forever.
		fmt.Fprintf(&seq, "\x1b_Ga=a,q=2,i=%d,r=1,z=%d\x1b\\\x1b_Ga=a,q=2,i=%d,s=3,v=1\x1b\\",
			id, ms(frames[0].Delay), id)
	}
	if _, err := io.WriteString(k.out, seq.String()); err != nil {
		return "", fmt.Errorf("send image: %w", err)
	}
	k.mu.Lock()
	k.ids = append(k.ids, id)
	k.mu.Unlock()
	return Placeholder(id, cols, rows), nil
}

func ms(d time.Duration) int {
	if n := int(d / time.Millisecond); n > 0 {
		return n
	}
	return 100
}

// Close deletes all images this renderer transmitted and its temp dir.
func (k *Kitty) Close() {
	defer os.RemoveAll(k.dir)
	k.mu.Lock()
	defer k.mu.Unlock()
	var b strings.Builder
	for _, id := range k.ids {
		fmt.Fprintf(&b, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
	}
	if b.Len() > 0 {
		_, _ = io.WriteString(k.out, b.String())
	}
	k.ids = nil
}

// Placeholder returns the Unicode placeholder text for image id: the image ID
// is the 24-bit foreground colour, each row starts with its row and column-0
// diacritics and later cells inherit (column+1) from the cell on their left.
func Placeholder(id uint32, cols, rows int) string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", (id>>16)&0xFF, (id>>8)&0xFF, id&0xFF)
	var b strings.Builder
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(fg)
		b.WriteRune(0x10EEEE)
		b.WriteRune(diacritics[r%len(diacritics)])
		b.WriteRune(diacritics[0])
		for c := 1; c < cols; c++ {
			b.WriteRune(0x10EEEE)
		}
		b.WriteString("\x1b[39m")
	}
	return b.String()
}

// ---------- half blocks ----------

// Blocks renders img into cols×rows cells using "▀" with the top pixel as
// foreground and the bottom pixel as background. Fully transparent pixels
// keep the terminal background.
func Blocks(img image.Image, cols, rows int) string {
	px := Scale(img, cols, rows*2)
	var b strings.Builder
	for r := 0; r < rows; r++ {
		if r > 0 {
			b.WriteByte('\n')
		}
		for c := 0; c < cols; c++ {
			top, bot := px.NRGBAAt(c, 2*r), px.NRGBAAt(c, 2*r+1)
			switch {
			case top.A < 128 && bot.A < 128:
				b.WriteString("\x1b[0m ")
			case bot.A < 128:
				fmt.Fprintf(&b, "\x1b[0;38;2;%d;%d;%dm▀", top.R, top.G, top.B)
			case top.A < 128:
				fmt.Fprintf(&b, "\x1b[0;38;2;%d;%d;%dm▄", bot.R, bot.G, bot.B)
			default:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", top.R, top.G, top.B, bot.R, bot.G, bot.B)
			}
		}
		b.WriteString("\x1b[0m")
	}
	return b.String()
}
