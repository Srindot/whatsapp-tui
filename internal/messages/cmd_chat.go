package messages

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
)

func cmdSend(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	if checkParam(params, 2) {
		receiver := params[0]
		textParams := params[1:]
		text := strings.Join(textParams, " ")
		sm.sendText(receiver, text)
	} else {
		sm.printCommandUsage(cmdName, "[chat-id] [message text]")
	}
}

func cmdSelect(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	if checkParam(params, 1) {
		sm.setCurrentReceiver(params[0])
	} else {
		sm.printCommandUsage(cmdName, "[chat-id]")
	}
}

func cmdBacklog(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	sm.mu.RLock()
	receiver := sm.currentReceiver
	sm.mu.RUnlock()

	if receiver == "" {
		sm.printCommandUsage(cmdName, "-> only works in a chat")
		return
	}

	// Anchor on the oldest known message to fetch what came before it; with
	// no messages yet, ask for the latest ones instead.
	var anchor *Message
	if oldest, err := sm.db.GetOldestMessage(receiver); err == nil {
		anchor = &oldest
	}
	if err := sm.requestHistory(receiver, anchor, 50); err != nil {
		sm.uiHandler.PrintError(err)
		return
	}
	if anchor != nil {
		sm.uiHandler.PrintText(fmt.Sprintf("Requested 50 messages before %s from your phone…",
			time.Unix(int64(anchor.Timestamp), 0).Format("2 Jan 2006 15:04")))
	} else {
		sm.uiHandler.PrintText("Requested the latest messages from your phone…")
	}
}

func cmdSyncGroups(sm *SessionManager, client *whatsmeow.Client, cmdName string, params []string) {
	sm.uiHandler.PrintText("Fetching joined groups from WhatsApp servers...")
	go func() {
		// Re-fetch client inside the goroutine — the captured parameter
		// may be stale if the connection dropped between dispatch and
		// goroutine start (audit R-3).
		client := sm.getClient()
		if client == nil {
			sm.uiHandler.PrintError(errors.New("not connected"))
			return
		}
		groups, err := client.GetJoinedGroups(context.Background())
		if err != nil {
			sm.uiHandler.PrintError(fmt.Errorf("failed to fetch groups: %v", err))
			return
		}

		// Batch all PQ operations under a single lock
		var toUpsert []Conversation
		sm.mu.Lock()
		for _, group := range groups {
			jid := group.JID.String()
			name := group.Name
			if name == "" {
				name = "Unknown Group"
			}

			if existing := sm.convByJID[jid]; existing != nil {
				if existing.Name != name {
					existing.Name = name
					toUpsert = append(toUpsert, *existing)
				}
			} else {
				newConv := &Conversation{
					JID:         jid,
					Name:        name,
					LastMsgTime: 0,
					Preview:     "Group synced",
					Unread:      0,
					IsPinned:    false,
				}
				heap.Push(&sm.priorityQueue, newConv)
				sm.convByJID[jid] = newConv
				toUpsert = append(toUpsert, *newConv)
			}
		}
		safeList := sm.snapshotPQ()
		sm.mu.Unlock()

		// DB writes outside lock
		for _, c := range toUpsert {
			if err := sm.db.UpsertConversation(c); err != nil {
				sm.uiHandler.PrintError(fmt.Errorf("upsert conversation: %v", err))
			}
		}

		sm.uiHandler.UpdateChatList(safeList)
		sm.uiHandler.PrintText(fmt.Sprintf("Synced %d groups.", len(groups)))
	}()
}
