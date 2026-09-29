package termimg

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func loadPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// maxDiff is the largest per-channel difference between two images,
// comparing colour only where the pixel is visible.
func maxDiff(a, b image.Image) int {
	worst := 0
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			d := func(p, q uint32) int {
				v := int(p>>8) - int(q>>8)
				if v < 0 {
					v = -v
				}
				return v
			}
			worst = max(worst, d(aa, ba))
			if aa > 0x8000 && ba > 0x8000 {
				worst = max(worst, d(ar, br), d(ag, bg), d(ab, bb))
			}
		}
	}
	return worst
}

// Reference frames were composited by Pillow (libwebp) from the same files.
func TestDecodeAnimatedWebP(t *testing.T) {
	tests := []struct {
		name      string
		frames    int
		tolerance float64 // mean colour error; lossy decoders upsample chroma differently
		delays    []time.Duration
	}{
		{"anim_alpha_lossless", 6, 0, []time.Duration{50, 60, 70, 80, 90, 100}},
		{"anim_alpha_lossy", 6, 3, nil},
		{"anim_lossy", 10, 8, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := os.ReadFile("testdata/" + tt.name + ".webp")
			if err != nil {
				t.Fatal(err)
			}
			frames, err := DecodeFrames(data, 4096)
			if err != nil {
				t.Fatal(err)
			}
			if len(frames) != tt.frames {
				t.Fatalf("got %d frames, want %d", len(frames), tt.frames)
			}
			for i, f := range frames {
				want := loadPNG(t, fmt.Sprintf("testdata/%s.%d.png", tt.name, i))
				if f.Img.Bounds() != want.Bounds() {
					t.Fatalf("frame %d bounds %v, want %v", i, f.Img.Bounds(), want.Bounds())
				}
				if tt.tolerance == 0 {
					if d := maxDiff(f.Img, want); d != 0 {
						t.Errorf("frame %d differs from reference by %d", i, d)
					}
				} else if d := meanDiff(f.Img, want); d > tt.tolerance {
					t.Errorf("frame %d: mean colour error %.2f > %.2f", i, d, tt.tolerance)
				}
				if tt.delays != nil && f.Delay != tt.delays[i]*time.Millisecond {
					t.Errorf("frame %d delay %v, want %v", i, f.Delay, tt.delays[i]*time.Millisecond)
				}
			}
		})
	}
}

func TestDecodeFramesScalesAndStill(t *testing.T) {
	data, err := os.ReadFile("testdata/anim_lossy.webp")
	if err != nil {
		t.Fatal(err)
	}
	frames, err := DecodeFrames(data, 80)
	if err != nil {
		t.Fatal(err)
	}
	if b := frames[0].Img.Bounds(); b.Dx() != 80 || b.Dy() != 60 {
		t.Fatalf("scaled frame %v, want 80x60", b)
	}
	// frames must be independent copies, not views of one canvas
	if frames[0].Img == frames[1].Img {
		t.Fatal("frames share an image")
	}

	still, err := DecodeFrames(pngBytes(t, loadPNG(t, "testdata/anim_lossy.0.png")), 4096)
	if err != nil || len(still) != 1 {
		t.Fatalf("still image: %d frames, err %v", len(still), err)
	}
	if _, err := DecodeFrames([]byte("RIFF\x10\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02"), 100); err == nil {
		t.Fatal("truncated animation decoded without error")
	}
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// meanDiff is the mean per-channel colour difference over pixels visible in
// both images; alpha must match within 8.
func meanDiff(a, b image.Image) float64 {
	sum, n := 0, 0
	r := a.Bounds()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			ar, ag, ab, aa := a.At(x, y).RGBA()
			br, bg, bb, ba := b.At(x, y).RGBA()
			d := func(p, q uint32) int {
				v := int(p>>8) - int(q>>8)
				if v < 0 {
					v = -v
				}
				return v
			}
			if d(aa, ba) > 8 {
				return 255
			}
			if aa > 0x8000 {
				sum += d(ar, br) + d(ag, bg) + d(ab, bb)
				n += 3
			}
		}
	}
	if n == 0 {
		return 0
	}
	return float64(sum) / float64(n)
}

func TestVideoFrames(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	src := filepath.Join(t.TempDir(), "clip.mp4")
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=24",
		"-t", "2", "-pix_fmt", "yuv420p", src).CombinedOutput(); err != nil {
		t.Skipf("can't make a test clip: %v %s", err, out)
	}
	frames, err := VideoFrames(context.Background(), src, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 24 { // 2 s at 12 fps
		t.Fatalf("%d frames, want 24", len(frames))
	}
	if b := frames[0].Img.Bounds(); b.Dx() > 100 || b.Dy() > 100 || b.Dx() != 100 {
		t.Fatalf("frame size %v", b)
	}
}
