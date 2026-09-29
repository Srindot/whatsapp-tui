# whatsapp-tui
#   make            build ./whatsapp-tui
#   make install    install to ~/.local/bin (PREFIX=/usr/local for everyone; needs sudo)
#   make uninstall  remove it again
#   make test       run the tests

PREFIX ?= $(HOME)/.local
BINDIR := $(PREFIX)/bin
BIN    := whatsapp-tui

.PHONY: build install uninstall test clean

build:
	go build -o $(BIN) .

install: build
	mkdir -p $(DESTDIR)$(BINDIR)
	install -m 755 $(BIN) $(DESTDIR)$(BINDIR)/$(BIN)
	@echo "installed $(DESTDIR)$(BINDIR)/$(BIN)"
	@case ":$$PATH:" in *":$(BINDIR):"*) ;; \
		*) echo "note: $(BINDIR) is not on your PATH; add: export PATH=\"$(BINDIR):\$$PATH\"";; esac

uninstall:
	rm -f $(DESTDIR)$(BINDIR)/$(BIN)

test:
	go test ./...

clean:
	rm -f $(BIN)
