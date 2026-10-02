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

// EditWindow is how long after sending WhatsApp lets you edit a message.
const EditWindow = 15 * time.Minute

// CanEdit says whether m can still be edited, and if not, why.
func CanEdit(m Message) (bool, string) {
	switch {
	case !m.FromMe:
		return false, "you can only edit your own messages"
	case m.Text == noteYouDeleted || m.Text == noteDeleted:
		return false, "deleted messages can't be edited"
	case m.MediaType != "":
		return false, "only text messages can be edited"
	case m.Status == StatusFailed || m.Status == StatusPending:
		return false, "this message wasn't sent; press R to retry it"
	case time.Since(time.Unix(int64(m.Timestamp), 0)) >= EditWindow:
		return false, "WhatsApp only allows edits in the first 15 minutes"
	}
	return true, ""
}

// EditMessage changes the text of one of your messages, for everyone.
func (sm *SessionManager) EditMessage(ctx context.Context, m Message, text string, mentions []string) error {
	if ok, why := CanEdit(m); !ok {
		return errors.New(why)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return errors.New("a message can't be edited to nothing; use d to delete it")
	}
	if text == m.Text {
		return nil
	}
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(m.ChatId)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	mentions = sm.expandMentions(ctx, m.ChatId, text, sm.completeMentions(ctx, m.ChatId, mentions))
	content := &waE2E.Message{Conversation: proto.String(text)}
	if len(mentions) > 0 {
		content = &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(text),
			ContextInfo: &waE2E.ContextInfo{MentionedJID: mentions},
		}}
	}
	if _, err := client.SendMessage(ctx, chat, client.BuildEdit(chat, m.Id, content)); err != nil {
		return fmt.Errorf("edit: %w", err)
	}
	if _, err := sm.db.EditMessage(m.Id, text); err != nil {
		return fmt.Errorf("save the edit: %w", err)
	}
	sm.refreshIfOpen(m.ChatId)
	return nil
}

// completeMentions turns bare numbers (a mention kept from the message being
// edited) into full JIDs: the group member with that number (phone number or
// LID, as the group uses), else a phone number.
func (sm *SessionManager) completeMentions(ctx context.Context, chat string, mentions []string) []string {
	var members []GroupMember
	out := make([]string, 0, len(mentions))
	for _, m := range mentions {
		if strings.Contains(m, "@") || m == MentionAll {
			out = append(out, m)
			continue
		}
		if members == nil {
			members, _ = sm.GroupMembers(ctx, chat)
			if members == nil {
				members = []GroupMember{}
			}
		}
		full := types.NewJID(m, types.DefaultUserServer).String()
		for _, gm := range members {
			if strings.Split(gm.JID, "@")[0] == m {
				full = gm.JID
				break
			}
		}
		out = append(out, full)
	}
	return out
}

// editOf returns the target message and new text when msg is an edit.
func editOf(msg *waE2E.Message) (id, text string, ok bool) {
	pm := msg.GetProtocolMessage()
	if pm == nil || pm.GetType() != waE2E.ProtocolMessage_MESSAGE_EDIT {
		return "", "", false
	}
	text, _ = extractMessageContent(pm.GetEditedMessage())
	return pm.GetKey().GetID(), text, pm.GetKey().GetID() != "" && text != ""
}

// handleEdit applies an edit someone (or your phone) made.
func (sm *SessionManager) handleEdit(chat, id, text string) {
	changed, err := sm.db.EditMessage(id, text)
	if err != nil {
		sm.debugf("edit %s: %v", id, err)
		return
	}
	if changed {
		sm.refreshIfOpen(chat)
	}
}
