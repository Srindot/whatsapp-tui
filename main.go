// whatsapp-tui is a vim-style terminal WhatsApp client.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/config"
	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
	"github.com/Srindot/whatsapp-tui/internal/ui"
)

// mediaSource adapts the session manager to the UI, honouring the avatars
// setting.
type mediaSource struct {
	*messages.SessionManager
	avatars bool
}

func (m mediaSource) ProfilePicture(ctx context.Context, jid string) (string, error) {
	if !m.avatars {
		return "", nil
	}
	return m.SessionManager.ProfilePicture(ctx, jid)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "whatsapp-tui:", err)
		os.Exit(1)
	}
}

func run() error {
	debug := flag.Bool("debug", false, "write WhatsApp protocol logs to "+debugLogPath())
	flag.Parse()

	if err := config.InitConfig(); err != nil {
		return fmt.Errorf("config: %w", err)
	}

	// The handler forwards backend events into the program; the program
	// is created after the session manager, so wire send in afterwards.
	handler := ui.NewHandler(func(tea.Msg) {})
	sm := &messages.SessionManager{}
	if err := sm.Init(handler); err != nil {
		return fmt.Errorf("session: %w", err)
	}
	defer sm.Shutdown()
	if *debug {
		if err := os.MkdirAll(filepath.Dir(debugLogPath()), 0o700); err != nil {
			return fmt.Errorf("debug log: %w", err)
		}
		f, err := os.OpenFile(debugLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return fmt.Errorf("debug log: %w", err)
		}
		defer f.Close()
		sm.SetDebug(true)
		sm.SetLogWriter(f)
	}

	opts := ui.Options{
		SidebarWidth:    config.Config.Ui.ChatSidebarWidth,
		Theme:           config.Config.Ui.Theme,
		PaintBackground: config.Config.Ui.PaintBackground,
		Images:          termimg.Detect(config.Config.Ui.Images),
		Media:           mediaSource{sm, config.Config.Ui.Avatars},
		Sender:          sm,
		Actions:         sm,
		Searcher:        sm,
		GlobalSearcher:  sm,
		Forwarder:       sm,
		Privacy:         sm,
		Stickers:        sm,
		Mentioner:       sm,
		Pictures:        sm,
		Deleter:         sm,
	}
	if opts.Images == termimg.ModeKitty {
		kitty, err := termimg.NewKitty(os.Stdout)
		if err != nil {
			opts.Images = termimg.ModeBlocks
		} else {
			opts.Kitty = kitty
			defer kitty.Close()
		}
	}
	model := ui.New(sm.CommandChannel, sm.Conversations(), opts)
	progOpts := []tea.ProgramOption{tea.WithAltScreen(), tea.WithReportFocus()} // focus: only mark chats read while you look
	if config.Config.Ui.Mouse {
		progOpts = append(progOpts, tea.WithMouseCellMotion())
	}
	p := tea.NewProgram(model, progOpts...)
	handler.SetSend(p.Send)

	if err := sm.StartManager(); err != nil {
		return fmt.Errorf("start: %w", err)
	}
	// Mark this kitty window so kitty.conf can pass keys it normally handles
	// itself (ctrl+v, shift+enter) to us; see the README.
	if termimg.Detect("auto") == termimg.ModeKitty {
		fmt.Fprint(os.Stdout, "\x1b]1337;SetUserVar=whatsapp_tui=MQ==\x07")
		defer fmt.Fprint(os.Stdout, "\x1b]1337;SetUserVar=whatsapp_tui\x07")
	}
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("ui: %w", err)
	}
	return nil
}

func debugLogPath() string { return filepath.Join(config.GetCacheDir(), "debug.log") }
