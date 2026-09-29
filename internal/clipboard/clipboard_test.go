package clipboard

import (
	"bytes"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// These tests need a throwaway X server, e.g.:
//
//	Xephyr :99 -noreset -screen 320x240 & CLIPBOARD_TEST_DISPLAY=:99 go test ./internal/clipboard
//
// They take ownership of that server's clipboard, so never point them at
// your real display.
func testDisplay(t *testing.T) {
	d := os.Getenv("CLIPBOARD_TEST_DISPLAY")
	if d == "" {
		t.Skip("set CLIPBOARD_TEST_DISPLAY to a throwaway X server to run")
	}
	t.Setenv("DISPLAY", d)
	t.Setenv("WAYLAND_DISPLAY", "")
}

// fakeOwner serves CLIPBOARD like a screenshot tool would; chunk > 0 forces INCR.
type fakeOwner struct {
	conn   *xgb.Conn
	win    xproto.Window
	offers map[string][]byte
	chunk  int
	atoms  map[string]xproto.Atom
	names  map[xproto.Atom]string
}

func newFakeOwner(t *testing.T, offers map[string][]byte, chunk int) *fakeOwner {
	t.Helper()
	conn, err := xgb.NewConn()
	if err != nil {
		t.Fatal(err)
	}
	o := &fakeOwner{conn: conn, offers: offers, chunk: chunk, atoms: map[string]xproto.Atom{}, names: map[xproto.Atom]string{}}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	o.win, _ = xproto.NewWindowId(conn)
	xproto.CreateWindow(conn, 0, o.win, screen.Root, 0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual, 0, nil)
	for _, n := range []string{"CLIPBOARD", "TARGETS", "INCR", "ATOM"} {
		o.atom(n)
	}
	for n := range offers {
		o.atom(n)
	}
	xproto.SetSelectionOwner(conn, o.win, o.atoms["CLIPBOARD"], xproto.TimeCurrentTime)
	if r, err := xproto.GetSelectionOwner(conn, o.atoms["CLIPBOARD"]).Reply(); err != nil || r.Owner != o.win {
		t.Fatal("could not own the clipboard")
	}
	go o.serve()
	t.Cleanup(func() { conn.Close() })
	return o
}

func (o *fakeOwner) atom(n string) xproto.Atom {
	r, _ := xproto.InternAtom(o.conn, false, uint16(len(n)), n).Reply()
	o.atoms[n], o.names[r.Atom] = r.Atom, n
	return r.Atom
}

func (o *fakeOwner) serve() {
	for {
		ev, err := o.conn.WaitForEvent()
		if ev == nil && err == nil {
			return
		}
		req, ok := ev.(xproto.SelectionRequestEvent)
		if !ok {
			continue
		}
		prop := req.Property
		switch name := o.names[req.Target]; {
		case name == "TARGETS":
			var b []byte
			for n := range o.offers {
				v := make([]byte, 4)
				xgb.Put32(v, uint32(o.atoms[n]))
				b = append(b, v...)
			}
			xproto.ChangeProperty(o.conn, xproto.PropModeReplace, req.Requestor, prop, o.atoms["ATOM"], 32, uint32(len(b)/4), b)
			o.notify(req, prop)
		case o.offers[name] != nil && o.chunk > 0:
			o.sendIncr(req, o.offers[name])
		case o.offers[name] != nil:
			d := o.offers[name]
			xproto.ChangeProperty(o.conn, xproto.PropModeReplace, req.Requestor, prop, req.Target, 8, uint32(len(d)), d)
			o.notify(req, prop)
		default:
			o.notify(req, xproto.AtomNone)
		}
	}
}

func (o *fakeOwner) notify(req xproto.SelectionRequestEvent, prop xproto.Atom) {
	ev := xproto.SelectionNotifyEvent{Time: req.Time, Requestor: req.Requestor, Selection: req.Selection, Target: req.Target, Property: prop}
	xproto.SendEvent(o.conn, false, req.Requestor, 0, string(ev.Bytes()))
}

// sendIncr transfers data in chunks on a separate connection, so this fakeOwner
// can watch the requestor's property deletions.
func (o *fakeOwner) sendIncr(req xproto.SelectionRequestEvent, data []byte) {
	c, err := xgb.NewConn()
	if err != nil {
		return
	}
	defer c.Close()
	xproto.ChangeWindowAttributes(c, req.Requestor, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange})
	size := make([]byte, 4)
	xgb.Put32(size, uint32(len(data)))
	xproto.ChangeProperty(c, xproto.PropModeReplace, req.Requestor, req.Property, o.atoms["INCR"], 32, 1, size)
	o.notify(req, req.Property)
	for sent := 0; ; {
		// wait until the requestor deletes the property
		for {
			ev, _ := c.WaitForEvent()
			if pn, ok := ev.(xproto.PropertyNotifyEvent); ok && pn.Atom == req.Property && pn.State == xproto.PropertyDelete {
				break
			}
			if ev == nil {
				return
			}
		}
		end := min(sent+o.chunk, len(data))
		xproto.ChangeProperty(c, xproto.PropModeReplace, req.Requestor, req.Property, req.Target, 8, uint32(end-sent), data[sent:end])
		if end == sent {
			return
		}
		sent = end
	}
}

