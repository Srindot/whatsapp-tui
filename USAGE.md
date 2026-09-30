# Using whatsapp-tui

whatsapp-tui works like vim: you're in **normal mode** to move around, **insert
mode** to type, and **visual mode** to act on messages. Press `?` any time for
the full key list.

## Run it from anywhere

```bash
make install            # copies the binary to ~/.local/bin
whatsapp-tui            # now works from any directory
```

`~/.local/bin` must be on your `PATH` (it is on most Linux setups; `make
install` warns if not). If it isn't, add this to `~/.bashrc` or `~/.zshrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

It also adds **WhatsApp TUI** to rofi and your app menu, opening in its own
kitty window (another terminal: `make install TERMINAL=alacritty`).

To install for every user instead: `sudo make install PREFIX=/usr/local`.
Without make: `install -m 755 whatsapp-tui ~/.local/bin/`. Remove it with
`make uninstall`. Rebuild after pulling changes with `make install` again.

## First start

Run `whatsapp-tui` and scan the QR code with your phone: **WhatsApp →
Settings → Linked devices → Link a device**. The login is remembered; your
chats and recent history load in the background.

## Moving around

The chat list opens first. `enter` (or `l`) opens a chat; the list moves to
the left as a sidebar and `backspace` (or `q`) goes back. A chat with unread
messages opens at the first unread one, below a "N unread messages" line;
otherwise (or when the phone reports more unread than have loaded) at the
newest message. Older history loading in doesn't move what you're reading.

A chat counts as read once it's open while the terminal window has focus
(messages that arrive while you watch too). That clears it on your phone as
well, and reading it on the phone clears it here.

| Key | |
|---|---|
| `j` `k` | down / up |
| `gg` `G` | top / bottom |
| `ctrl+d` `ctrl+u` | half a page |
| `h` `l` | jump between the sidebar and the messages |
| `u` | only unread chats |
| `A` | archived chats, from anywhere (or `enter` on the **Archived** row at the top) |
| `K` | chat info: description / about, members, picture |
| `V` | the profile picture, full screen |
| `d` then `enter` | delete the chat (on all your devices) |
| `:q` | quit |

With the mouse: click a chat to open it, click a link to open it, scroll with
the wheel.

## Writing

`i` starts typing, `enter` sends, `esc` goes back to normal mode.

