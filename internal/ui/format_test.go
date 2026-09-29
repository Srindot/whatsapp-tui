package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func spansString(sp []span) string {
	var b strings.Builder
	for _, s := range sp {
		tags := ""
		for _, t := range []struct {
			f textStyle
			n string
		}{{fmtBold, "B"}, {fmtItalic, "I"}, {fmtStrike, "S"}, {fmtCode, "C"}, {fmtLink, "L"}, {fmtMention, "M"}} {
			if s.style&t.f != 0 {
				tags += t.n
			}
		}
		if tags == "" {
			b.WriteString(s.text)
		} else {
			b.WriteString("<" + tags + ":" + s.text + ">")
		}
	}
	return b.String()
}

func TestParseInline(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text", "plain text"},
		{"this is *bold* ok", "this is <B:bold> ok"},
		{"_italic_ and ~gone~", "<I:italic> and <S:gone>"},
		{"*_both_*", "<BI:both>"},
		{"*bold phrase here*", "<B:bold phrase here>"},
		{"wow *nice*!", "wow <B:nice>!"},
		{"(*paren*)", "(<B:paren>)"},
		// not formatting
		{"2*3*4 = 24", "2*3*4 = 24"},
		{"snake_case_name", "snake_case_name"},
		{"a * b * c", "a * b * c"},
		{"*not closed", "*not closed"},
		{"** empty", "** empty"},
		{"email@x.com_", "email@x.com_"},
		// code: no formatting inside
		{"run `make *all*` now", "run <C:make *all*> now"},
		{"```mono _x_```", "<C:mono _x_>"},
		// links and mentions
		{"see https://example.com/a_b_c.", "see <L:https://example.com/a_b_c>."},
		{"go to www.iiit.ac.in", "go to <L:www.iiit.ac.in>"},
		{"hi @919876543210 !", "hi <M:@919876543210> !"},
		{"*bold https://x.io*", "<B:bold ><BL:https://x.io>"},
		{"ünïcode *wörds* ✨", "ünïcode <B:wörds> ✨"},
	}
	for _, tt := range tests {
		if got := spansString(parseInline(tt.in)); got != tt.want {
			t.Errorf("parseInline(%q)\n got %q\nwant %q", tt.in, got, tt.want)
		}
	}
}

var sgr = func(s string) string { return ansi.Strip(s) }

func TestFormatTextBlocks(t *testing.T) {
	base := lipgloss.NewStyle()
	in := "Plan:\n- buy *milk*\n- call mom\n1. first\n> quoted _words_\n```\nfunc main() {}\n```\nbye"
	got := formatText(in, 30, base, nil)
	var plain []string
	for _, l := range got {
		plain = append(plain, strings.TrimRight(sgr(l), " "))
	}
	want := []string{"Plan:", "• buy milk", "• call mom", "1. first", "▎quoted words", "func main() {}", "bye"}
	if strings.Join(plain, "|") != strings.Join(want, "|") {
		t.Fatalf("got  %q\nwant %q", plain, want)
	}
	// an unclosed fence is left as text
	if l := formatText("```\nnot a block", 30, base, nil); sgr(l[0]) != "```" {
		t.Fatalf("unclosed fence: %q", l)
	}
}

func TestWrapKeepsStyleAndWidth(t *testing.T) {
	base := lipgloss.NewStyle()
	lines := formatText("start *this bold phrase wraps across lines* end", 14, base, nil)
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 14 {
			t.Fatalf("line %d is %d wide: %q", i, w, sgr(l))
		}
	}
	if got := strings.Join(func() []string {
		var p []string
		for _, l := range lines {
			p = append(p, sgr(l))
		}
		return p
	}(), "|"); got != "start this|bold phrase|wraps across|lines end" {
		t.Fatalf("wrapped = %q", got)
	}
	// long words are split, lists indent their continuation lines
	for _, l := range formatText("- "+strings.Repeat("x", 40), 12, base, nil) {
		if ansi.StringWidth(l) > 12 {
			t.Fatalf("too wide: %q", sgr(l))
		}
	}
	if l := formatText("- aaa bbb ccc ddd", 9, base, nil); len(l) < 2 || !strings.HasPrefix(sgr(l[1]), "  ") {
		t.Fatalf("list continuation not indented: %q", l)
	}
}

func TestPlainText(t *testing.T) {
	if got := plainText("*Meeting* at _5pm_ ~cancelled~"); got != "Meeting at 5pm cancelled" {
		t.Fatalf("plainText = %q", got)
	}
}

func TestMentionsShownByName(t *testing.T) {
	names := map[string]string{"919876543210": "Arjun", "918331840042": "You"}
	lines := formatText("hey @919876543210 and @918331840042, also @11111111", 60, lipgloss.NewStyle(), names)
	if got := sgr(lines[0]); got != "hey @Arjun and @You, also @11111111" {
		t.Fatalf("got %q", got)
	}
	sp := withMentions(parseInline("@918331840042"), names)
	if sp[0].style&fmtMentionYou == 0 {
		t.Fatal("a mention of you should be highlighted as such")
	}
}
