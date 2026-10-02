package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// setCurrentReceiver sets the currently selected chat and refreshes the
// message view. Automatically marks the chat as read in the background.
func (sm *SessionManager) setCurrentReceiver(id string) {
	sm.mu.Lock()
	sm.currentReceiver = id
	sm.mu.Unlock()
	screen := sm.getMessages(id)
	sm.uiHandler.NewScreen(screen)
	go sm.autoBackfill(id)
	// marking it read is up to the UI ("read"), which knows whether you're
	// actually looking at it
}

// markChatAsRead clears a chat's unread count (the UI calls it when you look
// at the chat), then tells WhatsApp: read receipts for the unread messages
// and a "chat read" sync so your phone and other devices clear it too.
// Safe for background use; network failures only go to the debug log.
func (sm *SessionManager) markChatAsRead(jidStr string) {
	sm.mu.Lock()
	conv := sm.convByJID[jidStr]
	if conv == nil || (conv.Unread == 0 && !conv.Mentioned) {
		sm.mu.Unlock()
		return
	}
	unread := int(conv.Unread)
	conv.Unread, conv.Mentioned = 0, false
	convCopy := *conv
	safeList := sm.snapshotPQ()
	sm.mu.Unlock()

	if err := sm.db.UpsertConversation(convCopy); err != nil {
		sm.debugf("mark read %s: %v", jidStr, err)
	}
	sm.uiHandler.UpdateChatList(safeList)
	if err := sm.sendRead(jidStr, unread); err != nil {
		sm.debugf("mark read %s: %v", jidStr, err)
	}
}

// maxReadReceipts caps how many unread messages get a read receipt.
const maxReadReceipts = 300

func (sm *SessionManager) sendRead(jidStr string, unread int) error {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return errors.New("not connected")
	}
	chat, err := types.ParseJID(jidStr)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	msgs, err := sm.db.GetLatestMessages(jidStr, min(max(unread, 1)*2+20, maxReadReceipts))
	if err != nil {
		return err
	}
	// the newest `unread` incoming messages, grouped by sender (WhatsApp
	// wants one receipt per sender)
	bySender := map[types.JID][]types.MessageID{}
	var order []types.JID
	left := max(unread, 1)
	for i := len(msgs) - 1; i >= 0 && left > 0; i-- {
		m := msgs[i]
		if m.FromMe {
			continue
		}
		left--
		sender := chat
		if s, err := types.ParseJID(m.ContactId); err == nil && !s.IsEmpty() {
			sender = s.ToNonAD()
		}
		if _, ok := bySender[sender]; !ok {
			order = append(order, sender)
		}
		bySender[sender] = append(bySender[sender], m.Id)
	}
	var errs []error
	for _, sender := range order {
		if err := client.MarkRead(ctx, bySender[sender], time.Now(), chat, sender); err != nil {
			errs = append(errs, fmt.Errorf("receipt: %w", err))
		}
	}
	// the phone's own unread badge (also covers messages this device
	// never received)
	lastTS, lastKey := sm.lastMessageKey(client, chat)
	if err := client.SendAppState(ctx, appstate.BuildMarkChatAsRead(chat, true, lastTS, lastKey)); err != nil {
		errs = append(errs, fmt.Errorf("sync: %w", err))
	}
	return errors.Join(errs...)
}

// snapshotPQ returns a deep copy of the priority queue. Caller must hold sm.mu.
func (sm *SessionManager) snapshotPQ() []*Conversation {
	safeList := make([]*Conversation, len(sm.priorityQueue))
	for i, item := range sm.priorityQueue {
		c := *item
		safeList[i] = &c
	}
	return safeList
}

// getChatName returns the best display name for a chat.
func (sm *SessionManager) getChatName(jid types.JID) string {
	client := sm.getClient()
	if client == nil {
		return jid.User
	}

	// For groups, use the group name if available
	if jid.Server == "g.us" {
		groupInfo, err := client.GetGroupInfo(context.Background(), jid)
		if err == nil && groupInfo.Name != "" {
			return groupInfo.Name
		}
		return "Group Chat"
	}

	return sm.contactName(context.Background(), jid)
}

// getMessages retrieves all messages for one chat id.
// screenLimit caps how many messages are loaded when a chat is opened.
const screenLimit = 400

func (sm *SessionManager) getMessages(wid string) []Message {
	msgs, err := sm.db.GetLatestMessages(wid, screenLimit)
	if err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("failed to load messages: %v", err))
		return []Message{}
	}
	sm.resolveSenders(msgs)
	sm.attachReactions(msgs)
	sm.selfChatRead(wid, msgs)
	sm.resolveMentions(msgs)
	return msgs
}

// resolveSenders replaces stored sender names (often just numbers) with the
// current contact names.
func (sm *SessionManager) resolveSenders(msgs []Message) {
	if sm.getClient() == nil {
		return
	}
	names := map[string]string{}
	for i, m := range msgs {
		if m.FromMe || m.ContactId == "" {
			continue
		}
		name, ok := names[m.ContactId]
		if !ok {
			if jid, err := types.ParseJID(m.ContactId); err == nil {
				name = sm.contactName(context.Background(), jid)
				if strings.HasPrefix(name, "+") && !isPlaceholderName(m.ContactShort, jid.User) {
					name = "~ " + strings.TrimPrefix(m.ContactShort, "~ ")
				}
			}
			names[m.ContactId] = name
		}
		if name != "" {
			msgs[i].ContactShort, msgs[i].ContactName = name, name
		}
	}
}

// sendText sends a text message to a WhatsApp JID.
func (sm *SessionManager) sendText(wid string, text string) {
	receiver, err := types.ParseJID(wid)
	if err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("invalid JID: %v", err))
		return
	}
	msg := &waProto.Message{Conversation: proto.String(text)}
	// Shown as "sending" right away; a failure stays in the chat as
	// "not sent" so it can be retried.
	if _, err := sm.sendTracked(context.Background(), receiver, msg, Message{Text: text}, truncatePreview(text)); err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("failed to send message: %v", err))
	}
}

// storeSent saves a message sent from this device, moves its chat to the top
// of the list and shows it. WhatsApp doesn't echo our own sends back.
func (sm *SessionManager) storeSent(msg Message, preview string) {
	if err := sm.db.AddMessage(msg); err != nil {
		sm.uiHandler.PrintError(fmt.Errorf("failed to save sent message: %v", err))
	}

	sm.mu.Lock()
	var toUpsert *Conversation
	if conv := sm.convByJID[msg.ChatId]; conv != nil {
		conv.LastMsgTime = int64(msg.Timestamp)
		conv.Preview = preview
		sm.priorityQueue.Update(conv, conv.LastMsgTime, conv.IsPinned)
		c := *conv
		toUpsert = &c
	}
	isCurrent := sm.currentReceiver == msg.ChatId
	safeList := sm.snapshotPQ()
	sm.mu.Unlock()

	if toUpsert != nil {
		if err := sm.db.UpsertConversation(*toUpsert); err != nil {
			sm.uiHandler.PrintError(fmt.Errorf("upsert conversation: %w", err))
		}
		sm.uiHandler.UpdateChatList(safeList)
	}
	if isCurrent {
		// names for its @mentions, as a loaded chat has (editing needs them)
		one := []Message{msg}
		sm.resolveMentions(one)
		sm.uiHandler.NewMessage(one[0])
	}
}

// Conversations returns a snapshot of all known conversations (unsorted), so
// a UI can show cached chats before the connection is established.
func (sm *SessionManager) Conversations() []*Conversation {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.snapshotPQ()
}
