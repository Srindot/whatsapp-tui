package messages

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waWeb"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// Delivery states of your own messages, ordered so a higher value is
// further along. Receipts only ever move a message forward.
const (
	StatusFailed    = -1 // sending failed; can be retried
	StatusUnknown   = 0  // incoming, or sent before statuses were tracked
	StatusPending   = 1  // being sent
	StatusSent      = 2  // accepted by the WhatsApp server
	StatusDelivered = 3  // reached their phone
	StatusRead      = 4  // they opened it
	StatusPlayed    = 5  // voice note / video played
)

// statusFromHistory maps a history message's status. Errors and pending
// states in old history aren't shown: they'd be stale.
func statusFromHistory(s waWeb.WebMessageInfo_Status) int {
	switch s {
	case waWeb.WebMessageInfo_SERVER_ACK:
		return StatusSent
	case waWeb.WebMessageInfo_DELIVERY_ACK:
		return StatusDelivered
	case waWeb.WebMessageInfo_READ:
		return StatusRead
	case waWeb.WebMessageInfo_PLAYED:
		return StatusPlayed
	}
	return StatusUnknown
}

// handleReceipt moves your messages forward when they are delivered, read
// or played. In groups the first receipt counts (like "read by someone").
func (sm *SessionManager) handleReceipt(evt *events.Receipt) {
	var st int
	switch evt.Type {
	case types.ReceiptTypeDelivered:
		st = StatusDelivered
	case types.ReceiptTypeRead:
		st = StatusRead
	case types.ReceiptTypePlayed:
		st = StatusPlayed
	default:
		return // our own read receipts, retries, server errors…
	}
	if evt.IsFromMe {
		return // our other devices reading their chats
	}
	sm.debugf("receipt %q from %s for %d message(s) in %s", evt.Type, evt.Sender, len(evt.MessageIDs), evt.Chat)
	changed := false
	for _, id := range evt.MessageIDs {
		ok, err := sm.db.AdvanceStatus(id, st)
		if err != nil {
			sm.debugf("receipt for %s: %v", id, err)
		}
		changed = changed || ok
	}
	if changed {
		sm.canonicalSource(context.Background(), &evt.MessageSource) // LID chats
		sm.scheduleChatRefresh(evt.Chat.String())
	}
}

// scheduleChatRefresh re-sends the open chat shortly after status changes
// stop arriving (receipts often come in bursts).
func (sm *SessionManager) scheduleChatRefresh(chat string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.currentReceiver != chat {
		return
	}
	if sm.refreshTimer != nil {
		sm.refreshTimer.Reset(150 * time.Millisecond)
		return
	}
	sm.refreshTimer = time.AfterFunc(150*time.Millisecond, func() {
		sm.mu.Lock()
		sm.refreshTimer = nil
		current := sm.currentReceiver
		sm.mu.Unlock()
		if current != "" {
			sm.uiHandler.NewScreen(sm.getMessages(current))
		}
	})
}

// sendTracked shows msg in the chat as pending, sends it, then marks it
// sent or failed. stored is the local copy (its ID is filled in).
func (sm *SessionManager) sendTracked(ctx context.Context, jid types.JID, msg *waE2E.Message, stored Message, preview string) (Message, error) {
	client := sm.getClient()
	if client == nil || client.Store.ID == nil {
		return stored, errors.New("not connected to WhatsApp")
	}
	if stored.Id == "" {
		stored.Id = client.GenerateMessageID()
	}
	stored.ChatId, stored.FromMe = jid.String(), true
	if stored.Timestamp == 0 {
		stored.Timestamp = uint64(time.Now().Unix())
	}
	stored.ContactId = client.Store.ID.ToNonAD().String()
	stored.ContactName, stored.ContactShort = "Me", "Me"
	stored.Status = StatusPending
	sm.storeSent(stored, preview)

	var err error
	if !client.IsConnected() {
		err = errors.New("not connected to WhatsApp")
	} else {
		_, err = client.SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: stored.Id})
	}
	final := sm.sentStatus(stored.ChatId)
	if err != nil {
		final = StatusFailed
	}
	if serr := sm.db.SetStatus(stored.Id, final); serr != nil {
		sm.debugf("save status: %v", serr)
	}
	stored.Status = final
	sm.scheduleChatRefresh(stored.ChatId)
	return stored, err
}

