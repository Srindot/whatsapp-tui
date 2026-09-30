package messages

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// RevokeWindow is how long after sending you can still delete a message for
// everyone (WhatsApp's limit is about two and a half days).
const RevokeWindow = 60 * time.Hour

// Notes shown in place of deleted messages.
const (
	noteYouDeleted = "🚫 You deleted this message"
	noteDeleted    = "🚫 This message was deleted"
)

// CanDeleteForEveryone reports whether m can still be deleted for everyone.
func CanDeleteForEveryone(m Message) bool {
	return m.FromMe && m.Status != StatusFailed && m.Status != StatusPending &&
		m.Text != noteYouDeleted &&
		time.Since(time.Unix(int64(m.Timestamp), 0)) < RevokeWindow
}

// buildDeleteForMe is the app state patch that deletes a message on all your
// devices (whatsmeow has no builder for it; the layout matches its parser).
func buildDeleteForMe(chat, sender types.JID, id types.MessageID, fromMe bool, ts time.Time) appstate.PatchInfo {
	isFromMe, senderStr := "0", "0"
	if fromMe {
		isFromMe = "1"
	} else if chat.Server == types.GroupServer {
		senderStr = sender.ToNonAD().String()
	}
	return appstate.PatchInfo{
		Type: appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index:   []string{appstate.IndexDeleteMessageForMe, chat.String(), id, isFromMe, senderStr},
			Version: 3,
			Value: &waSyncAction.SyncActionValue{
				DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
					DeleteMedia:      proto.Bool(true),
					MessageTimestamp: proto.Int64(ts.Unix()),
				},
			},
		}},
	}
}

// DeleteForMe deletes a message from your devices only.
func (sm *SessionManager) DeleteForMe(ctx context.Context, m Message) error {
	localOnly := m.Status == StatusFailed || m.Status == StatusPending // never reached WhatsApp
	client := sm.getClient()
	if !localOnly && (client == nil || !client.IsConnected()) {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(m.ChatId)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	var sender types.JID
	if !m.FromMe {
		if sender, err = types.ParseJID(m.ContactId); err != nil {
			sender = chat
		}
	}
	if !localOnly {
		patch := buildDeleteForMe(chat, sender, m.Id, m.FromMe, time.Unix(int64(m.Timestamp), 0))
		if err := client.SendAppState(ctx, patch); err != nil {
			return fmt.Errorf("delete: %w", err)
		}
	}
	if err := sm.db.DeleteMessage(m.Id); err != nil {
		return fmt.Errorf("delete locally: %w", err)
	}
	sm.refreshIfOpen(m.ChatId)
	return nil
}

// DeleteForEveryone deletes one of your messages for everyone in the chat.
func (sm *SessionManager) DeleteForEveryone(ctx context.Context, m Message) error {
	if !CanDeleteForEveryone(m) {
		return errors.New("only your own messages from the last 2½ days can be deleted for everyone")
	}
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(m.ChatId)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	if _, err := client.SendMessage(ctx, chat, client.BuildRevoke(chat, types.EmptyJID, m.Id)); err != nil {
		return fmt.Errorf("delete for everyone: %w", err)
	}
	if err := sm.db.MarkRevoked(m.Id, noteYouDeleted); err != nil {
		return fmt.Errorf("delete locally: %w", err)
	}
	sm.refreshIfOpen(m.ChatId)
	return nil
}

// DeleteChat deletes a whole chat (on all your devices).
func (sm *SessionManager) DeleteChat(ctx context.Context, chatStr string) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected to WhatsApp")
	}
	chat, err := types.ParseJID(chatStr)
	if err != nil {
		return fmt.Errorf("invalid chat: %w", err)
	}
	lastTS, lastKey := sm.lastMessageKey(client, chat)
	if err := client.SendAppState(ctx, appstate.BuildDeleteChat(chat, lastTS, lastKey, true)); err != nil {
		return fmt.Errorf("delete chat: %w", err)
	}
	sm.removeChatLocally(chatStr)
	return nil
}

// lastMessageKey identifies a chat's newest message, which app state
// changes about the whole chat (delete, mark read) refer to.
func (sm *SessionManager) lastMessageKey(client *whatsmeow.Client, chat types.JID) (time.Time, *waCommon.MessageKey) {
	latest, err := sm.db.GetLatestMessages(chat.String(), 1)
	if err != nil || len(latest) == 0 {
		return time.Now(), client.BuildMessageKey(chat, chat, "")
	}
	l := latest[0]
	sender := chat
	if l.FromMe {
		if own, err := sm.ownJID(); err == nil {
			sender = own
		}
	} else if s, err := types.ParseJID(l.ContactId); err == nil {
		sender = s
	}
	return time.Unix(int64(l.Timestamp), 0), client.BuildMessageKey(chat, sender, l.Id)
}

// removeChatLocally drops a chat from the database and the chat list.
func (sm *SessionManager) removeChatLocally(chat string) {
	if err := sm.db.DeleteChat(chat); err != nil {
		sm.debugf("delete chat %s: %v", chat, err)
	}
	sm.mu.Lock()
	if conv := sm.convByJID[chat]; conv != nil {
		heap.Remove(&sm.priorityQueue, conv.Index)
		delete(sm.convByJID, chat)
	}
	if sm.currentReceiver == chat {
		sm.currentReceiver = ""
	}
	list := sm.snapshotPQ()
	sm.mu.Unlock()
	sm.uiHandler.UpdateChatList(list)
}

// handleRevoke shows "This message was deleted" when someone deletes a
// message for everyone.
func (sm *SessionManager) handleRevoke(chat string, pm *waE2E.ProtocolMessage, fromMe bool) {
	id := pm.GetKey().GetID()
	if id == "" {
		return
	}
	note := noteDeleted
	if fromMe {
		note = noteYouDeleted
	}
	if err := sm.db.MarkRevoked(id, note); err != nil {
		sm.debugf("revoke %s: %v", id, err)
		return
	}
	sm.refreshIfOpen(chat)
}

// handleDeleteForMe / handleDeleteChat apply deletions made on your phone.
func (sm *SessionManager) handleDeleteForMe(evt *events.DeleteForMe) {
	if err := sm.db.DeleteMessage(evt.MessageID); err == nil {
		sm.refreshIfOpen(sm.pnForLID(context.Background(), evt.ChatJID).String())
	}
}

func (sm *SessionManager) handleDeleteChat(evt *events.DeleteChat) {
	sm.removeChatLocally(sm.pnForLID(context.Background(), evt.JID.ToNonAD()).String())
}
