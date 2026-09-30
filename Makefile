# whatsapp-tui
#   make            build ./whatsapp-tui
#   make install    install to ~/.local (PREFIX=/usr/local for everyone; needs sudo):
#                   the binary, plus a launcher entry (rofi, app menus) and icon
#   make uninstall  remove it again
#   make test       run the tests
#
# The launcher opens it in TERMINAL (default kitty): make install TERMINAL=alacritty

PREFIX   ?= $(HOME)/.local
TERMINAL ?= kitty
BINDIR   := $(PREFIX)/bin
APPDIR   := $(PREFIX)/share/applications
ICONDIR  := $(PREFIX)/share/icons/hicolor/scalable/apps
BIN      := whatsapp-tui

.PHONY: build install uninstall test clean

build:
	go build -o $(BIN) .

install: build
	mkdir -p $(DESTDIR)$(BINDIR)
	install -m 755 $(BIN) $(DESTDIR)$(BINDIR)/$(BIN)
	mkdir -p $(DESTDIR)$(APPDIR) $(DESTDIR)$(ICONDIR)
	install -m 644 assets/$(BIN).svg $(DESTDIR)$(ICONDIR)/$(BIN).svg
	@termpath=$$(command -v $(TERMINAL) || echo $(TERMINAL)); \
	case "$(TERMINAL)" in \
		kitty) exec_line="$$termpath --class=$(BIN) --title=WhatsApp $(BINDIR)/$(BIN)";; \
		*)     exec_line="$$termpath -e $(BINDIR)/$(BIN)";; \
	esac; \
	printf '%s\n' '[Desktop Entry]' 'Type=Application' 'Name=WhatsApp TUI' \
		'GenericName=WhatsApp client' 'Comment=Vim-style WhatsApp in the terminal' \
		"Exec=$$exec_line" 'Icon=$(BIN)' 'Terminal=false' 'Categories=Network;InstantMessaging;Chat;' \
		'Keywords=whatsapp;chat;messages;tui;' 'StartupWMClass=$(BIN)' \
		> $(DESTDIR)$(APPDIR)/$(BIN).desktop
	-@update-desktop-database $(DESTDIR)$(APPDIR) 2>/dev/null
	@echo "installed $(DESTDIR)$(BINDIR)/$(BIN) and its launcher entry"
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; \
		*) echo "note: $(BINDIR) is not on your PATH; add: export PATH=\"$(BINDIR):\$$PATH\"";; esac

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BIN) $(DESTDIR)$(APPDIR)/$(BIN).desktop $(DESTDIR)$(ICONDIR)/$(BIN).svg

test:
	go test ./...

clean:
	rm -f $(BIN)
