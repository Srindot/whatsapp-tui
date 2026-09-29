package termimg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Frame rate and length used when playing GIFs (short silent videos).
const (
	videoFPS        = 12
	videoMaxSeconds = 8
)

// VideoFrames decodes the start of a video into frames no larger than
// maxSide pixels, using ffmpeg. WhatsApp GIFs are MP4 files.
func VideoFrames(ctx context.Context, path string, maxSide int) ([]Frame, error) {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, errors.New("ffmpeg not found")
	}
	dir, err := os.MkdirTemp("", "whatsapp-tui-frames-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	scale := fmt.Sprintf("scale='min(%d,iw)':'min(%d,ih)':force_original_aspect_ratio=decrease", maxSide, maxSide)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "-hide_banner", "-loglevel", "error", "-i", path,
		"-t", strconv.Itoa(videoMaxSeconds), "-an", "-vf", fmt.Sprintf("fps=%d,%s", videoFPS, scale),
		filepath.Join(dir, "%04d.png"))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg: %v %s", err, stderr.String())
	}
	names, _ := filepath.Glob(filepath.Join(dir, "*.png"))
	sort.Strings(names)
	if len(names) > maxFrames {
		names = names[:maxFrames]
	}
	frames := make([]Frame, 0, len(names))
	for _, n := range names {
		f, err := os.Open(n)
		if err != nil {
			return nil, err
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		frames = append(frames, Frame{Img: img, Delay: time.Second / videoFPS})
	}
	if len(frames) == 0 {
		return nil, errors.New("video has no frames")
	}
	return frames, nil
}
