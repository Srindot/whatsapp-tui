package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// contextInfo returns the ContextInfo (reply/forward metadata) of the
// message's main part, if any.
func contextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	switch {
	case msg == nil:
		return nil
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetContactMessage() != nil:
		return msg.GetContactMessage().GetContextInfo()
	case msg.GetLocationMessage() != nil:
		return msg.GetLocationMessage().GetContextInfo()
	}
	return nil
}

// quoteOf fills in the reply fields of m from msg.
func quoteOf(m *Message, msg *waE2E.Message) {
	ci := contextInfo(msg)
	if ci.GetStanzaID() == "" {
		return
	}
	m.QuotedID = ci.GetStanzaID()
	m.QuotedSender = ci.GetParticipant()
	m.QuotedText, _ = extractMessageContent(ci.GetQuotedMessage())
	m.Forwarded = m.Forwarded || ci.GetIsForwarded()
}

// handleReaction stores a reaction (from a live message or history) and
// refreshes the chat if it's open.
func (sm *SessionManager) handleReaction(chat string, sender types.JID, fromMe bool, r *waE2E.ReactionMessage) {
	target := r.GetKey().GetID()
	if target == "" {
		return
	}
	who := ""
	if !fromMe {
		who = sender.ToNonAD().String()
	}
	if err := sm.db.SetReaction(target, who, r.GetText(), r.GetSenderTimestampMS()); err != nil {
		sm.debugf("store reaction: %v", err)
		return
	}
	sm.refreshIfOpen(chat)
}

// refreshIfOpen re-sends the open chat's messages when it is chat.
func (sm *SessionManager) refreshIfOpen(chat string) {
	sm.mu.RLock()
	current := sm.currentReceiver
	sm.mu.RUnlock()
	if current == chat {
		sm.uiHandler.NewScreen(sm.getMessages(chat))
	}
}

// attachReactions fills in Message.Reactions.
func (sm *SessionManager) attachReactions(msgs []Message) {
	ids := make([]string, len(msgs))
	for i, m := range msgs {
		ids[i] = m.Id
	}
	byID, err := sm.db.GetReactions(ids)
	if err != nil {
		sm.debugf("load reactions: %v", err)
	}
	for i := range msgs {
		msgs[i].Reactions = byID[msgs[i].Id]
	}
}

// ownJID is this account's user JID (no device part).
func (sm *SessionManager) ownJID() (types.JID, error) {
	client := sm.getClient()
	if client == nil || client.Store.ID == nil {
		return types.JID{}, errors.New("not logged in")
	}
	return client.Store.ID.ToNonAD(), nil
}

// senderJID is the author of m, as needed for message keys.
func (sm *SessionManager) senderJID(m Message) (types.JID, error) {
	if m.FromMe {
		return sm.ownJID()
	}
	jid, err := types.ParseJID(m.ContactId)
	if err != nil {
		return types.JID{}, fmt.Errorf("invalid sender %q: %w", m.ContactId, err)
	}
	return jid.ToNonAD(), nil
}

// quotedProto rebuilds the quoted message for a reply's preview.
func quotedProto(m Message) *waE2E.Message {
	if len(m.Media) > 0 {
		var pm waE2E.Message
		if proto.Unmarshal(m.Media, &pm) == nil {
			return &pm
		}
	}
	text := m.Text
	if label, rest := splitMediaTag(text); label {
		text = rest
	}
	return &waE2E.Message{Conversation: proto.String(text)}
}

// splitMediaTag strips a leading "[IMAGE]"-style tag.
func splitMediaTag(text string) (bool, string) {
	if strings.HasPrefix(text, "[") {
		if i := strings.Index(text, "]"); i > 0 && strings.ToUpper(text[:i]) == text[:i] {
			return true, strings.TrimSpace(text[i+1:])
		}
	}
	return false, text
}

// SendReply sends text to chat as a reply to quoted. When quoted is from
// another chat (a group), this is a private reply. Safe from any goroutine.
func (sm *SessionManager) SendReply(ctx context.Context, chat, text string, quoted Message, mentions []string) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	jid, err := types.ParseJID(chat)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	author, err := sm.senderJID(quoted)
	if err != nil {
		return err
	}
	mentions = sm.expandMentions(ctx, chat, text, mentions)
	ci := &waE2E.ContextInfo{
		StanzaID:      proto.String(quoted.Id),
		Participant:   proto.String(author.String()),
		QuotedMessage: quotedProto(quoted),
		MentionedJID:  mentions,
	}
	if quoted.ChatId != chat {
		ci.RemoteJID = proto.String(quoted.ChatId)
	}
	msg := &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: ci}}
	stored := Message{Text: text}
	quoteOf(&stored, msg)
	if _, err := sm.sendTracked(ctx, jid, msg, stored, truncatePreview(text)); err != nil {
		return fmt.Errorf("send reply: %w", err)
	}
	return nil
}

// SendReaction reacts to m with emoji; an empty emoji removes the reaction.
func (sm *SessionManager) SendReaction(ctx context.Context, m Message, emoji string) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(m.ChatId)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	author, err := sm.senderJID(m)
	if err != nil {
		return err
	}
	if _, err := client.SendMessage(ctx, chat, client.BuildReaction(chat, author, m.Id, emoji)); err != nil {
		return fmt.Errorf("send reaction: %w", err)
	}
	if err := sm.db.SetReaction(m.Id, "", emoji, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save reaction: %w", err)
	}
	sm.refreshIfOpen(m.ChatId)
	return nil
}

// DirectChat returns the one-to-one chat JID for a group member, mapping a
// hidden LID to their phone number when known.
func (sm *SessionManager) DirectChat(ctx context.Context, sender string) string {
	jid, err := types.ParseJID(sender)
	if err != nil {
		return sender
	}
	jid = jid.ToNonAD()
	if client := sm.getClient(); client != nil && client.Store.LIDs != nil && jid.Server == types.HiddenUserServer {
		if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
			return pn.ToNonAD().String()
		}
	}
	return jid.String()
}

// ChatName is the display name for a chat JID.
func (sm *SessionManager) ChatName(ctx context.Context, jidStr string) string {
	sm.mu.RLock()
	conv := sm.convByJID[jidStr]
	sm.mu.RUnlock()
	if conv != nil && !isPlaceholderName(conv.Name, strings.Split(jidStr, "@")[0]) {
		return conv.Name
	}
	if jid, err := types.ParseJID(jidStr); err == nil {
		return sm.contactName(ctx, jid)
	}
	return jidStr
}
