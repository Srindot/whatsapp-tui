package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
)

func TestMentionAllPickedAndTyped(t *testing.T) {
	m, fm, _ := mentionModel(t, groupJID)
	fm.members = append([]messages.GroupMember{{JID: messages.MentionAll, Name: messages.MentionAll}}, fm.members...)
	m = typeText(t, m, "@al")
	if v := stripANSI(m.View()); !strings.Contains(v, "@all") || !strings.Contains(v, "everyone in the group") {
		t.Fatalf("no @all in the picker:\n%s", v)
	}
	m, _ = press(t, m, tea.KeyTab)
	m = typeText(t, m, "dinner")
	m, cmd := press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	if len(fm.sent) != 1 || fm.sent[0] != "@all dinner|all" {
		t.Fatalf("sent %v", fm.sent)
	}

	// typed without the picker: still goes out as a mention (not plain text)
	m = typeText(t, m, "ok @all go")
	m, _ = keys(t, m, "esc", "i") // close any picker
	m, cmd = press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	if len(fm.sent) != 2 || !strings.HasPrefix(fm.sent[1], "ok @all go|") {
		t.Fatalf("typed @all: sent %v", fm.sent)
	}
}

func TestMentionAllHighlight(t *testing.T) {
	m := visualModel(t, &fakeActions{}, fakeClip{})
	msgs := groupMsgs()
	msgs[0].Text = "@all dinner at 8?"
	msgs[0].Mentions = map[string]string{messages.MentionAll: "You"}
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	if v := stripANSI(m.View()); !strings.Contains(v, "@all dinner") {
		t.Fatalf("@all should stay as written:\n%s", v)
	}
	if !mentionsYou(msgs[0]) {
		t.Fatal("@all should count as mentioning you")
	}
}

func TestEditShowsMentionNamesFromMembers(t *testing.T) {
	a := &fakeActions{}
	m := visualModel(t, a, fakeClip{})
	// your just-sent message: its mention has no name yet
	msgs := groupMsgs()
	msgs[2].Timestamp = uint64(time.Now().Add(-time.Minute).Unix())
	msgs[2].Status = messages.StatusSent
	msgs[2].Text = "see you @919440438863"
	msgs[2].Mentions = nil
	next, _ := m.Update(screenMsg(msgs))
	m = next.(Model)
	m.members[groupJID] = []messages.GroupMember{{JID: "919440438863@s.whatsapp.net", Name: "Hari Shankar"}}
	m, _ = keys(t, m, "v", "e")
	if got := m.compose.Value(); got != "see you @Hari Shankar" {
		t.Fatalf("compose = %q", got)
	}
	m, _ = keys(t, m, "!")
	m, cmd := press(t, m, tea.KeyEnter)
	drain(t, m, cmd)
	if len(a.edits) != 1 || a.edits[0] != "m3|see you @919440438863!|919440438863" {
		t.Fatalf("edits = %v", a.edits)
	}
}
