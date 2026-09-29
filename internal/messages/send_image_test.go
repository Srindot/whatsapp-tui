package messages

import (
	"container/heap"
	"image"
	"image/color"
	"testing"
)

func TestFitAndFlatten(t *testing.T) {
	tests := []struct {
		w, h, max, wantW, wantH int
	}{
		{400, 300, 256, 256, 192},
		{108, 240, 256, 108, 240}, // already small
		{300, 600, 256, 128, 256},
		{5000, 10, 100, 100, 1}, // never rounds to zero
	}
	for _, tt := range tests {
		b := fit(image.NewRGBA(image.Rect(0, 0, tt.w, tt.h)), tt.max).Bounds()
		if b.Dx() != tt.wantW || b.Dy() != tt.wantH {
			t.Errorf("fit(%dx%d, %d) = %dx%d, want %dx%d", tt.w, tt.h, tt.max, b.Dx(), b.Dy(), tt.wantW, tt.wantH)
		}
	}

	src := image.NewNRGBA(image.Rect(10, 10, 12, 11)) // offset bounds, one transparent pixel
	src.SetNRGBA(11, 10, color.NRGBA{255, 0, 0, 255})
	out := flatten(src)
	if out.Bounds() != image.Rect(0, 0, 2, 1) {
		t.Fatalf("bounds %v", out.Bounds())
	}
	if c := out.RGBAAt(0, 0); c != (color.RGBA{255, 255, 255, 255}) {
		t.Errorf("transparent pixel became %v, want white", c)
	}
	if c := out.RGBAAt(1, 0); c != (color.RGBA{255, 0, 0, 255}) {
		t.Errorf("red pixel became %v", c)
	}
}

func TestStoreSentMovesChatUp(t *testing.T) {
	ui := NewMockUiHandler()
	sm := &SessionManager{uiHandler: ui, db: newTestDB(t), convByJID: map[string]*Conversation{}}
	heap.Init(&sm.priorityQueue)
	for _, c := range []*Conversation{
		{JID: "a@s.whatsapp.net", Name: "A", LastMsgTime: 100},
		{JID: "b@s.whatsapp.net", Name: "B", LastMsgTime: 200},
	} {
		heap.Push(&sm.priorityQueue, c)
		sm.convByJID[c.JID] = c
	}
	sm.currentReceiver = "a@s.whatsapp.net"

	sm.storeSent(Message{Id: "x", ChatId: "a@s.whatsapp.net", FromMe: true, Timestamp: 300, Text: "[IMAGE] hi"}, "[IMAGE] hi")

	if got := sm.convByJID["a@s.whatsapp.net"]; got.LastMsgTime != 300 || got.Preview != "[IMAGE] hi" {
		t.Fatalf("conversation not updated: %+v", got)
	}
	if len(ui.ChatLists) != 1 || len(ui.Messages) != 1 {
		t.Fatalf("ui updates: %d lists, %d messages", len(ui.ChatLists), len(ui.Messages))
	}
	if msgs, _ := sm.db.GetLatestMessages("a@s.whatsapp.net", 5); len(msgs) != 1 {
		t.Fatalf("message not stored")
	}
	convs, _ := sm.db.GetConversations()
	for _, c := range convs {
		if c.JID == "a@s.whatsapp.net" && c.LastMsgTime != 300 {
			t.Fatalf("conversation not persisted: %+v", c)
		}
	}
}
