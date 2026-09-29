package messages

import (
	"container/heap"
	"context"

	"go.mau.fi/whatsmeow/types"
)

// WhatsApp is moving users to hidden "LID" addresses (123…@lid). The same
// person can then show up under their phone number in older messages and
// under their LID in newer ones. We keep one chat per person, addressed by
// phone number whenever the mapping is known.

// pnForLID maps a LID to its phone-number JID, or returns jid unchanged.
func (sm *SessionManager) pnForLID(ctx context.Context, jid types.JID) types.JID {
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	client := sm.getClient()
	if client == nil || client.Store == nil || client.Store.LIDs == nil {
		return jid
	}
	if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid.ToNonAD()); err == nil && !pn.IsEmpty() {
		return pn.ToNonAD()
	}
	return jid
}

// canonicalSource rewrites LID chat and sender addresses to phone numbers.
func (sm *SessionManager) canonicalSource(ctx context.Context, src *types.MessageSource) {
	if !src.IsGroup && src.Chat.Server == types.HiddenUserServer {
		chat := sm.pnForLID(ctx, src.Chat)
		if chat.Server == types.HiddenUserServer {
			// Not in the store yet: the message carries the other address.
			switch {
			case !src.IsFromMe && src.SenderAlt.Server == types.DefaultUserServer:
				chat = src.SenderAlt.ToNonAD()
			case src.IsFromMe && src.RecipientAlt.Server == types.DefaultUserServer:
				chat = src.RecipientAlt.ToNonAD()
			}
		}
		src.Chat = chat
	}
	if src.Sender.Server == types.HiddenUserServer {
		if pn := sm.pnForLID(ctx, src.Sender); pn.Server != types.HiddenUserServer {
			src.Sender = pn
		} else if src.SenderAlt.Server == types.DefaultUserServer {
			src.Sender = src.SenderAlt.ToNonAD()
		}
	}
}

// mergeLIDChats folds chats stored under a LID into the phone-number chat
// of the same person (fixing duplicates created before this was handled).
func (sm *SessionManager) mergeLIDChats(ctx context.Context) {
	sm.mu.RLock()
	var lids []string
	for jid := range sm.convByJID {
		if j, err := types.ParseJID(jid); err == nil && j.Server == types.HiddenUserServer {
			lids = append(lids, jid)
		}
	}
	sm.mu.RUnlock()

	merged := 0
	for _, lidStr := range lids {
		lid, _ := types.ParseJID(lidStr)
		pn := sm.pnForLID(ctx, lid)
		if pn.Server == types.HiddenUserServer {
			continue // mapping unknown; keep it as is
		}
		pnStr := pn.String()
		if err := sm.db.MergeChat(lidStr, pnStr); err != nil {
			sm.debugf("merge %s into %s: %v", lidStr, pnStr, err)
			continue
		}

		sm.mu.Lock()
		from := sm.convByJID[lidStr]
		to := sm.convByJID[pnStr]
		if from != nil {
			heap.Remove(&sm.priorityQueue, from.Index)
			delete(sm.convByJID, lidStr)
			if to == nil {
				from.JID = pnStr
				heap.Push(&sm.priorityQueue, from)
				sm.convByJID[pnStr] = from
				to = from
			} else {
				if from.LastMsgTime > to.LastMsgTime {
					to.LastMsgTime, to.Preview = from.LastMsgTime, from.Preview
				}
				to.Unread += from.Unread
				to.IsPinned = to.IsPinned || from.IsPinned
				sm.priorityQueue.Update(to, to.LastMsgTime, to.IsPinned)
			}
			if sm.currentReceiver == lidStr {
				sm.currentReceiver = pnStr
			}
		}
		var c Conversation
		if to != nil {
			c = *to
		}
		sm.mu.Unlock()
		if to != nil {
			if err := sm.db.UpsertConversation(c); err != nil {
				sm.debugf("save merged chat: %v", err)
			}
		}
		merged++
	}
	if merged > 0 {
		sm.debugf("merged %d LID chats into phone-number chats", merged)
		sm.mu.RLock()
		list := sm.snapshotPQ()
		sm.mu.RUnlock()
		sm.uiHandler.UpdateChatList(list)
	}
}
