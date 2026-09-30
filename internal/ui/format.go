package ui

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"
)

// WhatsApp's text formatting: *bold*, _italic_, ~strike~, `code`,
// ```monospace```, "> " quotes, "- "/"* "/"1. " lists and ``` blocks.
// Markers only count at word edges (like WhatsApp), so "2*3*4" and
// "snake_case_name" stay as typed.

type textStyle uint8

const (
	fmtBold textStyle = 1 << iota
	fmtItalic
	fmtStrike
	fmtCode
	fmtLink
	fmtMention
	fmtMentionYou
)

// span is a run of text with one style.
type span struct {
	text  string
	style textStyle
	url   string // links: the full address, kept on every wrapped piece
}

var inlineMarkers = map[byte]textStyle{'*': fmtBold, '_': fmtItalic, '~': fmtStrike}

// isEdge reports runes that may sit next to a marker on its outer side.
func isEdge(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r) && r != '*' && r != '_' && r != '~' && r != '`' || unicode.IsSymbol(r)
}

// parseInline splits one line into styled spans.
func parseInline(s string) []span {
	var out []span
	add := func(text string, st textStyle, url string) {
		if text == "" {
			return
		}
		if n := len(out); n > 0 && out[n-1].style == st && out[n-1].url == url {
			out[n-1].text += text
			return
		}
		out = append(out, span{text, st, url})
	}
	var walk func(s string, st textStyle)
	walk = func(s string, st textStyle) {
		plainStart := 0
		for i := 0; i < len(s); {
			// ```monospace``` and `code`: no formatting inside
			if strings.HasPrefix(s[i:], "```") {
				if j := strings.Index(s[i+3:], "```"); j > 0 {
					walkLinks(s[plainStart:i], st, add)
					add(s[i+3:i+3+j], st|fmtCode, "")
					i += 6 + j
					plainStart = i
					continue
				}
			}
			c := s[i]
			if c == '`' {
				if j := strings.IndexByte(s[i+1:], '`'); j > 0 {
					walkLinks(s[plainStart:i], st, add)
					add(s[i+1:i+1+j], st|fmtCode, "")
					i += 2 + j
					plainStart = i
					continue
				}
			}
			if mk, ok := inlineMarkers[c]; ok && st&mk == 0 && openerAt(s, i) {
				if j := closerFor(s, i); j > 0 {
					walkLinks(s[plainStart:i], st, add)
					walk(s[i+1:j], st|mk)
					i = j + 1
					plainStart = i
					continue
				}
			}
			_, size := utf8.DecodeRuneInString(s[i:])
			i += size
		}
		walkLinks(s[plainStart:], st, add)
	}
	walk(s, 0)
	return out
}

// openerAt: the marker at i starts a word (edge before, text after).
func openerAt(s string, i int) bool {
	if i > 0 {
		r, _ := utf8.DecodeLastRuneInString(s[:i])
		if !isEdge(r) {
			return false
		}
	}
	if i+1 >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i+1:])
	return !unicode.IsSpace(r) && r != rune(s[i])
}

// closerFor finds the matching marker for the opener at i on the same line.
func closerFor(s string, i int) int {
	mk := s[i]
	for j := i + 2; j < len(s); j++ {
		if s[j] != mk {
			continue
		}
		before, _ := utf8.DecodeLastRuneInString(s[:j])
		if unicode.IsSpace(before) {
			continue
		}
		if j+1 < len(s) {
			after, _ := utf8.DecodeRuneInString(s[j+1:])
			if !isEdge(after) {
				continue
			}
		}
		return j
	}
	return -1
}

var (
	linkRe    = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"]+[^\s<>".,;:!?)\]}'"]`)
	mentionRe = regexp.MustCompile(`@\d{5,}`)
)

// walkLinks adds plain text, marking links and @mentions.
func walkLinks(s string, st textStyle, add func(string, textStyle, string)) {
	for s != "" {
		loc := linkRe.FindStringIndex(s)
		mloc := mentionRe.FindStringIndex(s)
		style := fmtLink
		if mloc != nil && (loc == nil || mloc[0] < loc[0]) {
			loc, style = mloc, fmtMention
		}
		if loc == nil {
			add(s, st, "")
			return
		}
		add(s[:loc[0]], st, "")
		match, url := s[loc[0]:loc[1]], ""
		if style == fmtLink {
			url = match
			if !strings.Contains(strings.ToLower(url), "://") {
				url = "https://" + url // www.example.com
			}
		}
		add(match, st|style, url)
		s = s[loc[1]:]
	}
}

// spanStyle turns a span style into a lipgloss style on top of base.
func spanStyle(base lipgloss.Style, st textStyle) lipgloss.Style {
	s := base
	if st&fmtBold != 0 {
		s = s.Bold(true)
	}
	if st&fmtItalic != 0 {
		s = s.Italic(true)
	}
	if st&fmtStrike != 0 {
		s = s.Strikethrough(true).Foreground(pal.Subtle)
	}
	if st&fmtCode != 0 {
		s = s.Foreground(pal.Gold).Background(pal.Overlay)
	}
	if st&fmtLink != 0 {
		s = s.Foreground(pal.Iris).Underline(true)
	}
	if st&fmtMention != 0 {
		s = s.Foreground(pal.Rose).Bold(true)
	}
	if st&fmtMentionYou != 0 {
		s = s.Foreground(pal.Base).Background(pal.Rose).Bold(true)
	}
	return s
}