func TestX11Image(t *testing.T) {
	testDisplay(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{7}, 5000)...)
	newFakeOwner(t, map[string][]byte{"image/png": png, "UTF8_STRING": []byte("caption")}, 0)
	data, mime, err := Image()
	if err != nil || mime != "image/png" || !bytes.Equal(data, png) {
		t.Fatalf("got %d bytes %q err %v", len(data), mime, err)
	}
	text, err := Text()
	if err != nil || text != "caption" {
		t.Fatalf("text %q err %v", text, err)
	}
}

func TestX11ImageIncr(t *testing.T) {
	testDisplay(t)
	big := bytes.Repeat([]byte("screenshot"), 300_000)             // 3 MB, like a real screenshot
	newFakeOwner(t, map[string][]byte{"image/jpeg": big}, 64*1024) // X11 requests max out at 256 KB
	start := time.Now()
	data, mime, err := Image()
	if err != nil || mime != "image/jpeg" || !bytes.Equal(data, big) {
		t.Fatalf("got %d bytes %q err %v", len(data), mime, err)
	}
	t.Logf("3 MB INCR transfer took %v", time.Since(start))
}

func TestX11NoImage(t *testing.T) {
	testDisplay(t)
	newFakeOwner(t, map[string][]byte{"UTF8_STRING": []byte("just text")}, 0)
	if _, _, err := Image(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("err = %v, want ErrEmpty", err)
	}
}

func TestX11WriteThenRead(t *testing.T) {
	testDisplay(t)
	if err := WriteText("héllo from the tui"); err != nil {
		t.Fatal(err)
	}
	if got, err := Text(); err != nil || got != "héllo from the tui" {
		t.Fatalf("text %q err %v", got, err)
	}
	if _, _, err := Image(); !errors.Is(err, ErrEmpty) {
		t.Fatalf("image after text copy: %v", err)
	}

	big := bytes.Repeat([]byte("PNGDATA!"), 400_000) // 3.2 MB: needs INCR
	if err := WriteImage(big, "image/png", "caption"); err != nil {
		t.Fatal(err)
	}
	data, mime, err := Image()
	if err != nil || mime != "image/png" || !bytes.Equal(data, big) {
		t.Fatalf("image: %d bytes %q err %v", len(data), mime, err)
	}
	if got, _ := Text(); got != "caption" {
		t.Fatalf("text alongside image = %q", got)
	}

	// Another app copying takes over; we stop serving.
	newFakeOwner(t, map[string][]byte{"UTF8_STRING": []byte("theirs")}, 0)
	if got, err := Text(); err != nil || got != "theirs" {
		t.Fatalf("after takeover: %q err %v", got, err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		ownerMu.Lock()
		gone := owner == nil
		ownerMu.Unlock()
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fakeOwner still running after losing the selection")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// And we can copy again afterwards.
	if err := WriteText("again"); err != nil {
		t.Fatal(err)
	}
	if got, _ := Text(); got != "again" {
		t.Fatalf("recopy = %q", got)
	}
}
