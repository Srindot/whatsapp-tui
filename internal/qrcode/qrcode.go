/*
 *BSD 3-Clause License
 *
 *Copyright (c) 2017, Baozisoftware
 *All rights reserved.
 *
 *Redistribution and use in source and binary forms, with or without
 *modification, are permitted provided that the following conditions are met:
 *
 ** Redistributions of source code must retain the above copyright notice, this
 *  list of conditions and the following disclaimer.
 *
 ** Redistributions in binary form must reproduce the above copyright notice,
 *  this list of conditions and the following disclaimer in the documentation
 *  and/or other materials provided with the distribution.
 *
 ** Neither the name of the copyright holder nor the names of its
 *  contributors may be used to endorse or promote products derived from
 *  this software without specific prior written permission.
 *
 *THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
 *AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
 *IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
 *DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
 *FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
 *DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
 *SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
 *CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
 *OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
 *OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 */

package qrcode

import (
	"fmt"
	"github.com/mattn/go-colorable"
	"github.com/skip2/go-qrcode"
	"io"
	"strings"
)

type consoleColor string
type consoleColors struct {
	NormalBlack   consoleColor
	NormalRed     consoleColor
	NormalGreen   consoleColor
	NormalYellow  consoleColor
	NormalBlue    consoleColor
	NormalMagenta consoleColor
	NormalCyan    consoleColor
	NormalWhite   consoleColor
	BrightBlack   consoleColor
	BrightRed     consoleColor
	BrightGreen   consoleColor
	BrightYellow  consoleColor
	BrightBlue    consoleColor
	BrightMagenta consoleColor
	BrightCyan    consoleColor
	BrightWhite   consoleColor
}
type qrcodeRecoveryLevel qrcode.RecoveryLevel
type qrcodeRecoveryLevels struct {
	Low     qrcodeRecoveryLevel
	Medium  qrcodeRecoveryLevel
	High    qrcodeRecoveryLevel
	Highest qrcodeRecoveryLevel
}

var (
	ConsoleColors consoleColors = consoleColors{
		NormalBlack:   "\033[38;5;0m  \033[0m",
		NormalRed:     "\033[38;5;1m  \033[0m",
		NormalGreen:   "\033[38;5;2m  \033[0m",
		NormalYellow:  "\033[38;5;3m  \033[0m",
		NormalBlue:    "\033[38;5;4m  \033[0m",
		NormalMagenta: "\033[38;5;5m  \033[0m",
		NormalCyan:    "\033[38;5;6m  \033[0m",
		NormalWhite:   "\033[38;5;7m  \033[0m",
		BrightBlack:   "\033[48;5;0m  \033[0m",
		BrightRed:     "\033[48;5;1m  \033[0m",
		BrightGreen:   "\033[48;5;2m  \033[0m",
		BrightYellow:  "\033[48;5;3m  \033[0m",
		BrightBlue:    "\033[48;5;4m  \033[0m",
		BrightMagenta: "\033[48;5;5m  \033[0m",
		BrightCyan:    "\033[48;5;6m  \033[0m",
		BrightWhite:   "\033[48;5;7m  \033[0m"}
	QRCodeRecoveryLevels = qrcodeRecoveryLevels{
		Low:     qrcodeRecoveryLevel(qrcode.Low),
		Medium:  qrcodeRecoveryLevel(qrcode.Medium),
		High:    qrcodeRecoveryLevel(qrcode.High),
		Highest: qrcodeRecoveryLevel(qrcode.Highest)}
)

type QRCodeString string

func (v *QRCodeString) Print() {
	fmt.Fprintln(outer, *v)
}

type qrcodeTerminal struct {
	front   consoleColor
	back    consoleColor
	level   qrcodeRecoveryLevel
	compact bool
}

func (v *qrcodeTerminal) Get(content interface{}) (result *QRCodeString) {
	var qr *qrcode.QRCode
	var err error
	if t, ok := content.(string); ok {
		qr, err = qrcode.New(t, qrcode.RecoveryLevel(v.level))
	} else if t, ok := content.([]byte); ok {
		qr, err = qrcode.New(string(t), qrcode.RecoveryLevel(v.level))
	}

	if qr != nil && err == nil {
		bmp := qr.Bitmap()
		if v.compact {
			result = v.renderCompact(bmp)
		} else {
			result = v.renderSmall(bmp)
		}
	}
	return
}

