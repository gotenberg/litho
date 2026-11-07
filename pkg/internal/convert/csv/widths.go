package csv

import (
	"context"
	"math"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// The column width arithmetic of Calc's CSV import (sc/source/ui/docshell
// docsh.cxx, ScDocShell::ConvertFrom, and sc/source/core/data
// column2.cxx, ScColumn::GetOptimalColWidth with its "simple text" path):
// each column with content takes the width of its longest string measured
// on a screen device, in pixels, plus the cell margins, two pixels and
// STD_EXTRA_WIDTH.
//
// # Corpus evidence
//
//   - litho: CSVs of one string per column ("a" to twelve "a", "n" to
//     thirty "n"), sales.csv's columns and a row of one value of each
//     detected type print with column widths that this arithmetic gives
//     to 0.03 points: "a" 14.08, twelve "a" 73.58, "Northwind Traders"
//     89.78, "2026-01-10" 58.13, "12/31/26 10:00 AM" 93.62,
//     "1.23456789012346E+019" 122.18 points; a column without content
//     keeps 64 points.
const (
	// screenPPEM is the size in pixels Liberation Sans 10 is measured at
	// on the 96 dpi screen device: 13.33 pixels, rounded to 13.
	screenPPEM = 13
	// screenPPTX is the pixels per twip of the 96 dpi screen (LibreOffice
	// Kit's ScGlobal::nScreenPPTX).
	screenPPTX = 96.0 / 1440
	// marginPixels is the cell's left and right margins, 20 twips each,
	// each truncated to whole pixels, plus the two pixels
	// GetOptimalColWidth adds.
	marginPixels = 2 + 2
	// extraTwips is STD_EXTRA_WIDTH, 2 mm: 113 twips.
	extraTwips = 113
	// maxWidthTwips bounds a width: half of 65,535 twips, the cap
	// GetOptimalColWidth applies.
	maxWidthTwips = 65535 / 2
)

// factorText is the string Calc measures on the printer and on the
// screen to find their ratio (ScDocShell::CalcOutputFactor).
const factorText = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz01234567890123456789"

// columns tracks what the column widths are computed from.
type columns struct {
	// head holds the kinds of each column's first four rows.
	head map[int]*[4]kind
	// cells holds each column's non-empty fields in row order.
	cells map[int][]colCell
	last  int
}

// colCell is one non-empty field of a column.
type colCell struct {
	text  string
	kind  kind
	value float64
}

// newColumns returns an empty tracker.
func newColumns() *columns {
	return &columns{head: map[int]*[4]kind{}, cells: map[int][]colCell{}, last: -1}
}

// add records the field raw of column col, row row, read as d.
func (c *columns) add(col, row int, raw string, d detected) {
	if row < 4 {
		h := c.head[col]
		if h == nil {
			h = &[4]kind{}
			c.head[col] = h
		}
		h[row] = d.kind
	}
	c.cells[col] = append(c.cells[col], colCell{text: raw, kind: d.kind, value: d.value})
	c.last = max(c.last, col)
}

// standard reports a kind of Calc's standard number format.
func standard(k kind) bool { return k == kText || k == kGeneral }

// format returns the number format column col's width is measured with:
// the first row's, or, when it is the standard format, the first of the
// next three rows' that is not (a header row above typed data).
func (c *columns) format(col int) kind {
	h := c.head[col]
	if h == nil {
		return kGeneral
	}
	for _, k := range h {
		if !standard(k) {
			return k
		}
	}
	return kGeneral
}

// longest returns the longest string of column col by UTF-16 length, the
// first of the longest, a text field's longest line: a text field as it
// reads, a value as the column's format shows it.
func (c *columns) longest(col int) string {
	f := c.format(col)
	best, n := "", 0
	for _, cell := range c.cells[col] {
		s := cell.text
		if cell.kind != kText {
			s = display(f, cell.value)
		}
		if cell.kind == kText && strings.Contains(s, "\n") {
			for _, line := range strings.Split(s, "\n") {
				if l := u16len(line); l > n {
					best, n = line, l
				}
			}
			continue
		}
		if l := u16len(s); l > n {
			best, n = s, l
		}
	}
	return best
}

// u16len returns the UTF-16 length of s, the length Calc compares.
func u16len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// widths returns the columns' widths in points, one Column per column
// with content.
func (c *columns) widths(ctx context.Context, fonts *text.Fonts) []sheet.Column {
	if fonts == nil || c.last < 0 {
		return nil
	}
	factor := outputFactor(ctx, fonts)
	if factor <= 0 {
		return nil
	}
	pptx := screenPPTX / factor
	var out []sheet.Column
	for col := 0; col <= c.last; col++ {
		if len(c.cells[col]) == 0 {
			continue
		}
		s := c.longest(col)
		if s == "" {
			continue
		}
		px, ok := pixels(ctx, fonts, s)
		if !ok {
			continue
		}
		tw := min(math.Floor(float64(px+marginPixels)/pptx), maxWidthTwips) + extraTwips
		out = append(out, sheet.Column{First: col, Last: col, Width: tw / 20})
	}
	return out
}

// shape returns the advances of s in Liberation Sans at size, without
// kerning, as Calc measures it.
func shape(ctx context.Context, fonts *text.Fonts, s string, size float64) ([]float64, bool) {
	p, _, err := fonts.Shape(ctx, []text.ParagraphRun{{Text: s, Style: text.Style{Families: []string{fontFamily}, Size: size, NoKerning: true}}}, text.ParagraphOptions{})
	if err != nil {
		return nil, false
	}
	adv := make([]float64, p.Len())
	for i := range adv {
		adv[i] = p.Advance(i)
	}
	return adv, true
}

// pixels returns the width of s on the screen device: each glyph's
// advance at [screenPPEM] rounded to whole pixels.
func pixels(ctx context.Context, fonts *text.Fonts, s string) (int, bool) {
	adv, ok := shape(ctx, fonts, s, screenPPEM)
	if !ok {
		return 0, false
	}
	px := 0
	for _, a := range adv {
		px += int(math.Floor(a + 0.5))
	}
	return px, true
}

// outputFactor returns Calc's printer-to-screen ratio: [factorText]'s
// width at 10 points over its width in whole pixels on the screen
// device, a 96 dpi pixel being 0.75 points.
func outputFactor(ctx context.Context, fonts *text.Fonts) float64 {
	adv, ok := shape(ctx, fonts, factorText, fontSize)
	if !ok {
		return 0
	}
	px, ok := pixels(ctx, fonts, factorText)
	if !ok || px == 0 {
		return 0
	}
	sum := 0.0
	for _, a := range adv {
		sum += a
	}
	return sum / (float64(px) * 0.75)
}
