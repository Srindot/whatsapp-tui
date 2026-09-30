package ui

import (
	"net/url"
	"os"
	"strings"

	"github.com/Srindot/whatsapp-tui/internal/config"
)

// Drag and drop: terminals don't pass dropped files to programs, they paste
// their paths (kitty: one per line; others: file:// URIs, or shell-quoted
// paths on one line). A paste that is nothing but existing files is taken
// as a drop and attaches them; anything else stays a text paste.

// maxDropped caps how many dropped files are attached at once.
const maxDropped = 30

// droppedFiles returns the files a paste names, or ok=false when the paste
// isn't (only) files.
func droppedFiles(text string) (paths []string, ok bool) {
	for _, line := range strings.FieldsFunc(text, func(r rune) bool { return r == '\n' || r == '\r' }) {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") { // text/uri-list comments
			continue
		}
		// the whole line is one path (spaces and all), else several
		// quoted or escaped ones
		if p, ok := droppedPath(unquote(line)); ok {
			paths = append(paths, p)
			continue
		}
		words, ok := shellWords(line)
		if !ok || len(words) < 2 {
			return nil, false
		}
		for _, w := range words {
			p, ok := droppedPath(w)
			if !ok {
				return nil, false
			}
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 || len(paths) > maxDropped {
		return nil, false
	}
	return paths, true
}

// droppedPath turns a path or file:// URI into an existing file's path.
func droppedPath(s string) (string, bool) {
	if strings.HasPrefix(s, "file://") {
		u, err := url.Parse(s)
		if err != nil || (u.Host != "" && u.Host != "localhost") {
			return "", false
		}
		s = u.Path
	}
	if !strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "~/") {
		return "", false // relative paths are just text
	}
	s = config.ExpandPath(s)
	info, err := os.Stat(s)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return s, true
}

// unquote strips one pair of surrounding quotes and backslash escapes, as a
// shell would for a single word.
func unquote(s string) string {
	if w, ok := shellWords(s); ok && len(w) == 1 {
		return w[0]
	}
	return s
}

// shellWords splits s like a shell: spaces separate words, '…' and "…"
// quote, and a backslash escapes the next character.
func shellWords(s string) ([]string, bool) {
	var words []string
	var cur strings.Builder
	inWord := false
	var quote rune
	escaped := false
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case r == '\\' && quote != '\'':
			escaped, inWord = true, true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 || escaped {
		return nil, false
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, true
}