func (v *qrcodeTerminal) renderSmall(data [][]bool) (result *QRCodeString) {
	// Using ANSI Inverse for maximum compatibility (monochrome)
	// We rely on the terminal's default colors:
	// Default: Black BG, White FG.
	// We want: White blocks (Background) and Black blocks (Foreground/Data).

	// INVERSE (\033[7m): Swaps FG and BG.
	// Space ' ' is usually BG color.
	// Space + Inverse = FG color (White).
	// Space + Normal = BG color (Black).

	reset := "\033[0m"

	// Characters
	// ▀ (Upper Half): Upper=FG(Black), Lower=BG(White)
	// ▄ (Lower Half): Lower=FG(Black), Upper=BG(White)

	str := "" // Initialize string builder

	rows := len(data)
	if rows == 0 {
		return
	}
	cols := len(data[0])

	// Skip margin (Quiet Zone) - standard is 4 modules.
	// User requested a "Small Border" (2 modules).
	margin := 2

	// Safety check: if data provided is smaller than 2*margin, don't strip
	if rows <= 2*margin || cols <= 2*margin {
		margin = 0
	}

	for r := margin; r < rows-margin; r += 2 {
		for c := margin; c < cols-margin; c++ {
			top := data[r][c]
			bot := false
			if r+1 < rows-margin {
				bot = data[r+1][c]
			}

			// Matrix: True = Black (Module), False = White (Background)
			// But wait, standard QR code: Dark modules on Light background.
			// data[][] = true means "Dark Module".
			// data[][] = false means "Light Module".

			// We want to print:
			// True = Black (Terminal Background, usually) -> Wait, if terminal is Black BG, we want True to be Light?
			// NO. QR Code: Dark Modules on Light Background.
			// Most terminals: White FG, Black BG.
			// So we want Background (False) to be White (FG color).
			// And Modules (True) to be Black (BG color).

			// Let's stick to standard printing:
			// We want 'False' (Background) to look White (Light).
			// We want 'True' (Module) to look Black (Dark).

			// Explicit ANSI Colors (Black on Bright White)
			// FG=Black (\033[30m), BG=Bright White (\033[107m)
			// This ensures high contrast (Pure White vs Black).
			// █ (Full Block) -> Uses FG Color (Black)
			//   (Space)      -> Uses BG Color (Bright White)
			// ▀ (Upper Half) -> Top=FG(Black), Bot=BG(Bright White)
			// ▄ (Lower Half) -> Top=BG(Bright White), Bot=FG(Black)

			colorPrefix := "\033[30m\033[107m" // Black FG, Bright White BG

			if top && bot {
				// Both Black -> Full Block (FG=Black)
				str += colorPrefix + "█" + reset
			} else if !top && !bot {
				// Both White -> Space (BG=White)
				str += colorPrefix + " " + reset
			} else if top && !bot {
				// Top Black, Bot White -> Upper Half '▀' (Top=FG=Black, Bot=BG=White)
				str += colorPrefix + "▀" + reset
			} else {
				// Top White, Bot Black -> Lower Half '▄' (Bot=FG=Black, Top=BG=White)
				str += colorPrefix + "▄" + reset
			}
		}
		str += fmt.Sprintln()
	}
	obj := QRCodeString(str)
	result = &obj
	return
}

