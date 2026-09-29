package messages

import (
	"context"
	"fmt"
	"strings"
	"time"

	waProto "go.mau.fi/whatsmeow/binary/proto"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"
)

// setCurrentReceiver sets the currently selected chat and refreshes the
// message view. Automatically marks the chat as read in the background.
func (sm *SessionManager) setCurrentReceiver(id string) {
	sm.mu.Lock()
	sm.currentReceiver = id
	// Check if conversation has unread messages
	hasUnread := false
	if conv := sm.convByJID[id]; conv != nil && conv.Unread > 0 {
		hasUnread = true
	}
	sm.mu.Unlock()
	screen := sm.getMessages(id)
	sm.uiHandler.NewScreen(screen)
	go sm.autoBackfill(id)

	// Auto-mark as read in background (like WhatsApp Web)
	if hasUnread {
		go sm.markChatAsRead(id)
	}
}

// markChatAsRead sends read receipts for the most recent incoming messages
// in the given chat and resets the unread counter in PQ and DB.
// Safe for background use — returns silently on any failure.
func (sm *SessionManager) markChatAsRead(jidStr string) {
	client := sm.getClient()
	if client == nil || !client.IsConnected() {
		return
	}

	chatJID, err := types.ParseJID(jidStr)
	if err != nil {
		return
	}

	// Load messages to collect IDs for read receipt
	msgs, err := sm.db.GetMessages(jidStr)
	if err != nil || len(msgs) == 0 {
		return
	}

	// Collect unread message IDs (up to last 50 messages from others)
	var ids []types.MessageID
	var lastSender types.JID
	for i := len(msgs) - 1; i >= 0 && len(ids) < 50; i-- {
		if !msgs[i].FromMe {
			sender, _ := types.ParseJID(msgs[i].ContactId)
			if len(ids) == 0 {
				lastSender = sender
			}
			// MarkRead requires all IDs to be from the same sender
			if sender == lastSender {
				ids = append(ids, msgs[i].Id)
			}
		}
	}

	if len(ids) == 0 {
		return
	}

	if err := client.MarkRead(context.Background(), ids, time.Now(), chatJID, lastSender); err != nil {
		return
	}

	// Reset unread counter — copy under lock, DB write outside.
	var convCopy *Conversation
	sm.mu.Lock()
	if conv := sm.convByJID[jidStr]; conv != nil {
		conv.Unread = 0
		c := *conv
		convCopy = &c
	}
	safeList := sm.snapshotPQ()
	sm.mu.Unlock()

	if convCopy != nil {
		_ = sm.db.UpsertConversation(*convCopy)
	}

	sm.uiHandler.UpdateChatList(safeList)
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
		sm.uiHandler.NewMessage(msg)
	}
}

// Conversations returns a snapshot of all known conversations (unsorted), so
// a UI can show cached chats before the connection is established.
func (sm *SessionManager) Conversations() []*Conversation {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.snapshotPQ()
}
