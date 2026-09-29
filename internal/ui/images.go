package ui

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// MediaSource fetches media files for the UI; *messages.SessionManager
// implements it. Both methods may block and are called off the UI loop.
type MediaSource interface {
	DownloadMedia(ctx context.Context, msgID string) (string, error)
	ProfilePicture(ctx context.Context, jid string) (string, error)
}

type imgKind int

const (
	imgMessage imgKind = iota
	imgAvatar
)

// imgKey identifies one rendered image: a kitty placement has a fixed cell
// size, so the same picture at two sizes is two entries.
type imgKey struct {
	kind       imgKind
	id         string // message ID or chat JID
	cols, rows int
}

type imgState int

const (
	imgLoading imgState = iota
	imgReady
	imgFailed // no picture / not downloadable; keep the text fallback
)

type imgEntry struct {
	state   imgState
	text    string // placeholder or half-block text, rows joined by "\n"
	full    bool   // text shows the full image (not just the thumbnail)
	pending int    // loaders still running
}

// imageReadyMsg delivers a rendered image to the UI loop.
type imageReadyMsg struct {
	key  imgKey
	text string
	full bool
	err  error
}

// images holds rendering state. The map is shared by all copies of the
// Model and only touched on the UI loop; loaders run in tea.Cmds and report
// back with imageReadyMsg.
type images struct {
	mode         termimg.Mode
	kitty        *termimg.Kitty
	src          MediaSource
	cellW, cellH int
	entries      map[imgKey]*imgEntry
}

func newImages(mode termimg.Mode, kitty *termimg.Kitty, src MediaSource) *images {
	if mode == termimg.ModeKitty && kitty == nil {
		mode = termimg.ModeBlocks
	}
	im := &images{mode: mode, kitty: kitty, src: src, entries: map[imgKey]*imgEntry{}}
	im.cellW, im.cellH = termimg.CellSize()
	return im
}

func (im *images) enabled() bool { return im != nil && im.mode != termimg.ModeOff }

// avatarsEnabled: half-block avatars at 4×2 cells are too coarse to be useful.
func (im *images) avatarsEnabled() bool {
	return im != nil && im.mode == termimg.ModeKitty && im.src != nil
}

func (im *images) get(k imgKey) *imgEntry {
	if im == nil {
		return nil
	}
	return im.entries[k]
}

// render draws img into cols×rows cells with the active mode. Called from
// loader goroutines; Kitty is safe for concurrent use.
func (im *images) render(img image.Image, cols, rows int) (string, error) {
	return im.renderFrames([]termimg.Frame{{Img: termimg.Shrink(img, 1024)}}, cols, rows)
}

// renderFrames draws a (possibly animated) image; only kitty animates.
func (im *images) renderFrames(frames []termimg.Frame, cols, rows int) (string, error) {
	if im.mode == termimg.ModeKitty {
		return im.kitty.ShowFrames(frames, cols, rows)
	}
	return termimg.Blocks(frames[0].Img, cols, rows), nil
}

// ensureMessage starts loading a message's image unless already done.
// The embedded thumbnail is shown first, then the full download replaces it.
func (im *images) ensureMessage(msg messages.Message, meta messages.MediaMeta, cols, rows int) tea.Cmd {
	k := imgKey{imgMessage, msg.Id, cols, rows}
	if !im.enabled() || im.entries[k] != nil {
		return nil
	}
	im.entries[k] = &imgEntry{state: imgLoading}

	var cmds []tea.Cmd
	if len(meta.Thumbnail) > 0 {
		thumb := meta.Thumbnail
		cmds = append(cmds, func() tea.Msg {
			img, err := termimg.Decode(thumb)
			if err != nil {
				return imageReadyMsg{key: k, err: err}
			}
			text, err := im.render(img, cols, rows)
			return imageReadyMsg{key: k, text: text, err: err}
		})
	}
	// GIFs are short videos: kitty plays them (frames via ffmpeg); other
	// terminals keep the thumbnail, as do real videos.
	if im.src != nil && meta.Type == messages.MediaGIF && im.mode == termimg.ModeKitty {
		id := msg.Id
		maxSide := min(max(cols*im.cellW, rows*im.cellH), 480)
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			path, err := im.src.DownloadMedia(ctx, id)
			if err != nil {
				return imageReadyMsg{key: k, full: true, err: err}
			}
			frames, err := termimg.VideoFrames(ctx, path, maxSide)
			if err != nil {
				return imageReadyMsg{key: k, full: true, err: err}
			}
			text, err := im.renderFrames(frames, cols, rows)
			return imageReadyMsg{key: k, text: text, full: true, err: err}
		})
	}
	// Stickers may be animated WebP: kitty plays every frame, other
	// terminals show the first.
	if im.src != nil && (meta.Type == messages.MediaImage || meta.Type == messages.MediaSticker) {
		id := msg.Id
		maxSide := min(max(cols*im.cellW, rows*im.cellH)*2, 1024)
		cmds = append(cmds, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			path, err := im.src.DownloadMedia(ctx, id)
			if err != nil {
				return imageReadyMsg{key: k, full: true, err: err}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return imageReadyMsg{key: k, full: true, err: err}
			}
			frames, err := termimg.DecodeFrames(data, maxSide)
			if err != nil {
				return imageReadyMsg{key: k, full: true, err: err}
			}
			text, err := im.renderFrames(frames, cols, rows)
			return imageReadyMsg{key: k, text: text, full: true, err: err}
		})
	}
	if len(cmds) == 0 {
		im.entries[k].state = imgFailed
		return nil
	}
	im.entries[k].pending = len(cmds)
	return tea.Batch(cmds...)
}

