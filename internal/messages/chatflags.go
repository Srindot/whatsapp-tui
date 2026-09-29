package messages

import (
	"context"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// setChatFlags applies archive/pin changes from the phone (app state).
// A nil value leaves that flag alone.
func (sm *SessionManager) setChatFlags(jid types.JID, archived, pinned *bool) {
	jid = sm.pnForLID(context.Background(), jid.ToNonAD())
	key := jid.String()

	sm.mu.Lock()
	conv := sm.convByJID[key]
	if conv == nil {
		sm.mu.Unlock()
		return // a chat we don't show (no messages yet)
	}
	changed := false
	if archived != nil && conv.IsArchived != *archived {
		conv.IsArchived, changed = *archived, true
	}
	if pinned != nil && conv.IsPinned != *pinned {
		conv.IsPinned, changed = *pinned, true
		sm.priorityQueue.Update(conv, conv.LastMsgTime, conv.IsPinned)
	}
	c := *conv
	sm.mu.Unlock()

	if !changed {
		return
	}
	if err := sm.db.UpsertConversation(c); err != nil {
		sm.debugf("save chat flags for %s: %v", key, err)
	}
	sm.scheduleListPush()
}

// scheduleListPush sends the chat list to the UI shortly after changes stop
// arriving (an app state sync can deliver hundreds at once).
func (sm *SessionManager) scheduleListPush() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.listTimer != nil {
		sm.listTimer.Reset(300 * time.Millisecond)
		return
	}
	sm.listTimer = time.AfterFunc(300*time.Millisecond, func() {
		sm.mu.Lock()
		sm.listTimer = nil
		list := sm.snapshotPQ()
		sm.mu.Unlock()
		sm.uiHandler.UpdateChatList(list)
	})
}
