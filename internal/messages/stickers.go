package messages

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// recentMediaScan is how many rows are read to find distinct recents.
const recentMediaScan = 600

// RecentMedia returns up to limit distinct stickers or GIFs (mediaType
// MediaSticker or MediaGIF) you received or sent, newest first. The same
// sticker sent many times appears once.
func (sm *SessionManager) RecentMedia(ctx context.Context, mediaType string, limit int) ([]Message, error) {
	rows, err := sm.db.GetRecentMedia(mediaType, recentMediaScan)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []Message
	for _, m := range rows {
		var pm waE2E.Message
		if proto.Unmarshal(m.Media, &pm) != nil {
			continue
		}
		var hash []byte
		var path string
		switch {
		case pm.GetStickerMessage() != nil:
			hash, path = pm.GetStickerMessage().GetFileSHA256(), pm.GetStickerMessage().GetDirectPath()
		case pm.GetVideoMessage() != nil:
			hash, path = pm.GetVideoMessage().GetFileSHA256(), pm.GetVideoMessage().GetDirectPath()
		}
		if path == "" {
			continue // never uploaded; can't be re-sent
		}
		key := hex.EncodeToString(hash)
		if key == "" {
			key = path
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	return out, ctx.Err()
}

// SendExisting sends a sticker or GIF you already have (received or sent
// before) to chat, reusing the file on WhatsApp's servers, like the
// phone's sticker tray. Safe from any goroutine.
func (sm *SessionManager) SendExisting(ctx context.Context, chat, msgID string) error {
	m, err := sm.db.GetMessage(msgID)
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	var pm waE2E.Message
	if err := proto.Unmarshal(m.Media, &pm); err != nil || len(m.Media) == 0 {
		return errors.New("this message has no media to send")
	}
	switch {
	case pm.GetStickerMessage() != nil:
		pm.GetStickerMessage().ContextInfo = nil
	case pm.GetVideoMessage() != nil:
		v := pm.GetVideoMessage()
		v.ContextInfo, v.Caption = nil, nil
	default:
		return errors.New("only stickers and GIFs can be sent from the picker")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	text, preview := extractMessageContent(&pm)
	mediaType, media := extractMedia(&pm)
	stored := Message{Id: sm.getClient().GenerateMessageID(), Text: text, MediaType: mediaType, Media: media}
	sm.copyCachedMedia(m.Id, stored.Id)
	_, err = sm.sendTracked(ctx, jid, &pm, stored, preview)
	return err
}

// ffmpeg runs ffmpeg quietly and returns its error output on failure.
func ffmpeg(ctx context.Context, args ...string) error {
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("ffmpeg not found (needed to make stickers and GIFs)")
	}
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndexByte(msg, '\n'); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("ffmpeg: %s", firstNonEmpty(msg, err.Error()))
	}
	return nil
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

// stickerMaxBytes is WhatsApp's limit for static stickers.
const stickerMaxBytes = 100 << 10

// MakeSticker converts an image file into a WhatsApp sticker: a 512×512
// WebP with the picture centred on a transparent background.
func MakeSticker(ctx context.Context, src string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "whatsapp-tui-sticker-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	out := filepath.Join(dir, "sticker.webp")
	vf := "scale=512:512:force_original_aspect_ratio=decrease,format=rgba," +
		"pad=512:512:(ow-iw)/2:(oh-ih)/2:color=0x00000000"
	for _, q := range []string{"80", "60", "40"} { // shrink until it fits
		if err := ffmpeg(ctx, "-i", src, "-frames:v", "1", "-vf", vf,
			"-c:v", "libwebp", "-quality", q, "-compression_level", "6", out); err != nil {
			return nil, err
		}
		data, err := os.ReadFile(out)
		if err != nil {
			return nil, err
		}
		if len(data) <= stickerMaxBytes {
			return data, nil
		}
	}
	return nil, errors.New("the sticker is too detailed to fit WhatsApp's 100 KB limit")
}

// SendNewSticker makes a sticker from an image file and sends it.
func (sm *SessionManager) SendNewSticker(ctx context.Context, chat, src string) error {
	data, err := MakeSticker(ctx, src)
	if err != nil {
		return err
	}
	st := &waE2E.StickerMessage{
		Mimetype: proto.String("image/webp"),
		Width:    proto.Uint32(512),
		Height:   proto.Uint32(512),
	}
	return sm.uploadAndSend(ctx, chat, data, whatsmeow.MediaImage, &waE2E.Message{StickerMessage: st},
		func(up whatsmeow.UploadResponse) {
			st.URL, st.DirectPath = proto.String(up.URL), proto.String(up.DirectPath)
			st.MediaKey, st.FileEncSHA256, st.FileSHA256 = up.MediaKey, up.FileEncSHA256, up.FileSHA256
			st.FileLength = proto.Uint64(up.FileLength)
		})
}

// gifMaxSeconds caps GIF length, like WhatsApp's GIF maker.
const gifMaxSeconds = 15

// MakeGIF converts a .gif/.mp4/.webm (or any video) into the silent MP4
// WhatsApp plays as a GIF, plus a JPEG thumbnail and its size.
func MakeGIF(ctx context.Context, src string) (mp4, thumb []byte, width, height, seconds int, err error) {
	dir, err := os.MkdirTemp("", "whatsapp-tui-gif-")
	if err != nil {
		return nil, nil, 0, 0, 0, err
	}
	defer os.RemoveAll(dir)
	out, still := filepath.Join(dir, "gif.mp4"), filepath.Join(dir, "first.jpg")
	// even dimensions for yuv420p, at most 480 px wide
	if err = ffmpeg(ctx, "-i", src, "-t", strconv.Itoa(gifMaxSeconds), "-an",
		"-vf", "scale='trunc(min(480,iw)/2)*2':-2:flags=lanczos,fps=24",
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "26", "-pix_fmt", "yuv420p",
		"-movflags", "+faststart", out); err != nil {
		return
	}
	if err = ffmpeg(ctx, "-i", out, "-frames:v", "1", still); err != nil {
		return
	}
	if mp4, err = os.ReadFile(out); err != nil {
		return
	}
	frame, err := os.ReadFile(still)
	if err != nil {
		return
	}
	img, _, err := image.Decode(bytes.NewReader(frame))
	if err != nil {
		return nil, nil, 0, 0, 0, fmt.Errorf("gif thumbnail: %w", err)
	}
	width, height = img.Bounds().Dx(), img.Bounds().Dy()
	var tb bytes.Buffer
	if err = jpeg.Encode(&tb, fit(img, thumbMaxSide), &jpeg.Options{Quality: 70}); err != nil {
		return
	}
	seconds = videoSeconds(ctx, out)
	return mp4, tb.Bytes(), width, height, seconds, nil
}

// videoSeconds asks ffprobe for a video's length (0 if unknown).
func videoSeconds(ctx context.Context, path string) int {
	bin, err := exec.LookPath("ffprobe")
	if err != nil {
		return 0
	}
	out, err := exec.CommandContext(ctx, bin, "-v", "error", "-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil {
		return 0
	}
	return int(f + 0.5)
}

// SendNewGIF converts a file to a WhatsApp GIF and sends it.
func (sm *SessionManager) SendNewGIF(ctx context.Context, chat, src, caption string) error {
	mp4, thumb, w, h, secs, err := MakeGIF(ctx, src)
	if err != nil {
		return err
	}
	v := &waE2E.VideoMessage{
		Mimetype:      proto.String("video/mp4"),
		GifPlayback:   proto.Bool(true),
		Width:         proto.Uint32(uint32(w)),
		Height:        proto.Uint32(uint32(h)),
		Seconds:       proto.Uint32(uint32(secs)),
		JPEGThumbnail: thumb,
	}
	if caption != "" {
		v.Caption = proto.String(caption)
	}
	return sm.uploadAndSend(ctx, chat, mp4, whatsmeow.MediaVideo, &waE2E.Message{VideoMessage: v},
		func(up whatsmeow.UploadResponse) {
			v.URL, v.DirectPath = proto.String(up.URL), proto.String(up.DirectPath)
			v.MediaKey, v.FileEncSHA256, v.FileSHA256 = up.MediaKey, up.FileEncSHA256, up.FileSHA256
			v.FileLength = proto.Uint64(up.FileLength)
		})
}

// uploadAndSend shows msg as pending (with data cached for display),
// uploads data, fills the upload info in with setUpload, and sends it.
func (sm *SessionManager) uploadAndSend(ctx context.Context, chat string, data []byte, mt whatsmeow.MediaType,
	msg *waE2E.Message, setUpload func(whatsmeow.UploadResponse)) error {
	client := sm.getClient()
	if client == nil || client.Store.ID == nil {
		return errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	id := client.GenerateMessageID()
	if dir, err := cacheDir("media"); err == nil {
		_ = os.WriteFile(filepath.Join(dir, safeName(id)), data, 0o600)
	}
	text, preview := extractMessageContent(msg)
	mediaType, media := extractMedia(msg)
	sm.storeSent(Message{
		Id: id, ChatId: jid.String(), FromMe: true, Timestamp: uint64(time.Now().Unix()),
		Text: text, ContactId: client.Store.ID.ToNonAD().String(), ContactName: "Me", ContactShort: "Me",
		MediaType: mediaType, Media: media, Status: StatusPending,
	}, preview)
	fail := func(err error) error {
		_ = sm.db.SetStatus(id, StatusFailed)
		sm.scheduleChatRefresh(jid.String())
		return err
	}
	if !client.IsConnected() {
		return fail(errors.New("not connected to WhatsApp"))
	}
	up, err := client.Upload(ctx, data, mt)
	if err != nil {
		return fail(fmt.Errorf("upload: %w", err))
	}
	setUpload(up)
	if _, media := extractMedia(msg); media != nil {
		_ = sm.db.SetMedia(id, media)
	}
	if _, err := client.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id}); err != nil {
		return fail(fmt.Errorf("send: %w", err))
	}
	_ = sm.db.SetStatus(id, sm.sentStatus(jid.String()))
	sm.scheduleChatRefresh(jid.String())
	return nil
}
