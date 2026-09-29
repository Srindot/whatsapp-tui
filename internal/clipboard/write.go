package clipboard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

// WriteText puts text on the clipboard.
func WriteText(text string) error {
	data := []byte(text)
	return write(map[string][]byte{
		"UTF8_STRING": data, "text/plain;charset=utf-8": data, "text/plain": data, "STRING": data, "TEXT": data,
	}, "text/plain")
}

// WriteImage puts an image (with its MIME type, e.g. "image/png") on the
// clipboard, optionally with text alongside for apps that only paste text.
func WriteImage(data []byte, mime, text string) error {
	offers := map[string][]byte{mime: data}
	if text != "" {
		offers["UTF8_STRING"] = []byte(text)
		offers["text/plain;charset=utf-8"] = []byte(text)
	}
	return write(offers, mime)
}

func write(offers map[string][]byte, primary string) error {
	if useWayland() {
		if _, err := exec.LookPath("wl-copy"); err != nil {
			return errors.New("clipboard: wl-copy not found (install wl-clipboard)")
		}
		cmd := exec.Command("wl-copy", "--type", primary)
		cmd.Stdin = bytes.NewReader(offers[primary])
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("clipboard: wl-copy: %w", err)
		}
		return nil
	}
	return x11Own(offers)
}

// ---------- X11 selection owner ----------

// On X11 the clipboard holds no data: the copying program answers every
// paste request. The owner serves until another program copies something
// (or we exit).
type x11Owner struct {
	mu     sync.Mutex
	conn   *xgb.Conn
	win    xproto.Window
	offers map[string][]byte
	atoms  map[string]xproto.Atom
	names  map[xproto.Atom]string
	chunk  int
	sends  map[incrKey]*incrSend
}

type incrKey struct {
	win  xproto.Window
	prop xproto.Atom
}

type incrSend struct {
	target xproto.Atom
	data   []byte
	done   bool // zero-length terminator sent
}

var (
	ownerMu sync.Mutex
	owner   *x11Owner
)

func x11Own(offers map[string][]byte) error {
	if os.Getenv("DISPLAY") == "" {
		return errors.New("clipboard: no X11 or Wayland display")
	}
	ownerMu.Lock()
	defer ownerMu.Unlock()
	if owner != nil {
		owner.mu.Lock()
		owner.offers = offers
		if err := owner.intern(offers); err != nil {
			owner.mu.Unlock()
			return err
		}
		owner.mu.Unlock()
		return owner.claim()
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return fmt.Errorf("clipboard: connect to X11: %w", err)
	}
	o := &x11Owner{conn: conn, offers: offers, atoms: map[string]xproto.Atom{},
		names: map[xproto.Atom]string{}, sends: map[incrKey]*incrSend{}}
	// Properties larger than the maximum request size must use INCR.
	o.chunk = int(xproto.Setup(conn).MaximumRequestLength)*4 - 1024
	if o.chunk > 256*1024 {
		o.chunk = 256 * 1024
	}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	if o.win, err = xproto.NewWindowId(conn); err != nil {
		conn.Close()
		return fmt.Errorf("clipboard: %w", err)
	}
	if err := xproto.CreateWindowChecked(conn, 0, o.win, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOutput, screen.RootVisual, 0, nil).Check(); err != nil {
		conn.Close()
		return fmt.Errorf("clipboard: create window: %w", err)
	}
	for _, n := range []string{"CLIPBOARD", "TARGETS", "INCR", "ATOM", "TIMESTAMP"} {
		if _, err := o.atom(n); err != nil {
			conn.Close()
			return err
		}
	}
	if err := o.intern(offers); err != nil {
		conn.Close()
		return err
	}
	if err := o.claim(); err != nil {
		conn.Close()
		return err
	}
	owner = o
	go o.serve()
	return nil
}

func (o *x11Owner) atom(name string) (xproto.Atom, error) {
	if a, ok := o.atoms[name]; ok {
		return a, nil
	}
	r, err := xproto.InternAtom(o.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("clipboard: intern %s: %w", name, err)
	}
	o.atoms[name], o.names[r.Atom] = r.Atom, name
	return r.Atom, nil
}

