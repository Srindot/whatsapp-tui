package ui

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Srindot/whatsapp-tui/internal/messages"
	"github.com/Srindot/whatsapp-tui/internal/termimg"
)

var diacriticIndex = func() map[rune]int {
	m := map[rune]int{}
	for i, r := range termimgDiacritics() {
		m[r] = i
	}
	return m
}()

// placedImages decodes kitty Unicode placeholders in a screen the way kitty
// does (a cell without diacritics continues the cell on its left), and
// returns image id -> set of (row, col) cells shown.
func placedImages(t *testing.T, screen string) map[int]map[[2]int]bool {
	t.Helper()
	sgr := regexp.MustCompile("\x1b\\[([0-9;]*)m")
	out := map[int]map[[2]int]bool{}
	for _, line := range strings.Split(screen, "\n") {
		fg := -1
		prevID, prevRow, prevCol := -1, -1, -1
		rs := []rune(line)
		for i := 0; i < len(rs); i++ {
			if rs[i] == 0x1b {
				loc := sgr.FindStringSubmatchIndex(string(rs[i:]))
				if loc != nil && loc[0] == 0 {
					params := string(rs[i:])[loc[2]:loc[3]]
					if strings.HasPrefix(params, "38;2;") {
						p := strings.Split(params, ";")
						r, _ := strconv.Atoi(p[2])
						g, _ := strconv.Atoi(p[3])
						b, _ := strconv.Atoi(p[4])
						fg = r<<16 | g<<8 | b
					} else if params == "39" || params == "0" || params == "" {
						fg = -1
					}
					i += len([]rune(string(rs[i:])[:loc[1]])) - 1
					continue
				}
			}
			if rs[i] != 0x10EEEE {
				prevID = -1
				continue
			}
			row, col := -1, -1
			for i+1 < len(rs) {
				if d, ok := diacriticIndex[rs[i+1]]; ok {
					if row < 0 {
						row = d
					} else if col < 0 {
						col = d
					}
					i++
					continue
				}
				break
			}
			switch {
			case row < 0 && prevID == fg:
				row, col = prevRow, prevCol+1
			case col < 0 && prevID == fg && row == prevRow:
				col = prevCol + 1
			}
			if row < 0 || col < 0 || fg < 0 {
				t.Fatalf("placeholder cell without a position")
			}
			if out[fg] == nil {
				out[fg] = map[[2]int]bool{}
			}
			out[fg][[2]int{row, col}] = true
			prevID, prevRow, prevCol = fg, row, col
		}
	}
	return out
}

func TestStickerGridShowsWholeStickers(t *testing.T) {
	var out bytes.Buffer
	kitty, err := termimg.NewKitty(&out)
	if err != nil {
		t.Fatal(err)
	}
	defer kitty.Close()
	fs := &fakeStickers{stickers: stickerMsgs(24, messages.MediaSticker)}
	src := &fakeMedia{dir: t.TempDir(), mediaPath: "../termimg/testdata/anim_alpha_lossless.webp"}
	for _, h := range []int{24, 40} {
		m := New(make(chan messages.Command, 20), []*messages.Conversation{{JID: "c@s.whatsapp.net", Name: "C", LastMsgTime: 1}},
			Options{SidebarWidth: 38, Images: termimg.ModeKitty, Kitty: kitty, Media: src, Stickers: fs, Clipboard: fakeClip{}})
		m.img.cellW, m.img.cellH = 9, 19
		next, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: h})
		m = next.(Model)
		m, _ = keys(t, m, "enter")
		m, cmds := keys(t, m, "s")
		m = drain(t, m, tea.Batch(cmds...))

		perRow, rows := m.stickerGrid()
		for step := 0; step < 6; step++ { // browse down past the first screen
			view := m.View()
			if n := strings.Count(view, "\n") + 1; n != h {
				t.Fatalf("height %d, step %d: view has %d lines", h, step, n)
			}
			imgs := placedImages(t, view)
			if len(imgs) == 0 {
				t.Fatalf("height %d: no stickers drawn", h)
			}
			for id, cells := range imgs {
				// every image must show all of its rows and columns
				maxR, maxC := 0, 0
				for c := range cells {
					maxR, maxC = max(maxR, c[0]), max(maxC, c[1])
				}
				if len(cells) != (maxR+1)*(maxC+1) || !cells[[2]int{0, 0}] {
					t.Fatalf("height %d, step %d: image %d is cut (%d of %dx%d cells)", h, step, id, len(cells), maxR+1, maxC+1)
				}
			}
			// the selected sticker's frame is on screen and its corners line up
			plain := stripANSI(view)
			if strings.Count(plain, "╭") != 1 || strings.Count(plain, "╰") != 1 {
				t.Fatalf("height %d, step %d (cursor %d, %d per row, %d rows): selection not fully visible",
					h, step, m.stk.cursor[tabStickers], perRow, rows)
			}
			if col := frameColumns(plain); len(col) != 1 {
				t.Fatalf("height %d, step %d: frame edges misaligned: left edges at columns %v", h, step, col)
			}
			m, cmds = keys(t, m, "j")
			m = drain(t, m, tea.Batch(cmds...))
		}
	}
}

func termimgDiacritics() []rune { return termimg.Diacritics() }

// frameColumns returns the distinct columns of the frame's left edge
// (╭, │ and ╰), ignoring placeholder cells' zero-width diacritics.
func frameColumns(plain string) map[int]bool {
	cols := map[int]bool{}
	for _, line := range strings.Split(plain, "\n") {
		col := 0
		for _, r := range line {
			if _, ok := diacriticIndex[r]; ok {
				continue
			}
			if r == '╭' || r == '╰' {
				cols[col] = true
			}
			if r == '│' && col < 3 { // the first-column cell's left edge only
				cols[col] = true
			}
			col++
		}
	}
	return cols
}