// ResendMessage retries a message that failed to send. Safe from any
// goroutine.
func (sm *SessionManager) ResendMessage(ctx context.Context, msgID string) error {
	m, err := sm.db.GetMessage(msgID)
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	if !m.FromMe || m.Status != StatusFailed {
		return errors.New("only messages that failed to send can be resent")
	}
	jid, err := types.ParseJID(m.ChatId)
	if err != nil {
		return fmt.Errorf("invalid JID: %w", err)
	}
	var msg *waE2E.Message
	switch {
	case len(m.Media) > 0:
		msg = &waE2E.Message{}
		if err := proto.Unmarshal(m.Media, msg); err != nil {
			return fmt.Errorf("decode media: %w", err)
		}
		if im := msg.GetImageMessage(); im != nil && im.GetDirectPath() == "" {
			return errors.New("the image never finished uploading; paste it again to resend")
		}
		if im := msg.GetImageMessage(); im != nil {
			if caption := messageCaption(m.Text); caption != "" {
				im.Caption = proto.String(caption)
			}
		}
	case m.QuotedID != "":
		msg = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text: proto.String(m.Text),
			ContextInfo: &waE2E.ContextInfo{
				StanzaID:      proto.String(m.QuotedID),
				Participant:   proto.String(m.QuotedSender),
				QuotedMessage: &waE2E.Message{Conversation: proto.String(m.QuotedText)},
			},
		}}
	case m.Forwarded:
		if msg, err = forwardProto(m); err != nil {
			return err
		}
	default:
		msg = &waE2E.Message{Conversation: proto.String(m.Text)}
	}
	if err := sm.db.SetStatus(m.Id, StatusPending); err != nil {
		return err
	}
	sm.scheduleChatRefresh(m.ChatId)
	final := sm.sentStatus(m.ChatId)
	_, err = sm.getClient().SendMessage(ctx, jid, msg, whatsmeow.SendRequestExtra{ID: m.Id})
	if err != nil {
		final = StatusFailed
	}
	_ = sm.db.SetStatus(m.Id, final)
	sm.scheduleChatRefresh(m.ChatId)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	return nil
}

// messageCaption strips the "[IMAGE] " tag from a stored image text.
func messageCaption(text string) string {
	_, rest := splitMediaTag(text)
	return rest
}

// isSelfChat reports whether chat is your own "message yourself" chat.
func (sm *SessionManager) isSelfChat(chat string) bool {
	client := sm.getClient()
	if client == nil || client.Store == nil || client.Store.ID == nil {
		return false
	}
	jid, err := types.ParseJID(chat)
	if err != nil || jid.Server == types.GroupServer {
		return false
	}
	return jid.User == client.Store.ID.User || (!client.Store.LID.IsEmpty() && jid.User == client.Store.LID.User)
}

// IsSelfChat is isSelfChat for the UI.
func (sm *SessionManager) IsSelfChat(chat string) bool { return sm.isSelfChat(chat) }

// sentStatus is the state of a message the server accepted: in your own
// chat that's read (WhatsApp sends no receipt there).
func (sm *SessionManager) sentStatus(chat string) int {
	if sm.isSelfChat(chat) {
		return StatusRead
	}
	return StatusSent
}

// selfChatRead shows your messages in your own chat as read, including ones
// stored before this was handled or sent from another device.
func (sm *SessionManager) selfChatRead(chat string, msgs []Message) {
	if !sm.isSelfChat(chat) {
		return
	}
	for i, m := range msgs {
		if m.FromMe && (m.Status == StatusUnknown || m.Status == StatusSent || m.Status == StatusDelivered) {
			msgs[i].Status = StatusRead
		}
	}
}

// ReadReceiptsOff reports whether your WhatsApp privacy setting turns read
// receipts off. WhatsApp then doesn't send you other people's read receipts
// either (except in groups), so one-to-one chats stop at "delivered".
func (sm *SessionManager) ReadReceiptsOff(ctx context.Context) (bool, error) {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return false, errors.New("not connected to WhatsApp")
	}
	settings, err := client.TryFetchPrivacySettings(ctx, false)
	if err != nil {
		return false, fmt.Errorf("privacy settings: %w", err)
	}
	return settings.ReadReceipts == types.PrivacySettingNone, nil
}