// withMentions shows "@<number>" mentions as "@Name" (names from the
// message's Mentions); a mention of you gets its own highlight.
func withMentions(spans []span, names map[string]string) []span {
	if len(names) == 0 {
		return spans
	}
	for i, sp := range spans {
		if sp.style&fmtMention == 0 {
			continue
		}
		if name, ok := names[strings.TrimPrefix(sp.text, "@")]; ok {
			spans[i].text = "@" + name
			if name == "You" {
				spans[i].style = sp.style&^fmtMention | fmtMentionYou
			}
		}
	}
	return spans
}

// wrapSpans word-wraps styled spans to width, keeping each piece's style
// on every line it lands on. Words longer than a line are split.
func wrapSpans(spans []span, width int, base lipgloss.Style) []string {
	type word struct {
		pieces []span // a word can change style midway ("*bold*,")
		w      int
		space  bool // whitespace, not a word
	}
	var words []word
	for _, sp := range spans {
		for _, tok := range splitKeepSpaces(sp.text) {
			isSpace := strings.TrimSpace(tok) == ""
			n := len(words)
			if n > 0 && !isSpace && !words[n-1].space {
				words[n-1].pieces = append(words[n-1].pieces, span{tok, sp.style, sp.url})
				words[n-1].w += ansi.StringWidth(tok)
				continue
			}
			words = append(words, word{pieces: []span{{tok, sp.style, sp.url}}, w: ansi.StringWidth(tok), space: isSpace})
		}
	}
	width = max(width, 1)
	var lines []string
	var cur strings.Builder
	curW := 0
	flush := func() {
		lines = append(lines, cur.String())
		cur.Reset()
		curW = 0
	}
	render := func(p span) string {
		out := spanStyle(base, p.style).Render(p.text)
		if p.url != "" {
			// OSC 8 hyperlink: each wrapped piece opens the whole link
			out = "\x1b]8;;" + p.url + "\x1b\\" + out + "\x1b]8;;\x1b\\"
		}
		return out
	}
	for _, w := range words {
		if w.space {
			if curW > 0 && curW+w.w <= width {
				cur.WriteString(base.Render(w.pieces[0].text))
				curW += w.w
			}
			continue
		}
		if curW > 0 && curW+w.w > width {
			// drop the trailing space before breaking
			lines = append(lines, strings.TrimRight(cur.String(), " "))
			cur.Reset()
			curW = 0
		}
		// only words longer than a whole line get split here
		for _, p := range w.pieces {
			text := p.text
			for ansi.StringWidth(text) > width-curW {
				head := ansi.Truncate(text, width-curW, "")
				if head == "" {
					if curW > 0 { // not even one character fits: next line
						flush()
						continue
					}
					// a wide character in a too-narrow line: put it there anyway
					head, _, _, _ = uniseg.FirstGraphemeClusterInString(text, -1)
				}
				cur.WriteString(render(span{head, p.style, p.url}))
				flush()
				text = text[len(head):]
			}
			cur.WriteString(render(span{text, p.style, p.url}))
			curW += ansi.StringWidth(text)
		}
	}
	if cur.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

func splitKeepSpaces(s string) []string {
	var out []string
	start := 0
	inSpace := false
	for i, r := range s {
		sp := r == ' ' || r == '\t'
		if i == 0 {
			inSpace = sp
			continue
		}
		if sp != inSpace {
			out = append(out, s[start:i])
			start, inSpace = i, sp
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

var listRe = regexp.MustCompile(`^(\s*)([-*•]|\d{1,3}[.)])\s+(.*)$`)

// formatText renders a message's text with WhatsApp formatting, wrapped to
// width, showing mentions by name. Lines are ready to go in a bubble.
func formatText(text string, width int, base lipgloss.Style, mentions map[string]string) []string {
	inline := func(s string) []span { return withMentions(parseInline(s), mentions) }
	var out []string
	lines := strings.Split(text, "\n")
	codeStyle := lipgloss.NewStyle().Foreground(pal.Gold).Background(pal.Overlay)
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		// ``` on its own line starts a monospace block until the next one
		if strings.TrimSpace(line) == "```" {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) != "```" {
				j++
			}
			if j < len(lines) {
				for _, l := range lines[i+1 : j] {
					for _, part := range strings.Split(ansi.Hardwrap(l, width, true), "\n") {
						out = append(out, codeStyle.Render(part+strings.Repeat(" ", max(width-ansi.StringWidth(part), 0))))
					}
				}
				i = j
				continue
			}
		}
		switch m := listRe.FindStringSubmatch(line); {
		case strings.HasPrefix(line, "> ") || line == ">":
			bar := lipgloss.NewStyle().Foreground(pal.Muted).Render("▎")
			quote := base.Foreground(pal.Subtle).Italic(true)
			for _, l := range wrapSpans(inline(strings.TrimPrefix(strings.TrimPrefix(line, ">"), " ")), width-1, quote) {
				out = append(out, bar+l)
			}
		case m != nil:
			indent, marker, rest := m[1], m[2], m[3]
			if marker == "-" || marker == "*" || marker == "•" {
				marker = "•"
			}
			head := indent + marker + " "
			hw := ansi.StringWidth(head)
			bullet := lipgloss.NewStyle().Foreground(pal.Rose).Render(head)
			for k, l := range wrapSpans(inline(rest), max(width-hw, 4), base) {
				if k == 0 {
					out = append(out, bullet+l)
				} else {
					out = append(out, strings.Repeat(" ", hw)+l)
				}
			}
		default:
			out = append(out, wrapSpans(inline(line), width, base)...)
		}
	}
	return out
}

// plainText removes WhatsApp formatting markers (for previews).
func plainText(text string) string {
	var b strings.Builder
	for _, sp := range parseInline(text) {
		b.WriteString(sp.text)
	}
	return b.String()
}