- **New line:** `shift+enter` in kitty (see [kitty setup](#kitty-setup)), or
  `alt+enter` / `ctrl+j` anywhere.
- **Formatting** shows like on the phone: `*bold*`, `_italic_`, `~strike~`,
  `` `code` ``, `> quote`, `- lists`.
- **Mentions:** in a group, type `@` and a member list pops up; `tab` picks,
  `ctrl+n`/`ctrl+p` move. The person gets notified.
- **Mentions of you** stand out in red ("@ mentioned you"), chats with one show
  a red `@` badge, and `@` (in normal or visual mode) jumps between them.

## Acting on messages (visual mode)

Press `v`: the newest message is selected. Move with `j` `k` (`gg` `G`), then:

| Key | |
|---|---|
| `r` / `enter` | reply |
| `p` | reply privately to a group member |
| `e` | react: `1`–`6` quick emoji, `x` removes, or type any emoji + `enter` |
| `f` | forward: type to filter chats, `space` picks several, `enter` sends |
| `y` | copy the text and/or image |
| `s` | save (download) to your download folder |
| `d` | delete: `enter` for you, `e` for everyone (your messages, up to ~2½ days old) |
| `space` | view the photo, sticker or GIF full screen; play a video |
| `o` | open the photo/file in its app, or the message's link |
| `R` | retry a message that failed to send |
| `esc` | done |

`ctrl+x` cancels a reply (or drops an attachment) in any mode.

## Searching

| Where | Key | Finds |
|---|---|---|
| chat list | `/` | chats and contacts, by name or number |
| in a chat | `/` | messages in this chat (whole history) |
| anywhere | `S` or `:search <text>` | messages in every chat |

While typing a chat search, `ctrl+n`/`ctrl+p` jump between matches. `enter`
selects the match (visual mode, ready for `r` `e` `f` …); then `n` goes to the
older match and `N` to the newer one. `esc` twice clears it. Searches are
smartcase: lowercase matches any case, an uppercase letter makes it exact.

In the chat list, `/` also finds contacts you've never written to and
archived chats (by name or number). `enter` stops typing and keeps the
results: browse them with `j` `k`, open one with `enter`, or use `K` `V` `d`
on them. They stay in the sidebar while you're in a chat, so you can go
through several. `/` edits the search, `esc` (or `backspace`) clears it.

In the all-chats search, `enter` opens the result in its chat with the message
selected.

## Photos, files, stickers, GIFs

- **Paste a screenshot:** `ctrl+v` while typing (or `p` in a chat). It waits
  above the input box; type a caption and `enter`.
- **Attach files:** `a` in a chat (or `ctrl+a` while typing) opens
  [yazi](https://yazi-rs.github.io): `space` picks several files, `enter`
  attaches them. Images are sent as photos, the rest as documents. Without
  yazi: `:attach ~/file.pdf`.
- **Drag and drop:** drag files from your file manager onto the window while
  a chat is open; they're attached the same way. (The terminal pastes their
  paths, so this works in kitty and most other terminals. An image dragged
  straight from a browser is a link, not a file: save it first, or copy it
  and `ctrl+v`.)
- **Stickers & GIFs:** `s` opens a tray of the ones you've received or sent.
  `hjkl` move, `tab` switches between stickers and GIFs, `enter` sends. In
  the tray, `n` makes a new one from a file and `p` turns the clipboard image
  into a sticker (needs ffmpeg).
- **Viewing:** `v`, select a photo, sticker or GIF, `space` shows it full
  screen (GIFs and animated stickers play in kitty); `esc` closes. Videos play
  in mpv (inside the terminal with mpv 0.36+, otherwise in its own window).
- **Downloads** go to `~/Downloads`. `:download-dir` shows the folder,
  `:download-dir ~/Pictures/wa` changes it.

## Message status

Your messages show two blocks instead of ticks:

| | |
|---|---|
| `□□` | sending |
| `■□` | sent |
| `■■` gold | delivered |
| `■■` rose | read (iris: voice note played) |
| `✕` | not sent — `v`, select it, `R` to retry |

If you turned read receipts off in WhatsApp, WhatsApp doesn't send you
anyone's either, so one-to-one chats stop at delivered.

## kitty setup

kitty sends the same key for `enter` and `shift+enter`, and keeps `ctrl+v` for
its own paste. These lines in `~/.config/kitty/kitty.conf` change that only
inside whatsapp-tui:

```conf
map --when-focus-on var:whatsapp_tui shift+enter send_text all \x1b\r
map --when-focus-on var:whatsapp_tui ctrl+v
```

Reload with `ctrl+shift+f5` and restart whatsapp-tui. To select text with the
mouse while it runs, hold `shift` while dragging.

## Settings

`~/.config/whatsapp-tui/config.ini` (macOS: `~/Library/Application
Support/whatsapp-tui/config.ini`):

```ini
[general]
download_path = ~/Downloads

[ui]
theme            = rose-pine   ; rose-pine, rose-pine-moon, rose-pine-dawn
paint_background = false       ; true paints the theme background
images           = auto        ; auto, kitty, blocks, off
avatars          = true
mouse            = true
chat_sidebar_width = 38
qr_compact       = false       ; smaller login QR code
```

## When something's off

- **Names show as numbers** or **chats are missing history:** give it a minute
  after connecting; contact names and history sync from your phone, which
  must be online.
- **Old photos show as "📷 Photo":** they arrived before this device was
  linked with media support; recent ones load when you open the chat.
- **Anything else:** run `whatsapp-tui --debug` and check
  `~/.cache/whatsapp-tui/debug.log`.
- **Start over:** `:logout` unlinks this device; delete
  `~/.config/whatsapp-tui/session.db` to force a fresh QR login.
