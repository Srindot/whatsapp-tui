package messages

import (
	"container/heap"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

// sm.mu isn't re-entrant: calling a method that locks it while already
// holding it deadlocks the backend, and the UI with it (opening a new
// contact froze the app this way). This checks the source for that.
func TestNoNestedSessionLock(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	methodRe := regexp.MustCompile(`(?ms)^func (?:\(\w+ \*?\w+\) )?(\w+)\(.*?^}`)
	lockRe := regexp.MustCompile(`sm\.mu\.R?Lock\(\)`)
	unlockRe := regexp.MustCompile(`sm\.mu\.R?Unlock\(\)`)
	callRe := regexp.MustCompile(`sm\.(\w+)\(`)
	bodies := map[string]string{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range methodRe.FindAllStringSubmatch(string(src), -1) {
			bodies[m[1]] = m[0]
		}
	}
	// methods that take sm.mu, directly or through another method
	locks := map[string]bool{}
	for changed := true; changed; {
		changed = false
		for name, b := range bodies {
			if locks[name] {
				continue
			}
			take := lockRe.MatchString(b)
			for _, c := range callRe.FindAllStringSubmatch(b, -1) {
				take = take || locks[c[1]]
			}
			if take {
				locks[name], changed = true, true
			}
		}
	}
	for name, b := range bodies {
		held := false
		for _, line := range strings.Split(b, "\n") {
			switch {
			case strings.Contains(line, "defer sm.mu"):
				held = true
				continue
			case lockRe.MatchString(line):
				held = true
				continue
			case unlockRe.MatchString(line):
				held = false
				continue
			}
			if !held {
				continue
			}
			for _, c := range callRe.FindAllStringSubmatch(line, -1) {
				if locks[c[1]] {
					t.Errorf("%s calls %s while holding sm.mu: %s", name, c[1], strings.TrimSpace(line))
				}
			}
		}
	}
}

// Receiving a message used to deadlock (mentionsMe took sm.mu while it was
// held), freezing the whole app on the first incoming message.
func TestIncomingMessageDoesntDeadlock(t *testing.T) {
	sm, _ := newTestSessionManager(t)
	eh := &eventHandler{sm: sm}
	chat := types.NewJID("919000000001", types.DefaultUserServer)
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: chat},
			ID:            "ABC", Timestamp: time.Now(),
		},
		Message: &waE2E.Message{Conversation: proto.String("hi")},
	}
	done := make(chan struct{})
	go func() {
		eh.processIncomingMessage(evt, "hi", "hi")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("processIncomingMessage deadlocked")
	}
	if c := sm.convByJID[chat.String()]; c == nil || c.Unread != 1 {
		t.Fatalf("conversation = %+v", c)
	}
}

// Looking at a chat clears its unread count even when WhatsApp can't be
// told right now (offline); it used to stay unread for good.
func TestMarkReadClearsLocallyWhenOffline(t *testing.T) {
	sm, ui := newTestSessionManager(t)
	c := &Conversation{JID: "919000000001@s.whatsapp.net", Name: "Mom", LastMsgTime: 5, Unread: 4, Mentioned: true}
	heap.Push(&sm.priorityQueue, c)
	sm.convByJID[c.JID] = c

	sm.markChatAsRead(c.JID)
	if c.Unread != 0 || c.Mentioned {
		t.Fatalf("still unread: %+v", c)
	}
	if ui.ChatListCount() != 1 {
		t.Fatalf("chat list pushed %d times", ui.ChatListCount())
	}
	saved, err := sm.db.GetConversations()
	if err != nil || len(saved) != 1 || saved[0].Unread != 0 {
		t.Fatalf("saved %+v %v", saved, err)
	}
	sm.markChatAsRead(c.JID) // already read: nothing to do
	if ui.ChatListCount() != 1 {
		t.Fatal("marking a read chat again shouldn't push the list")
	}
}
