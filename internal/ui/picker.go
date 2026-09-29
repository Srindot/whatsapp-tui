package ui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

// imageExts are sent as photos; everything else as a document.
var imageExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true, ".bmp": true}

type (
	// pickedFilesMsg lists files chosen in yazi (or given to :attach).
	pickedFilesMsg struct {
		paths []string
		err   error
	}
	// attachmentsReadyMsg carries loaded attachments (with previews).
	attachmentsReadyMsg struct {
		atts []*attachment
		err  error
	}
)

// pickFiles runs yazi as a file chooser: space selects several files,
// enter attaches them. The UI is suspended while yazi runs.
func (m Model) pickFiles() tea.Cmd {
	yazi, err := exec.LookPath("yazi")
	if err != nil {
		return func() tea.Msg {
			return pickedFilesMsg{err: errors.New("yazi not found; install it or use :attach <path>")}
		}
	}
	f, err := os.CreateTemp("", "whatsapp-tui-chooser-*")
	if err != nil {
		return func() tea.Msg { return pickedFilesMsg{err: err} }
	}
	chooser := f.Name()
	f.Close()
	start := m.lastPickDir
	if start == "" {
		start = config.ExpandPath("~")
	}
	cmd := exec.Command(yazi, "--chooser-file", chooser, start)
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.Remove(chooser)
		data, rerr := os.ReadFile(chooser)
		if err != nil {
			return pickedFilesMsg{err: fmt.Errorf("yazi: %w", err)}
		}
		if rerr != nil {
			return pickedFilesMsg{err: rerr}
		}
		var paths []string
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" {
				paths = append(paths, line)
			}
		}
		return pickedFilesMsg{paths: paths}
	})
}

// loadAttachments stats the files and renders image previews.
func (m Model) loadAttachments(paths []string) tea.Cmd {
	im := m.img
	return func() tea.Msg {
		var atts []*attachment
		var errs []error
		for _, p := range paths {
			p = config.ExpandPath(p)
			info, err := os.Stat(p)
			switch {
			case err != nil:
				errs = append(errs, err)
				continue
			case info.IsDir():
				errs = append(errs, fmt.Errorf("%s is a folder", filepath.Base(p)))
				continue
			}
			a := &attachment{path: p, name: filepath.Base(p), size: info.Size(),
				image: imageExts[strings.ToLower(filepath.Ext(p))]}
			if a.image && info.Size() < 50<<20 {
				if data, err := os.ReadFile(p); err == nil {
					if img, err := termimg.Decode(data); err == nil {
						a.width, a.height = img.Bounds().Dx(), img.Bounds().Dy()
						if im.enabled() {
							cols, rows := termimg.FitCells(a.width, a.height, attachPreviewCols, attachPreviewRows, im.cellW, im.cellH)
							a.preview, _ = im.render(img, cols, rows)
						}
					} else {
						a.image = false // not a readable image: send as a document
					}
				}
			}
			atts = append(atts, a)
		}
		return attachmentsReadyMsg{atts: atts, err: errors.Join(errs...)}
	}
}
