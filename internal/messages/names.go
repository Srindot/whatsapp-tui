package messages

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"
)

// formatPhone formats a JID user part as an international number, e.g.
// "919876543210" -> "+91 98765 43210". Unknown layouts just get a "+".
func formatPhone(user string) string {
	for _, r := range user {
		if !unicode.IsDigit(r) {
			return user
		}
	}
	if strings.HasPrefix(user, "91") && len(user) == 12 { // India
		return "+91 " + user[2:7] + " " + user[7:]
	}
	if strings.HasPrefix(user, "1") && len(user) == 11 { // US/Canada
		return "+1 " + user[1:4] + " " + user[4:7] + " " + user[7:]
	}
	return "+" + user
}

// isPlaceholderName reports names that are just a (possibly redacted) number,
// which a real contact name should replace.
func isPlaceholderName(name, user string) bool {
	name = strings.TrimSpace(name)
	if name == "" || name == user || name == "Group Chat" {
		return true
	}
	for _, r := range name {
		if unicode.IsLetter(r) {
			return false
		}
	}
	return true // digits, "+", spaces, "∙"
}

// contactName resolves a person's display name the way the phone does:
// your saved name, else their business name, else "~ their own name", else
// the phone number. LID-addressed contacts are looked up by phone number too.
func (sm *SessionManager) contactName(ctx context.Context, jid types.JID) string {
	client := sm.getClient()
	if client == nil || client.Store == nil || client.Store.Contacts == nil {
		return formatPhone(jid.User)
	}
	candidates := []types.JID{jid.ToNonAD()}
	if client.Store.LIDs != nil {
		if jid.Server == types.HiddenUserServer {
			if pn, err := client.Store.LIDs.GetPNForLID(ctx, jid); err == nil && !pn.IsEmpty() {
				candidates = append(candidates, pn)
			}
		} else if lid, err := client.Store.LIDs.GetLIDForPN(ctx, jid); err == nil && !lid.IsEmpty() {
			candidates = append(candidates, lid)
		}
	}
	push, business := "", ""
	for _, c := range candidates {
		info, err := client.Store.Contacts.GetContact(ctx, c)
		if err != nil || !info.Found {
			continue
		}
		if info.FullName != "" {
			return info.FullName
		}
		if info.FirstName != "" {
			return info.FirstName
		}
		if business == "" {
			business = info.BusinessName
		}
		if push == "" {
			push = info.PushName
		}
	}
	switch {
	case business != "":
		return business
	case push != "" && !isPlaceholderName(push, jid.User):
		return "~ " + push
	}
	for _, c := range candidates { // show the real number, not the LID
		if c.Server == types.DefaultUserServer {
			return formatPhone(c.User)
		}
	}
	return formatPhone(jid.User)
}

// fetchAppState does a full sync of an app state collection. If the local
// copy is corrupted ("mismatching LTHash", which stops all its updates:
// archive, pin, read on the phone) it's dropped and fetched fresh.
func (sm *SessionManager) fetchAppState(ctx context.Context, name appstate.WAPatchName) error {
	client := sm.getClient()
	if client == nil {
		return errors.New("not connected")
	}
	err := client.FetchAppState(ctx, name, true, false)
	if err == nil || !errors.Is(err, appstate.ErrMismatchingLTHash) {
		return err
	}
	sm.debugf("app state %s is corrupted (%v); fetching it fresh", name, err)
	if derr := client.Store.AppState.DeleteAppStateVersion(ctx, string(name)); derr != nil {
		return fmt.Errorf("reset %s: %w", name, derr)
	}
	return client.FetchAppState(ctx, name, true, false)
}

// refreshNames re-resolves the names of all one-to-one chats (and groups
// without a name) and pushes the updated list to the UI.
func (sm *SessionManager) refreshNames(ctx context.Context) {
	sm.mu.RLock()
	convs := sm.snapshotPQ()
	sm.mu.RUnlock()

	var changed []Conversation
	for _, c := range convs {
		jid, err := types.ParseJID(c.JID)
		if err != nil || jid.Server == types.GroupServer {
			continue
		}
		name := sm.contactName(ctx, jid)
		if name == c.Name {
			continue
		}
		sm.mu.Lock()
		if conv := sm.convByJID[c.JID]; conv != nil {
			conv.Name = name
			changed = append(changed, *conv)
		}
		sm.mu.Unlock()
	}
	for _, c := range changed {
		if err := sm.db.UpsertConversation(c); err != nil {
			sm.debugf("save name for %s: %v", c.JID, err)
		}
	}
	sm.debugf("refreshed names: %d changed", len(changed))
	if len(changed) > 0 {
		sm.mu.RLock()
		list := sm.snapshotPQ()
		sm.mu.RUnlock()
		sm.uiHandler.UpdateChatList(list)
	}
}

// scheduleNameRefresh refreshes names shortly after contact changes stop
// arriving (a contact sync delivers thousands of events).
func (sm *SessionManager) scheduleNameRefresh() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.nameTimer != nil {
		sm.nameTimer.Reset(2 * time.Second)
		return
	}
	sm.nameTimer = time.AfterFunc(2*time.Second, func() {
		sm.mu.Lock()
		sm.nameTimer = nil
		sm.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		sm.refreshNames(ctx)
	})
}

// syncContacts re-downloads the contact list (saved names live in the
// critical_unblock_low app state patch) once per session, then refreshes
// names. The first sync after pairing often fails because the phone sends
// the app state keys late, leaving saved names missing.
func (sm *SessionManager) syncContacts() {
	sm.mu.Lock()
	if sm.contactsSynced {
		sm.mu.Unlock()
		return
	}
	sm.contactsSynced = true
	sm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	sm.mergeLIDChats(ctx)
	sm.refreshNames(ctx) // cheap, fixes names from what's already stored
	client := sm.getClient()
	if client == nil {
		return
	}
	// Archived and pinned chats live in regular_low, which may never have
	// synced either; a full sync replays them as Archive/Pin events.
	// regular_low also carries "chat read on the phone"
	if err := sm.fetchAppState(ctx, appstate.WAPatchRegularLow); err != nil {
		sm.debugf("archive/pin/read sync failed: %v", err)
	} else {
		sm.debugf("archive/pin/read sync done")
	}
	if err := sm.fetchAppState(ctx, appstate.WAPatchCriticalUnblockLow); err != nil {
		sm.debugf("contact sync failed: %v", err)
		sm.mu.Lock()
		sm.contactsSynced = false // retry on the next connect
		sm.mu.Unlock()
		return
	}
	sm.debugf("contact sync done")
	sm.refreshNames(ctx)
}
