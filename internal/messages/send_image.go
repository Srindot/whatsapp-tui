package messages

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decoders for pasted images
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	_ "golang.org/x/image/bmp"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	"google.golang.org/protobuf/proto"
)

// Limits for sent images, matching what WhatsApp's apps produce.
const (
	sendMaxSide  = 2560
	thumbMaxSide = 100
	sendQuality  = 88
)

// SendImage sends an image (any common format; it's re-encoded as JPEG, as
// WhatsApp expects) with an optional caption. It blocks while uploading, so
// call it off the UI loop. Safe to call from any goroutine.
func (sm *SessionManager) SendImage(ctx context.Context, chat string, data []byte, caption string) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() || client.Store.ID == nil {
		return errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}

	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	full := flatten(fit(src, sendMaxSide))
	var body bytes.Buffer
	if err := jpeg.Encode(&body, full, &jpeg.Options{Quality: sendQuality}); err != nil {
		return fmt.Errorf("encode image: %w", err)
	}
	var thumb bytes.Buffer
	if err := jpeg.Encode(&thumb, fit(full, thumbMaxSide), &jpeg.Options{Quality: 70}); err != nil {
		return fmt.Errorf("encode thumbnail: %w", err)
	}

	// Show it right away as "sending": the local copy is cached under the
	// message ID, so the chat renders the real image while it uploads.
	b := full.Bounds()
	im := &waE2E.ImageMessage{
		Mimetype:      proto.String("image/jpeg"),
		Width:         proto.Uint32(uint32(b.Dx())),
		Height:        proto.Uint32(uint32(b.Dy())),
		JPEGThumbnail: thumb.Bytes(),
	}
	if caption != "" {
		im.Caption = proto.String(caption)
	}
	msg := &waE2E.Message{ImageMessage: im}
	id := client.GenerateMessageID()
	if dir, err := cacheDir("media"); err == nil {
		_ = os.WriteFile(filepath.Join(dir, safeName(id)), body.Bytes(), 0o600)
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

	up, err := client.Upload(ctx, body.Bytes(), whatsmeow.MediaImage)
	if err != nil {
		return fail(fmt.Errorf("upload image: %w", err))
	}
	im.URL = proto.String(up.URL)
	im.DirectPath = proto.String(up.DirectPath)
	im.MediaKey = up.MediaKey
	im.FileEncSHA256 = up.FileEncSHA256
	im.FileSHA256 = up.FileSHA256
	im.FileLength = proto.Uint64(up.FileLength)
	// Store the uploaded version, so it can be downloaded, copied or resent.
	if _, media := extractMedia(msg); media != nil {
		_ = sm.db.SetMedia(id, media)
	}
	if _, err := client.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id}); err != nil {
		return fail(fmt.Errorf("send image: %w", err))
	}
	_ = sm.db.SetStatus(id, sm.sentStatus(jid.String()))
	sm.scheduleChatRefresh(jid.String())
	return nil
}

// fit scales img down so its longest side is at most max pixels.
func fit(img image.Image, max int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return img
	}
	if w >= h {
		w, h = max, h*max/w
	} else {
		w, h = w*max/h, max
	}
	dst := image.NewRGBA(image.Rect(0, 0, max1(w), max1(h)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}

func max1(v int) int {
	if v < 1 {
		return 1
	}
	return v
}

// flatten puts transparent areas on white (JPEG has no alpha; screenshots of
// windows with rounded corners would otherwise get black corners).
func flatten(img image.Image) *image.RGBA {
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Over)
	return dst
}

// maxDocumentSize caps documents read into memory for sending.
const maxDocumentSize = 100 << 20

// SendDocument sends a file as a WhatsApp document, with an optional
// caption. Like SendImage it shows as "sending" right away and stays in the
// chat as "not sent" if it fails. Safe to call from any goroutine.
func (sm *SessionManager) SendDocument(ctx context.Context, chat, path, caption string) error {
	client := sm.getClient()
	if client == nil || client.Store.ID == nil {
		return errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a folder", filepath.Base(path))
	}
	if info.Size() > maxDocumentSize {
		return fmt.Errorf("%s is too large (%d MB, max %d MB)", filepath.Base(path), info.Size()>>20, maxDocumentSize>>20)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	name := filepath.Base(path)
	doc := &waE2E.DocumentMessage{
		FileName: proto.String(name),
		Title:    proto.String(name),
		Mimetype: proto.String(detectMIME(path, data)),
	}
	if caption != "" {
		doc.Caption = proto.String(caption)
	}
	msg := &waE2E.Message{DocumentMessage: doc}

	id := client.GenerateMessageID()
	if dir, err := cacheDir("media"); err == nil {
		_ = os.WriteFile(filepath.Join(dir, safeName(id)), data, 0o600)
	}
	text, preview := extractMessageContent(msg)
	if caption != "" {
		text += "\n" + caption
	}
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
	up, err := client.Upload(ctx, data, whatsmeow.MediaDocument)
	if err != nil {
		return fail(fmt.Errorf("upload %s: %w", name, err))
	}
	doc.URL = proto.String(up.URL)
	doc.DirectPath = proto.String(up.DirectPath)
	doc.MediaKey = up.MediaKey
	doc.FileEncSHA256 = up.FileEncSHA256
	doc.FileSHA256 = up.FileSHA256
	doc.FileLength = proto.Uint64(up.FileLength)
	if _, media := extractMedia(msg); media != nil {
		_ = sm.db.SetMedia(id, media)
	}
	if _, err := client.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: id}); err != nil {
		return fail(fmt.Errorf("send %s: %w", name, err))
	}
	_ = sm.db.SetStatus(id, sm.sentStatus(jid.String()))
	sm.scheduleChatRefresh(jid.String())
	return nil
}
