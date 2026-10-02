package messages

import "testing"

func TestHasMentionAll(t *testing.T) {
	for text, want := range map[string]bool{
		"@all dinner at 8":  true,
		"hey @all":          true,
		"@all, look":        true,
		"@allison hi":       false,
		"mail me@all.com":   false,
		"all of you":        false,
		"line one\n@all go": true,
	} {
		if got := HasMentionAll(text); got != want {
			t.Errorf("HasMentionAll(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestMarkMentionAll(t *testing.T) {
	msgs := []Message{
		{ChatId: "1@g.us", Text: "@all meeting"},               // someone's @all in a group
		{ChatId: "1@g.us", Text: "@all meeting", FromMe: true}, // yours
		{ChatId: "9@s.whatsapp.net", Text: "@all ha"},          // not a group
		{ChatId: "1@g.us", Text: "no mention"},
	}
	markMentionAll(msgs)
	if msgs[0].Mentions[MentionAll] != "You" {
		t.Fatalf("group @all not marked: %+v", msgs[0])
	}
	for _, m := range msgs[1:] {
		if m.Mentions[MentionAll] != "" {
			t.Fatalf("marked wrongly: %+v", m)
		}
	}
}

func TestExpandMentionsWithoutAll(t *testing.T) {
	sm := &SessionManager{}
	got := sm.expandMentions(t.Context(), "1@g.us", "hi @9111", []string{"9111@s.whatsapp.net"})
	if len(got) != 1 || got[0] != "9111@s.whatsapp.net" {
		t.Fatalf("plain mentions changed: %v", got)
	}
	// "all" picked but no connection: dropped rather than sent as a JID
	got = sm.expandMentions(t.Context(), "1@g.us", "@all go", []string{MentionAll})
	for _, j := range got {
		if j == MentionAll {
			t.Fatalf("the bare @all placeholder was kept: %v", got)
		}
	}
}