func (o *x11Owner) intern(offers map[string][]byte) error {
	for n := range offers {
		if _, err := o.atom(n); err != nil {
			return err
		}
	}
	return nil
}

func (o *x11Owner) claim() error {
	o.mu.Lock()
	clip := o.atoms["CLIPBOARD"]
	o.mu.Unlock()
	xproto.SetSelectionOwner(o.conn, o.win, clip, xproto.TimeCurrentTime)
	r, err := xproto.GetSelectionOwner(o.conn, clip).Reply()
	if err != nil {
		return fmt.Errorf("clipboard: %w", err)
	}
	if r.Owner != o.win {
		return errors.New("clipboard: could not take ownership")
	}
	return nil
}

func (o *x11Owner) serve() {
	defer func() {
		ownerMu.Lock()
		if owner == o {
			owner = nil
		}
		ownerMu.Unlock()
		o.conn.Close()
	}()
	for {
		ev, err := o.conn.WaitForEvent()
		if ev == nil && err == nil {
			return // connection closed
		}
		switch e := ev.(type) {
		case xproto.SelectionClearEvent:
			return // someone else copied; nothing more to serve
		case xproto.SelectionRequestEvent:
			o.mu.Lock()
			o.answer(e)
			o.mu.Unlock()
		case xproto.PropertyNotifyEvent:
			if e.State == xproto.PropertyDelete {
				o.mu.Lock()
				o.continueIncr(e.Window, e.Atom)
				o.mu.Unlock()
			}
		}
	}
}

func (o *x11Owner) notify(req xproto.SelectionRequestEvent, prop xproto.Atom) {
	ev := xproto.SelectionNotifyEvent{Time: req.Time, Requestor: req.Requestor,
		Selection: req.Selection, Target: req.Target, Property: prop}
	xproto.SendEvent(o.conn, false, req.Requestor, 0, string(ev.Bytes()))
}

func (o *x11Owner) answer(req xproto.SelectionRequestEvent) {
	prop := req.Property
	if prop == xproto.AtomNone { // obsolete clients
		prop = req.Target
	}
	switch name := o.names[req.Target]; {
	case name == "TARGETS":
		atoms := []xproto.Atom{o.atoms["TARGETS"]}
		for n := range o.offers {
			atoms = append(atoms, o.atoms[n])
		}
		b := make([]byte, 4*len(atoms))
		for i, a := range atoms {
			xgb.Put32(b[4*i:], uint32(a))
		}
		xproto.ChangeProperty(o.conn, xproto.PropModeReplace, req.Requestor, prop, o.atoms["ATOM"], 32, uint32(len(atoms)), b)
		o.notify(req, prop)
	case o.offers[name] != nil:
		data := o.offers[name]
		if len(data) <= o.chunk {
			xproto.ChangeProperty(o.conn, xproto.PropModeReplace, req.Requestor, prop, req.Target, 8, uint32(len(data)), data)
			o.notify(req, prop)
			return
		}
		// INCR: announce the size, then send a chunk each time the
		// requestor deletes the property.
		xproto.ChangeWindowAttributes(o.conn, req.Requestor, xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange})
		size := make([]byte, 4)
		xgb.Put32(size, uint32(len(data)))
		xproto.ChangeProperty(o.conn, xproto.PropModeReplace, req.Requestor, prop, o.atoms["INCR"], 32, 1, size)
		o.sends[incrKey{req.Requestor, prop}] = &incrSend{target: req.Target, data: data}
		o.notify(req, prop)
	default:
		o.notify(req, xproto.AtomNone) // refuse unknown targets
	}
}

func (o *x11Owner) continueIncr(win xproto.Window, prop xproto.Atom) {
	k := incrKey{win, prop}
	s := o.sends[k]
	if s == nil {
		return
	}
	if s.done {
		delete(o.sends, k)
		xproto.ChangeWindowAttributes(o.conn, win, xproto.CwEventMask, []uint32{0})
		return
	}
	n := min(o.chunk, len(s.data))
	xproto.ChangeProperty(o.conn, xproto.PropModeReplace, win, prop, s.target, 8, uint32(n), s.data[:n])
	s.data = s.data[n:]
	if n == 0 {
		s.done = true
	}
}
