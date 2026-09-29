package messages

import (
	"context"
	"fmt"
	"testing"
)

func TestSearchHistoryLoadsFromOldestMatch(t *testing.T) {
	sm := &SessionManager{uiHandler: NewMockUiHandler(), db: newTestDB(t)}
	for i := 0; i < 1000; i++ {
		text := fmt.Sprintf("msg %d", i)
		if i == 100 || i == 950 {
			text = "Pizza party"
		}
		addTestMsg(t, sm.db, Message{Id: fmt.Sprint(i), ChatId: "c", Timestamp: uint64(1000 + i), Text: text})
	}
	msgs, total, err := sm.SearchHistory(context.Background(), "c", "pizza")
	if err != nil || total != 2 {
		t.Fatalf("total %d err %v", total, err)
	}
	if len(msgs) != 900 || msgs[0].Id != "100" || msgs[len(msgs)-1].Id != "999" {
		t.Fatalf("loaded %d messages from %s", len(msgs), msgs[0].Id)
	}

	// a match within the normal window still returns a full screen
	msgs, total, _ = sm.SearchHistory(context.Background(), "c", "msg 998")
	if total != 1 || len(msgs) != screenLimit {
		t.Fatalf("recent match: %d msgs, total %d", len(msgs), total)
	}
	if msgs, total, _ := sm.SearchHistory(context.Background(), "c", "nothing"); msgs != nil || total != 0 {
		t.Fatal("no-match search returned results")
	}
}

func TestSearchAllAndLoadAround(t *testing.T) {
	sm := &SessionManager{uiHandler: NewMockUiHandler(), db: newTestDB(t), convByJID: map[string]*Conversation{
		"a@s.whatsapp.net": {JID: "a@s.whatsapp.net", Name: "Aneesh"},
		"g@g.us":           {JID: "g@g.us", Name: "Hostel"},
	}}
	for i := 0; i < 5000; i++ {
		text := fmt.Sprintf("a %d", i)
		if i == 10 {
			text = "wifi password is hunter2"
		}
		addTestMsg(t, sm.db, Message{Id: fmt.Sprint("a", i), ChatId: "a@s.whatsapp.net", Timestamp: uint64(1000 + i), Text: text})
	}
	addTestMsg(t, sm.db, Message{Id: "g1", ChatId: "g@g.us", Timestamp: 9000, Text: "who has the WIFI password"})
	addTestMsg(t, sm.db, Message{Id: "g2", ChatId: "g@g.us", Timestamp: 9001, Text: "[REACTION] wifi"})

	hits, err := sm.SearchAll(context.Background(), "wifi")
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits %d err %v", len(hits), err)
	}
	if hits[0].Id != "g1" || hits[0].ChatName != "Hostel" || hits[1].ChatName != "Aneesh" {
		t.Fatalf("hits = %+v", hits)
	}

	// an old hit in a long chat: a window starting at the hit
	msgs, err := sm.LoadAround(context.Background(), "a@s.whatsapp.net", "a10")
	if err != nil || len(msgs) != searchMaxMessages || msgs[0].Id != "a10" {
		t.Fatalf("window: %d msgs from %s err %v", len(msgs), msgs[0].Id, err)
	}
	// a recent hit: a normal screen that contains it
	msgs, _ = sm.LoadAround(context.Background(), "a@s.whatsapp.net", "a4990")
	if len(msgs) != screenLimit || msgs[len(msgs)-1].Id != "a4999" {
		t.Fatalf("recent: %d msgs ending %s", len(msgs), msgs[len(msgs)-1].Id)
	}
}