// ensureAvatar starts loading a chat's avatar unless already done. A round
// letter avatar is drawn right away; the profile picture replaces it when the
// chat has one.
func (im *images) ensureAvatar(c *messages.Conversation, cols, rows int) tea.Cmd {
	k := imgKey{imgAvatar, c.JID, cols, rows}
	if !im.avatarsEnabled() || im.entries[k] != nil {
		return nil
	}
	im.entries[k] = &imgEntry{state: imgLoading, pending: 2}
	size := cols * im.cellW
	if h := rows * im.cellH; h < size {
		size = h
	}
	size = max(size*2, 32) // oversample; kitty scales it into the cells
	letter, _ := termimg.AvatarInitial(chatName(c))
	bg, fg := hexColor(string(senderColor(c.JID))), hexColor(string(pal.Base))
	if letter == 0 { // no letter: WhatsApp's grey person icon
		bg, fg = hexColor(string(pal.HighlightHigh)), hexColor(string(pal.Subtle))
	}
	jid := c.JID

	fallback := func() tea.Msg {
		text, err := im.render(termimg.LetterAvatar(letter, bg, fg, size), cols, rows)
		return imageReadyMsg{key: k, text: text, err: err}
	}
	photo := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		path, err := im.src.ProfilePicture(ctx, jid)
		if err != nil || path == "" {
			return imageReadyMsg{key: k, full: true, err: errNoPicture(err)}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return imageReadyMsg{key: k, full: true, err: err}
		}
		img, err := termimg.Decode(data)
		if err != nil {
			return imageReadyMsg{key: k, full: true, err: err}
		}
		text, err := im.render(termimg.Circle(img, size), cols, rows)
		return imageReadyMsg{key: k, text: text, full: true, err: err}
	}
	return tea.Batch(fallback, photo)
}

// hexColor parses "#rrggbb"; anything else yields grey.
func hexColor(s string) color.NRGBA {
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return color.NRGBA{128, 128, 128, 255}
	}
	return color.NRGBA{r, g, b, 255}
}

type noPicture struct{ err error }

func (e noPicture) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return "no profile picture"
}

func errNoPicture(err error) error { return noPicture{err} }

// apply stores a loader result. It reports whether anything visible changed.
func (im *images) apply(msg imageReadyMsg) bool {
	e := im.entries[msg.key]
	if e == nil {
		return false
	}
	e.pending--
	changed := false
	// Take the result unless it's a late thumbnail after the full image.
	// A failed full download keeps the thumbnail if there is one.
	if msg.err == nil && !(e.full && !msg.full) {
		e.state, e.text, e.full = imgReady, msg.text, msg.full
		changed = true
	}
	if e.pending <= 0 && e.state == imgLoading {
		e.state = imgFailed
		changed = true
	}
	return changed
}

// reset forgets every rendered image so they're drawn (and sent to kitty)
// again, e.g. after another program used the terminal.
func (im *images) reset() {
	if im != nil {
		im.entries = map[imgKey]*imgEntry{}
	}
}

// resize updates the cell size; rendered kitty placements keep their size.
func (im *images) resize() {
	if im != nil {
		im.cellW, im.cellH = termimg.CellSize()
	}
}
