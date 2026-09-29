// Package clipboard reads the system clipboard: images and text, on X11
// (natively, no xclip needed) and Wayland (via wl-paste).
package clipboard

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func init() {
	// xgb logs connection details to stderr, which would draw over the TUI.
	xgb.Logger = log.New(io.Discard, "", 0)
}

// ErrEmpty means the clipboard holds nothing of the requested kind.
var ErrEmpty = errors.New("clipboard: nothing to paste")

// imageTypes are the image formats accepted, in order of preference.
var imageTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif", "image/bmp"}

// maxSize caps how much is read from the clipboard.
const maxSize = 50 << 20

// timeout bounds how long the clipboard owner may take to answer.
const timeout = 3 * time.Second

// Image returns the clipboard image and its MIME type, or ErrEmpty.
func Image() ([]byte, string, error) {
	if useWayland() {
		return waylandImage()
	}
	return x11Read(imageTypes)
}

// Text returns the clipboard text, or ErrEmpty.
func Text() (string, error) {
	if useWayland() {
		out, err := exec.Command("wl-paste", "--no-newline", "--type", "text/plain").Output()
		if err != nil || len(out) == 0 {
			return "", ErrEmpty
		}
		return string(out), nil
	}
	data, _, err := x11Read([]string{"UTF8_STRING", "text/plain;charset=utf-8", "STRING", "TEXT"})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func useWayland() bool {
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	_, err := exec.LookPath("wl-paste")
	return err == nil
}

func waylandImage() ([]byte, string, error) {
	out, err := exec.Command("wl-paste", "--list-types").Output()
	if err != nil {
		return nil, "", ErrEmpty
	}
	offered := strings.Fields(string(out))
	for _, want := range imageTypes {
		for _, t := range offered {
			if t == want {
				data, err := exec.Command("wl-paste", "--type", want).Output()
				if err != nil {
					return nil, "", fmt.Errorf("clipboard: wl-paste: %w", err)
				}
				return data, want, nil
			}
		}
	}
	return nil, "", ErrEmpty
}

// ---------- X11 ----------

type x11 struct {
	conn   *xgb.Conn
	win    xproto.Window
	prop   xproto.Atom
	events chan xgb.Event
}

// x11Read asks the CLIPBOARD owner for the first of targets it offers.
func x11Read(targets []string) ([]byte, string, error) {
	if os.Getenv("DISPLAY") == "" {
		return nil, "", errors.New("clipboard: no X11 or Wayland display")
	}
	conn, err := xgb.NewConn()
	if err != nil {
		return nil, "", fmt.Errorf("clipboard: connect to X11: %w", err)
	}
	defer conn.Close()

	x := &x11{conn: conn, events: make(chan xgb.Event, 64)}
	screen := xproto.Setup(conn).DefaultScreen(conn)
	if x.win, err = xproto.NewWindowId(conn); err != nil {
		return nil, "", fmt.Errorf("clipboard: %w", err)
	}
	// An unmapped window to receive the data; PropertyChange for INCR.
	if err := xproto.CreateWindowChecked(conn, 0, x.win, screen.Root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		return nil, "", fmt.Errorf("clipboard: create window: %w", err)
	}
	go func() {
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				close(x.events)
				return
			}
			if ev != nil {
				x.events <- ev
			}
		}
	}()

	clip, err := x.atom("CLIPBOARD")
	if err != nil {
		return nil, "", err
	}
	if x.prop, err = x.atom("WHATSAPP_TUI_CLIP"); err != nil {
		return nil, "", err
	}

	offered, err := x.targets(clip)
	if err != nil {
		return nil, "", err
	}
	for _, want := range targets {
		for _, t := range offered {
			if t == want {
				target, err := x.atom(want)
				if err != nil {
					return nil, "", err
				}
				data, err := x.convert(clip, target)
				if err != nil {
					return nil, "", err
				}
				if len(data) == 0 {
					return nil, "", ErrEmpty
				}
				return data, want, nil
			}
		}
	}
	return nil, "", ErrEmpty
}