// octantChars maps a 2x4 module block to a Unicode block character, indexed
// by a bitmask of dark modules laid out row-major (bit 0 = top-left,
// bit 1 = top-right, ..., bit 7 = bottom-right). Most entries are Unicode 16
// "BLOCK OCTANT-n" characters; the rest reuse existing half, quadrant and
// quarter blocks. Generated from the Unicode 17 character names.
var octantChars = [256]rune{0x0020, 0x1CEA8, 0x1CEAB, 0x1FB82, 0x1CD00, 0x2598, 0x1CD01, 0x1CD02,
	0x1CD03, 0x1CD04, 0x259D, 0x1CD05, 0x1CD06, 0x1CD07, 0x1CD08, 0x2580,
	0x1CD09, 0x1CD0A, 0x1CD0B, 0x1CD0C, 0x1FBE6, 0x1CD0D, 0x1CD0E, 0x1CD0F,
	0x1CD10, 0x1CD11, 0x1CD12, 0x1CD13, 0x1CD14, 0x1CD15, 0x1CD16, 0x1CD17,
	0x1CD18, 0x1CD19, 0x1CD1A, 0x1CD1B, 0x1CD1C, 0x1CD1D, 0x1CD1E, 0x1CD1F,
	0x1FBE7, 0x1CD20, 0x1CD21, 0x1CD22, 0x1CD23, 0x1CD24, 0x1CD25, 0x1CD26,
	0x1CD27, 0x1CD28, 0x1CD29, 0x1CD2A, 0x1CD2B, 0x1CD2C, 0x1CD2D, 0x1CD2E,
	0x1CD2F, 0x1CD30, 0x1CD31, 0x1CD32, 0x1CD33, 0x1CD34, 0x1CD35, 0x1FB85,
	0x1CEA3, 0x1CD36, 0x1CD37, 0x1CD38, 0x1CD39, 0x1CD3A, 0x1CD3B, 0x1CD3C,
	0x1CD3D, 0x1CD3E, 0x1CD3F, 0x1CD40, 0x1CD41, 0x1CD42, 0x1CD43, 0x1CD44,
	0x2596, 0x1CD45, 0x1CD46, 0x1CD47, 0x1CD48, 0x258C, 0x1CD49, 0x1CD4A,
	0x1CD4B, 0x1CD4C, 0x259E, 0x1CD4D, 0x1CD4E, 0x1CD4F, 0x1CD50, 0x259B,
	0x1CD51, 0x1CD52, 0x1CD53, 0x1CD54, 0x1CD55, 0x1CD56, 0x1CD57, 0x1CD58,
	0x1CD59, 0x1CD5A, 0x1CD5B, 0x1CD5C, 0x1CD5D, 0x1CD5E, 0x1CD5F, 0x1CD60,
	0x1CD61, 0x1CD62, 0x1CD63, 0x1CD64, 0x1CD65, 0x1CD66, 0x1CD67, 0x1CD68,
	0x1CD69, 0x1CD6A, 0x1CD6B, 0x1CD6C, 0x1CD6D, 0x1CD6E, 0x1CD6F, 0x1CD70,
	0x1CEA0, 0x1CD71, 0x1CD72, 0x1CD73, 0x1CD74, 0x1CD75, 0x1CD76, 0x1CD77,
	0x1CD78, 0x1CD79, 0x1CD7A, 0x1CD7B, 0x1CD7C, 0x1CD7D, 0x1CD7E, 0x1CD7F,
	0x1CD80, 0x1CD81, 0x1CD82, 0x1CD83, 0x1CD84, 0x1CD85, 0x1CD86, 0x1CD87,
	0x1CD88, 0x1CD89, 0x1CD8A, 0x1CD8B, 0x1CD8C, 0x1CD8D, 0x1CD8E, 0x1CD8F,
	0x2597, 0x1CD90, 0x1CD91, 0x1CD92, 0x1CD93, 0x259A, 0x1CD94, 0x1CD95,
	0x1CD96, 0x1CD97, 0x2590, 0x1CD98, 0x1CD99, 0x1CD9A, 0x1CD9B, 0x259C,
	0x1CD9C, 0x1CD9D, 0x1CD9E, 0x1CD9F, 0x1CDA0, 0x1CDA1, 0x1CDA2, 0x1CDA3,
	0x1CDA4, 0x1CDA5, 0x1CDA6, 0x1CDA7, 0x1CDA8, 0x1CDA9, 0x1CDAA, 0x1CDAB,
	0x2582, 0x1CDAC, 0x1CDAD, 0x1CDAE, 0x1CDAF, 0x1CDB0, 0x1CDB1, 0x1CDB2,
	0x1CDB3, 0x1CDB4, 0x1CDB5, 0x1CDB6, 0x1CDB7, 0x1CDB8, 0x1CDB9, 0x1CDBA,
	0x1CDBB, 0x1CDBC, 0x1CDBD, 0x1CDBE, 0x1CDBF, 0x1CDC0, 0x1CDC1, 0x1CDC2,
	0x1CDC3, 0x1CDC4, 0x1CDC5, 0x1CDC6, 0x1CDC7, 0x1CDC8, 0x1CDC9, 0x1CDCA,
	0x1CDCB, 0x1CDCC, 0x1CDCD, 0x1CDCE, 0x1CDCF, 0x1CDD0, 0x1CDD1, 0x1CDD2,
	0x1CDD3, 0x1CDD4, 0x1CDD5, 0x1CDD6, 0x1CDD7, 0x1CDD8, 0x1CDD9, 0x1CDDA,
	0x2584, 0x1CDDB, 0x1CDDC, 0x1CDDD, 0x1CDDE, 0x2599, 0x1CDDF, 0x1CDE0,
	0x1CDE1, 0x1CDE2, 0x259F, 0x1CDE3, 0x2586, 0x1CDE4, 0x1CDE5, 0x2588,
}

// renderCompact draws 2x4 modules per character cell using octant blocks.
// Terminal cells are about twice as tall as wide, so modules stay square and
// the code is half the width and height of renderSmall. Needs a terminal that
// renders Unicode 16 octants (e.g. recent kitty).
func (v *qrcodeTerminal) renderCompact(data [][]bool) (result *QRCodeString) {
	rows := len(data)
	if rows == 0 {
		return
	}
	cols := len(data[0])

	margin := 2
	if rows <= 2*margin || cols <= 2*margin {
		margin = 0
	}
	at := func(r, c int) bool {
		return r < rows-margin && c < cols-margin && data[r][c]
	}

	var sb strings.Builder
	for r := margin; r < rows-margin; r += 4 {
		// Black FG on Bright White BG for the whole line
		sb.WriteString("\033[30m\033[107m")
		for c := margin; c < cols-margin; c += 2 {
			idx := 0
			for dr := 0; dr < 4; dr++ {
				for dc := 0; dc < 2; dc++ {
					if at(r+dr, c+dc) {
						idx |= 1 << (dr*2 + dc)
					}
				}
			}
			sb.WriteRune(octantChars[idx])
		}
		sb.WriteString("\033[0m\n")
	}
	obj := QRCodeString(sb.String())
	result = &obj
	return
}

// SetCompact switches to octant-block rendering (half width and height).
func (v *qrcodeTerminal) SetCompact(compact bool) *qrcodeTerminal {
	v.compact = compact
	return v
}

func New() *qrcodeTerminal {
	// Level Low is sufficient for cleaner/smaller QR codes
	return &qrcodeTerminal{
		level: qrcodeRecoveryLevel(qrcode.Low),
	}
}

func (_ *qrcodeTerminal) SetOutput(out io.Writer) {
	outer = out
}

var outer = colorable.NewColorableStdout()
