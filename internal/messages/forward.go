package messages

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// forwardProto builds the message to send when forwarding m: the original
// media (already on WhatsApp's servers) or text, marked as forwarded.
func forwardProto(m Message) (*waE2E.Message, error) {
	score := uint32(1)
	if m.Forwarded {
		score = 2 // forwarded again; 5+ shows "forwarded many times"
	}
	ci := &waE2E.ContextInfo{IsForwarded: proto.Bool(true), ForwardingScore: proto.Uint32(score)}

	if len(m.Media) == 0 {
		if m.MediaType != "" || isMediaTag(m.Text) {
			return nil, errors.New("this media arrived before media support and can't be forwarded")
		}
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(m.Text), ContextInfo: ci,
		}}, nil
	}
	var pm waE2E.Message
	if err := proto.Unmarshal(m.Media, &pm); err != nil {
		return nil, fmt.Errorf("decode media: %w", err)
	}
	caption := messageCaption(m.Text)
	switch {
	case pm.GetImageMessage() != nil:
		im := pm.GetImageMessage()
		if im.GetDirectPath() == "" {
			return nil, errors.New("this image never finished uploading")
		}
		im.ContextInfo = ci
		im.Caption = optString(caption)
	case pm.GetVideoMessage() != nil:
		v := pm.GetVideoMessage()
		v.ContextInfo = ci
		v.Caption = optString(caption)
	case pm.GetStickerMessage() != nil:
		pm.GetStickerMessage().ContextInfo = ci
	case pm.GetDocumentMessage() != nil:
		pm.GetDocumentMessage().ContextInfo = ci
	case pm.GetAudioMessage() != nil:
		pm.GetAudioMessage().ContextInfo = ci
	default:
		return nil, errors.New("this message can't be forwarded")
	}
	return &pm, nil
}

// isMediaTag reports texts like "[IMAGE] …" / "[STICKER]" that stand for
// media we have no download info for.
func isMediaTag(text string) bool {
	for _, tag := range []string{"[IMAGE]", "[VIDEO]", "[GIF]", "[STICKER]", "[DOCUMENT]", "[AUDIO]", "[VOICE NOTE]"} {
		if strings.HasPrefix(text, tag) {
			return true
		}
	}
	return false
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return proto.String(s)
}

// ForwardMessage forwards msg to each chat in to. It returns the first
// error but tries every recipient. Safe from any goroutine.
func (sm *SessionManager) ForwardMessage(ctx context.Context, msgID string, to []string) error {
	m, err := sm.db.GetMessage(msgID)
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	var firstErr error
	for _, chat := range to {
		jid, err := types.ParseJID(chat)
		if err != nil {
			firstErr = errors.Join(firstErr, fmt.Errorf("invalid chat %q", chat))
			continue
		}
		msg, err := forwardProto(m) // fresh copy per recipient
		if err != nil {
			return err
		}
		text, preview := extractMessageContent(msg)
		mediaType, media := extractMedia(msg)
		stored := Message{Text: text, MediaType: mediaType, Media: media, Forwarded: true}
		if mediaType != "" {
			// reuse the cached file so the copy shows without a download
			stored.Id = sm.getClient().GenerateMessageID()
			sm.copyCachedMedia(m.Id, stored.Id)
		}
		if _, err := sm.sendTracked(ctx, jid, msg, stored, preview); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("forward to %s: %w", sm.ChatName(ctx, chat), err)
		}
	}
	return firstErr
}

func (sm *SessionManager) copyCachedMedia(from, to string) {
	dir, err := cacheDir("media")
	if err != nil {
		return
	}
	if data, err := os.ReadFile(filepath.Join(dir, safeName(from))); err == nil {
		_ = os.WriteFile(filepath.Join(dir, safeName(to)), data, 0o600)
	}
}
