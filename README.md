# whatsapp-tui

A vim-style WhatsApp client for the terminal: modal keys, inline images,
animated stickers and GIFs, replies, reactions, search and file sending, in
Rosé Pine colours. Built on [whatsmeow](https://github.com/tulir/whatsmeow).

## Install

You need **Go 1.21+** (it fetches the right toolchain itself) and a **C
compiler** (for SQLite).

```bash
git clone https://github.com/Srindot/whatsapp-tui
cd whatsapp-tui
go build -o whatsapp-tui .
./whatsapp-tui          # scan the QR code: WhatsApp → Settings → Linked devices
```

Or `go install github.com/Srindot/whatsapp-tui@latest` (puts it in `~/go/bin`).

### Dependencies

| Package | Needed for | |
|---|---|---|
| Go, gcc/clang | building | required |
| [kitty](https://sw.kovidgoyal.net/kitty/) (or another kitty-graphics terminal) | sharp images, animations, profile pictures | recommended; other terminals get block-character images |
| [yazi](https://yazi-rs.github.io) | attaching files (`a`) | optional |
| ffmpeg | making stickers/GIFs, playing GIFs | optional |
| wl-clipboard | clipboard on Wayland (X11 works without it) | optional |

**Debian / Ubuntu**
```bash
sudo apt install golang gcc ffmpeg wl-clipboard
# yazi: https://yazi-rs.github.io/docs/installation
```

**Arch**
```bash
sudo pacman -S go gcc ffmpeg yazi wl-clipboard
```

**Fedora**
```bash
sudo dnf install golang gcc ffmpeg-free wl-clipboard   # yazi: see its docs
```

**macOS**
```bash
xcode-select --install          # C compiler
brew install go ffmpeg yazi
```
Pasting images from the clipboard isn't supported on macOS yet.

**Windows**: use WSL with the Ubuntu steps above (images fall back to block
characters outside kitty). Native Windows is untested.

## Use

Press `?` in the app for every key. The basics: `j`/`k` move, `enter` opens a
chat, `i` writes, `esc` goes back to normal mode, `v` selects messages
(`r` reply, `e` react, `f` forward, `y` copy, `d` download), `/` searches,
`S` searches all chats, `:q` quits.

**kitty:** so `shift+enter` makes a new line and `ctrl+v` pastes images, add
to `kitty.conf` (only affects whatsapp-tui):

```conf
map --when-focus-on var:whatsapp_tui shift+enter send_text all \x1b\r
map --when-focus-on var:whatsapp_tui ctrl+v
```

Settings live in `~/.config/whatsapp-tui/config.ini` (on macOS:
`~/Library/Application Support/whatsapp-tui/`): theme, images, mouse,
download folder. `--debug` writes logs to `~/.cache/whatsapp-tui/debug.log`.

## Credits

Based on [whatscli](https://github.com/normen/whatscli) (MIT). Colours by
[Rosé Pine](https://rosepinetheme.com) (MIT). Unofficial client: using it may
break WhatsApp's terms of service.
