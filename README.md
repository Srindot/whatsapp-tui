# What's App TUI

A vim-style WhatsApp client for the terminal, with inline images, animated
stickers and profile pictures (in kitty), Rosé Pine colours, and a
nvim-tree-like chat list.

```bash
go build -o whatsapp-tui . && ./whatsapp-tui
```

On first start, scan the QR code with WhatsApp → Settings → Linked devices.

## Keys

| Where | Key | Action |
|---|---|---|
| Lists | `j` `k` · `gg` `G` · `ctrl+d` `ctrl+u` | move |
| | `enter` / `l` | open chat |
| | `backspace` / `q` | back |
| | `/` | filter chats by name |
| | `S` / `:search <text>` | search messages in all chats; `enter` opens the hit in its chat, selected |
| | `u` | only unread chats |
| | `A` | archived chats |
| | `K` | chat info: picture, description / about, members |
| | `V` | the chat's profile picture, full screen (`o` opens it in your viewer) |
| Chat | `h` / `l` | focus chat list / messages |
| | `i` `enter` | write a message |
| | `v` | visual mode: select messages |
| | `/` · `n` · `N` | search this chat (whole history) · older · newer match |
| | `ctrl+n` `ctrl+p` (while typing the search) | older · newer match; `enter` selects it |
| | `p` / `ctrl+v` | paste a screenshot (or text) |
| | `s` | stickers & GIFs you've received or sent: `hjkl` move, `tab` switch, `enter` send |
| | `:sticker [path]` · `:gif [path]` | open the tray, or make a sticker / GIF from a file |
| | `a` / `:attach [path…]` | attach files: opens [yazi](https://yazi-rs.github.io) (`space` picks several, `enter` attaches) |
| Insert | `enter` | send |
| | `alt+enter` / `ctrl+j` (`shift+enter` in kitty, see below) | new line |
| | `ctrl+v` | paste image or text |
| | `ctrl+a` | attach files with yazi |
| | `@` (in groups) | mention someone: type to filter, `tab`/`enter` pick, `ctrl+n`/`ctrl+p` move |
| | `ctrl+x` | drop the last attachment / cancel the reply (works in any mode) |
| | `esc` | back to normal mode |
| Visual | `j` `k` `gg` `G` | select another message |
| | `/` · `n` · `N` | search · jump to older / newer match |
| | `r` / `enter` | reply |
| | `p` | reply privately (group messages) |
| | `e` | react: `1`–`6` quick emoji, `x` remove, or type any emoji |
| | `f` | forward: type to filter chats, `space` picks several, `enter` sends |
| | `y` | copy (text, image, or both) |
| | `d` | download to the download folder |
| | `o` | open media in the default app |
| | `R` | retry a message that failed to send |
| | `esc` | leave visual mode |
| Anywhere | `:q` · `?` | quit · help |
| | `:download-dir [path]` | show / change the download folder |
| | `:backlog` | fetch older messages from your phone |

## Attachments

`a` (or `ctrl+a` while typing) opens yazi as a file picker. Select several
files with `space`, press `enter`, and they wait above the input box with
pasted screenshots. Images go as photos, everything else as documents (up to
100 MB each); the caption you type goes with the first one. `enter` sends
them all, `ctrl+x` removes the last one.

## Stickers and GIFs

`s` in a chat opens a tray of the stickers and GIFs you've received or sent
(from every chat, newest first, each once). Sending one reuses the file on
WhatsApp's servers, like the phone's sticker tray, so it's instant.

In the tray, `n` makes a new one from a file (picked in yazi): images become
512×512 stickers with a transparent background, `.gif`/`.mp4`/`.webm` become
WhatsApp GIFs (silent MP4, up to 15 s). On the sticker tab, `p` turns the
clipboard image into a sticker. Making them needs `ffmpeg`.

GIFs play in the chat in kitty (first 8 seconds, looped); other terminals
show the preview. There's no online GIF search: Google shut down the Tenor
API in June 2026, and the alternatives need a developer API key.

## Text formatting

Messages show WhatsApp's formatting: `*bold*`, `_italic_`, `~strike~`,
`` `code` ``, ```` ```monospace``` ````, `> quotes`, `- `/`* `/`1. ` lists and
```` ``` ```` blocks on their own lines. Links are underlined and @mentions
highlighted. As in WhatsApp, markers only count at word edges, so `2*3*4` and
`snake_case_name` stay as typed.

Mentions show by name (`@Arjun`, and `@You` highlighted when it's you). In a
group, typing `@` opens a member picker; the picked name becomes a real
WhatsApp mention, so that person is notified. (It's not full Markdown on purpose: `#1` or
`1.` at the start of a chat message would otherwise turn into headings and
lists.)

## Message status

Instead of ticks, your messages carry two blocks that fill up in Rosé Pine
colours:

| | |
|---|---|
| `□□` muted | sending |
| `■□` subtle | sent (reached WhatsApp) |
| `■■` gold | delivered |
| `■■` rose | read (iris: voice note or video played) |
| `✕` love, red border | not sent — select it (`v`) and press `R` to retry |

In groups, read means at least one member has read it. In your own chat
("message yourself") everything you send shows as read right away.

If you turned read receipts off in WhatsApp's privacy settings, WhatsApp
doesn't send you other people's read receipts either, so one-to-one chats
stop at delivered (`■■` gold); the chat header says so. Groups and played
voice notes still show.

## kitty setup

kitty sends the same key for `enter` and `shift+enter`, and by default keeps
`ctrl+v` for its own paste. whatsapp-tui marks its window with the kitty user
variable `whatsapp_tui`, so these lines in `kitty.conf` change only its keys:

```conf
# whatsapp-tui: shift+enter = new line, ctrl+v reaches the app (image paste)
map --when-focus-on var:whatsapp_tui shift+enter send_text all \x1b\r
map --when-focus-on var:whatsapp_tui ctrl+v
```

Reload kitty's config with `ctrl+shift+f5`.

## Settings

`~/.config/whatsapp-tui/config.ini`:

```ini
[general]
download_path = ~/Downloads

[ui]
chat_sidebar_width = 38
theme            = rose-pine   ; rose-pine, rose-pine-moon, rose-pine-dawn
paint_background = false       ; true paints the theme background
images           = auto        ; auto, kitty, blocks, off
avatars          = true
mouse            = true        ; click a chat to open it, wheel scrolls (shift+drag selects text)
qr_compact       = false
```

Run with `--debug` to log WhatsApp protocol details to
`~/.cache/whatsapp-tui/debug.log`.

## Credits

Based on [whatscli](https://github.com/normen/whatscli) (MIT) and
[whatsmeow](https://github.com/tulir/whatsmeow). Colours from
[Rosé Pine](https://rosepinetheme.com) (MIT).