func (x *x11) atom(name string) (xproto.Atom, error) {
	r, err := xproto.InternAtom(x.conn, false, uint16(len(name)), name).Reply()
	if err != nil {
		return 0, fmt.Errorf("clipboard: intern %s: %w", name, err)
	}
	return r.Atom, nil
}

// targets lists the formats the clipboard owner offers.
func (x *x11) targets(clip xproto.Atom) ([]string, error) {
	targetsAtom, err := x.atom("TARGETS")
	if err != nil {
		return nil, err
	}
	data, err := x.convert(clip, targetsAtom)
	if errors.Is(err, ErrEmpty) {
		return nil, ErrEmpty
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for i := 0; i+4 <= len(data); i += 4 {
		a := xproto.Atom(xgb.Get32(data[i:]))
		r, err := xproto.GetAtomName(x.conn, a).Reply()
		if err == nil {
			names = append(names, r.Name)
		}
	}
	return names, nil
}

// convert requests the selection as target and returns the bytes, following
// the INCR protocol for large transfers.
func (x *x11) convert(clip, target xproto.Atom) ([]byte, error) {
	xproto.ConvertSelection(x.conn, x.win, clip, target, x.prop, xproto.TimeCurrentTime)
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-x.events:
			if !ok {
				return nil, errors.New("clipboard: X11 connection closed")
			}
			sn, isNotify := ev.(xproto.SelectionNotifyEvent)
			if !isNotify || sn.Requestor != x.win {
				continue
			}
			if sn.Property == xproto.AtomNone {
				return nil, ErrEmpty // no owner, or target refused
			}
			return x.readProperty(deadline)
		case <-deadline:
			return nil, errors.New("clipboard: timed out waiting for the clipboard owner")
		}
	}
}

func (x *x11) readProperty(deadline <-chan time.Time) ([]byte, error) {
	incr, err := x.atom("INCR")
	if err != nil {
		return nil, err
	}
	data, typ, err := x.getProperty()
	if err != nil {
		return nil, err
	}
	if typ != incr {
		return data, nil
	}
	// INCR: each time we delete the property the owner writes the next
	// chunk; a zero-length chunk ends the transfer. getProperty deletes.
	var out []byte
	for {
		select {
		case ev, ok := <-x.events:
			if !ok {
				return nil, errors.New("clipboard: X11 connection closed")
			}
			pn, isProp := ev.(xproto.PropertyNotifyEvent)
			if !isProp || pn.Window != x.win || pn.Atom != x.prop || pn.State != xproto.PropertyNewValue {
				continue
			}
			chunk, _, err := x.getProperty()
			if err != nil {
				return nil, err
			}
			if len(chunk) == 0 {
				return out, nil
			}
			if len(out)+len(chunk) > maxSize {
				return nil, errors.New("clipboard: content too large")
			}
			out = append(out, chunk...)
		case <-deadline:
			return nil, errors.New("clipboard: timed out during a large transfer")
		}
	}
}

// getProperty reads and deletes the transfer property.
func (x *x11) getProperty() ([]byte, xproto.Atom, error) {
	var out []byte
	var typ xproto.Atom
	var offset uint32
	for {
		// Lengths and offsets are in 32-bit units. Delete only on the last read.
		r, err := xproto.GetProperty(x.conn, false, x.win, x.prop, xproto.GetPropertyTypeAny, offset, 1<<20).Reply()
		if err != nil {
			return nil, 0, fmt.Errorf("clipboard: read property: %w", err)
		}
		typ = r.Type
		out = append(out, r.Value...)
		if len(out) > maxSize {
			return nil, 0, errors.New("clipboard: content too large")
		}
		if r.BytesAfter == 0 {
			break
		}
		offset += uint32(len(r.Value) / 4)
	}
	xproto.DeleteProperty(x.conn, x.win, x.prop)
	return out, typ, nil
}
