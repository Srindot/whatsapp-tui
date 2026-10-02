package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

// editModel is a group chat whose newest message is yours, sent a minute ago
// (so it can still be edited), with "@918888800000" mentioning Priya.
func editModel(t *testing.T, a *fakeActions) Model {
	t.Helper()
	m := visualModel(t, a, fakeClip{})
	msgs := groupMsgs()
	msgs[2].Timestamp = uint64(time.Now().Add(-time.Minute).Unix())
	msgs[2].Status = messages.StatusSent
	msgs[2].Text = "count me in @918888800000"
	msgs[2].Mentions = map[string]string{"918888800000": "Priya"}
	next, _ := m.Update(screenMsg(msgs))
	return next.(Model)
}

func TestEditOwnMessage(t *testing.T) {
	a := &fakeActions{}
	m := editModel(t, a)
	m, _ = keys(t, m, "R", "i", "d", "r", "a", "f", "t", "esc", "esc") // something half-typed
	m, _ = keys(t, m, "v", "e")
	if m.mode != modeInsert || m.editing == nil || m.editing.Id != "m3" {
		t.Fatalf("mode %d editing %v", m.mode, m.editing)
	}
	// the mention shows as a name, like when writing
	if got := m.compose.Value(); got != "count me in @Priya" {
		t.Fatalf("compose = %q", got)
	}
	v := stripANSI(m.View())
	if !strings.Contains(v, "Editing your message") || !strings.Contains(v, "enter save") {
		t.Fatalf("no edit bar:\n%s", v)
	}
	m, _ = keys(t, m, " ", "t", "o", "o")
	m, cmd := press(t, m, tea.KeyEnter)
	m = drain(t, m, cmd)
	if len(a.edits) != 1 || a.edits[0] != "m3|count me in @918888800000 too|918888800000" {
		t.Fatalf("edits = %v", a.edits)
	}
	// what was being typed before comes back
	if m.editing != nil || m.compose.Value() != "draft" {
		t.Fatalf("after save: editing %v, compose %q", m.editing, m.compose.Value())
	}
}

func TestEditCancel(t *testing.T) {
	a := &fakeActions{}
	m := editModel(t, a)
	m, _ = keys(t, m, "v", "e", "x", "y", "z", "esc")
	if m.editing == nil || m.mode != modeText {
		t.Fatalf("one esc: still editing, in the box (editing %v, mode %d)", m.editing, m.mode)
	}
	m, _ = keys(t, m, "esc") // leaving the box drops the edit
	if m.editing != nil || m.compose.Value() != "" || m.mode != modeNormal {
		t.Fatalf("esc: editing %v compose %q mode %d", m.editing, m.compose.Value(), m.mode)
	}
	// ctrl+x cancels too
	m, _ = keys(t, m, "v", "e")
	m, _ = press(t, m, tea.KeyCtrlX)
	if m.editing != nil || m.compose.Value() != "" {
		t.Fatalf("ctrl+x: editing %v compose %q", m.editing, m.compose.Value())
	}
	if len(a.edits) != 0 {
		t.Fatalf("cancelled edits were sent: %v", a.edits)
	}
	// saving unchanged text sends nothing
	m, _ = keys(t, m, "v", "e")
	m, cmd := press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	if len(a.edits) != 0 {
		t.Fatalf("an unchanged edit was sent: %v", a.edits)
	}
}

func TestEditRefusesOthersAndOld(t *testing.T) {
	a := &fakeActions{}
	m := editModel(t, a)
	m, _ = keys(t, m, "v", "k", "e") // Priya's message
	if m.editing != nil || !strings.Contains(m.notice, "your own messages") {
		t.Fatalf("editing %v notice %q", m.editing, m.notice)
	}
	m = visualModel(t, a, fakeClip{}) // your message from 2023
	m, _ = keys(t, m, "v", "e")
	if m.editing != nil || !strings.Contains(m.notice, "15 minutes") {
		t.Fatalf("old message: editing %v notice %q", m.editing, m.notice)
	}
}

func TestVisualKeys(t *testing.T) {
	a := &fakeActions{}
	m := editModel(t, a)
	if m2, _ := keys(t, m, "v", "enter"); m2.replyTo == nil {
		t.Fatal("enter should reply")
	}
	if m2, _ := keys(t, m, "v", "r"); !m2.picker {
		t.Fatal("r should react")
	}
	if m2, _ := keys(t, m, "v", "e"); m2.editing == nil {
		t.Fatal("e should edit")
	}
}

func TestEditedTag(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	msgs := groupMsgs()
	msgs[0].Edited = true
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	if !strings.Contains(stripANSI(m.View()), "edited ") {
		t.Fatal("edited messages should say so")
	}
}
