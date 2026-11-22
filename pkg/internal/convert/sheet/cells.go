package sheet

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// maxOverflowScan bounds how many columns a cell's text is followed into
// its empty neighbours. Anti-DoS guard, not spec-mandated: a line of text
// crosses a few dozen columns at most.
const maxOverflowScan = 256

// maxCellText caps the characters of a cell drawn. Anti-DoS guard, not
// spec-mandated: Excel stops at 32,767 characters a cell.
const maxCellText = 32767

// shrinkAgainMax is how many times Calc shrinks a text by shrinkStep
// again after its first fit while it still overflows (ScOutputData,
// SC_SHRINKAGAIN_MAX).
const (
	shrinkAgainMax = 7
	shrinkStep     = 0.9
)

// slack widens a box sized to its measured text so that rounding never
// wraps it.
const slack = 0.5

// display is what a cell shows: its formatted text and how it aligns.
type display struct {
	text    string
	runs    []Run
	res     numfmt.Result
	value   bool
	hAlign  HAlign
	font    font
	wrap    bool
	shrink  bool
	textW   float64
	padding [4]float64
}

// format returns the format of the code, General for a code numfmt
// refuses, with a warning once per code; under Excel's rules, its Excel
// variant ([numfmt.Format.Excel]); formatting under the conversion's
// locale, a section that names its own keeping it.
func (b *builder) format(code string) *numfmt.Format {
	if f, ok := b.formats[code]; ok {
		return f
	}
	f, err := numfmt.Parse(code)
	if err != nil {
		b.warn(warn.ValueFormatUnsupported, warn.Approximated, "the number format "+quote(code)+" is not understood and shows as General")
		f = numfmt.General
	}
	if b.doc.ExcelRules {
		f = f.Excel()
	}
	f = f.WithLocale(b.opts.Locale)
	if len(b.formats) < maxFormats {
		b.formats[code] = f
	}
	return f
}

// maxFormats caps the parsed formats kept. Anti-DoS guard, not
// spec-mandated: a workbook holds at most a few hundred formats.
const maxFormats = 4096

// quote returns s quoted for a warning, shortened.
func quote(s string) string {
	if utf8.RuneCountInString(s) > 64 {
		s = string([]rune(s)[:64]) + "…"
	}
	return "\"" + s + "\""
}

// numValue returns the numfmt value of v.
func numValue(v Value) numfmt.Value {
	switch v.Kind {
	case Number:
		return numfmt.Value{Kind: numfmt.Number, Number: v.Number}
	case Bool:
		return numfmt.Value{Kind: numfmt.Bool, Number: v.Number}
	case Error:
		return numfmt.Value{Kind: numfmt.Error, Text: v.Text}
	case Text, Empty:
	}
	return numfmt.Value{Kind: numfmt.Text, Text: v.Text}
}

// printedError returns what an error value prints as under e, which is
// not ErrorsDisplayed or ErrorsBlank (ECMA-376 Part 1, 18.18.60
// ST_PrintError: "--" for dash, "#N/A" for NA).
//
// # Corpus evidence
//
//   - litho/xlsx print_errors: Excel prints #DIV/0!, #N/A and #VALUE! as
//     nothing under errors="blank", as "--" under "dash" and as "#N/A"
//     under "NA".
func printedError(e PrintErrors) string {
	if e == ErrorsDash {
		return "--"
	}
	return "#N/A"
}

// display returns what the unit u shows in its box, or false when it
// shows nothing.
func (b *pageBuilder) display(u *unit) (display, bool) {
	c := u.cell
	if !hasContent(c) {
		return display{}, false
	}
	if c.Value.Kind == Error && b.g.sh.Page.Errors != ErrorsDisplayed {
		if b.g.sh.Page.Errors == ErrorsBlank {
			return display{}, false
		}
		printed := *c
		printed.Value.Text, printed.Runs = printedError(b.g.sh.Page.Errors), nil
		c = &printed
	}
	st := u.st
	d := display{font: resolveFont(st.Font, b.doc.Default.Font), hAlign: st.Align.Horizontal, padding: b.padding(st)}
	d.font.excel = b.doc.ExcelRules
	if c.Value.Kind == Number && b.g.sh.HideZeros && c.Value.Number == 0 {
		return display{}, false
	}
	d.value = c.Value.Kind == Number || c.Value.Kind == Bool || c.Value.Kind == Error
	avail := u.w - d.padding[css.Left] - d.padding[css.Right] - st.Align.Indent
	if len(c.Runs) > 0 && c.Value.Kind != Number {
		d.runs = c.Runs
		var sb strings.Builder
		for _, r := range c.Runs {
			sb.WriteString(r.Text)
		}
		d.text = sb.String()
	} else {
		f := b.format(st.Format)
		v := numValue(c.Value)
		if c.Value.Kind == Text && c.Value.Text == "" && len(c.Runs) > 0 {
			v.Text = ""
		}
		measure := func(s string) float64 { return b.m.width(d.font, s) }
		if d.value && !st.Align.Shrink {
			d.res = f.Fit(v, avail, measure)
		} else {
			d.res = f.Apply(v)
		}
		d.text = d.res.Text
		if !d.res.Numeric && d.value && c.Value.Kind == Number {
			// A number in a text section aligns as text.
			d.value = false
		}
		if d.res.HasColor {
			d.font.color = d.res.Color
		}
	}
	d.wrap = (st.Align.Wrap || st.Align.Horizontal == HJustify || st.Align.Horizontal == HDistributed) && (!d.value || c.Value.Kind != Number)
	d.shrink = st.Align.Shrink && !d.wrap && st.Align.Horizontal != HFill
	// Every line break starts a line, wrapped or not: a carriage return
	// alone too, as Calc's edit engine reads one.
	d.text = normalizeBreaks(d.text)
	if len(d.runs) > 0 {
		runs := make([]Run, len(d.runs))
		for i, r := range d.runs {
			runs[i] = Run{Text: normalizeBreaks(r.Text), Font: r.Font}
		}
		d.runs = runs
	}
	if st.Align.Stacked {
		// Stacked text sets one character a line, centred (ODF
		// style:direction ttb; ECMA-376 Part 1, 18.8.1 textRotation 255),
		// each in its run's font.
		d.text = stack(d.text)
		d.runs = stackRuns(d.runs)
		d.wrap = false
		if d.hAlign == HGeneral {
			d.hAlign = HCenter
		}
	}
	if utf8.RuneCountInString(d.text) > maxCellText {
		d.text = string([]rune(d.text)[:maxCellText])
		d.runs = nil
	}
	if d.hAlign == HGeneral {
		// Calc aligns values, booleans and errors included, right and text
		// left, or right when it starts with a right-to-left character
		// (sc/source/ui/view/output2.cxx, getAlignmentFromContext).
		switch {
		case d.value:
			d.hAlign = HRight
		case startsRTL(d.text):
			d.hAlign = HRight
		default:
			d.hAlign = HLeft
		}
	}
	if d.res.Fill != 0 && !d.wrap {
		// Repeat the "*x" character to fill the cell, as Calc does
		// (ScDrawStringsVars::RepeatToFill).
		tw := b.m.width(d.font, d.text)
		// The fill is measured between two bars, so that a fill of
		// spaces, which the measure leaves out at a line's end, counts.
		fw := (b.m.width(d.font, "|"+strings.Repeat(string(d.res.Fill), 20)+"|") - b.m.width(d.font, "||")) / 20
		if fw > 0 && avail-tw > fw {
			n := min(int((avail-tw)/fw), maxCellText)
			at := min(max(d.res.FillAt, 0), len(d.text))
			d.text = d.text[:at] + strings.Repeat(string(d.res.Fill), n) + d.text[at:]
		}
	}
	d.textW = b.textWidth(&d)
	if st.Align.Horizontal == HFill && d.textW > 0 && !d.wrap {
		// Repeat the text to fill the cell (ScOutputData, bRepeat).
		n := int(avail / (d.textW + 0.01))
		if n > 1 {
			d.text = strings.Repeat(d.text, min(n, maxCellText/max(len(d.text), 1)))
			d.runs = nil
			d.textW = b.textWidth(&d)
		}
	}
	return d, d.text != ""
}

// textWidth returns the width of d's text on one line.
func (b *pageBuilder) textWidth(d *display) float64 {
	if len(d.runs) == 0 {
		w := 0.0
		for line := range strings.SplitSeq(d.text, "\n") {
			w = max(w, b.m.width(d.font, line))
		}
		return w
	}
	runs := make([]text.ParagraphRun, 0, len(d.runs))
	for _, r := range d.runs {
		runs = append(runs, text.ParagraphRun{Text: r.Text, Style: overlay(d.font, r.Font).textStyle(b.m.lang, b.m.fallback)})
	}
	return b.m.runsWidth(runs)
}

// startsRTL reports text whose first strong character is right to left.
func startsRTL(s string) bool {
	for _, r := range s {
		switch {
		case unicode.In(r, unicode.Hebrew, unicode.Arabic, unicode.Syriac, unicode.Thaana, unicode.Nko):
			return true
		case unicode.IsLetter(r):
			return false
		}
	}
	return false
}

// padding returns the inset of st's cells.
func (b *pageBuilder) padding(st *Style) [4]float64 {
	p := st.Padding
	if p == ([4]float64{}) {
		p = b.doc.Default.Padding
	}
	for i := range p {
		p[i] = min(max(p[i], 0), maxSide)
	}
	return p
}

// emptyAt reports a cell text may overflow into: no content, not part of
// a merge.
func (b *pageBuilder) emptyAt(r, c int) bool {
	if c < 0 || c >= maxColumns {
		return false
	}
	if _, ok := b.g.covered[[2]int{r, c}]; ok {
		return false
	}
	if _, ok := b.g.merges[[2]int{r, c}]; ok {
		return false
	}
	return !hasContent(b.g.cell(r, c))
}

// textNode returns the node that draws the unit's text in the region, and
// the horizontal span of its clip in region coordinates, nil when it shows
// nothing.
func (b *pageBuilder) textNode(rg *region, u *unit) (*layout.Node, float64, float64) {
	d, ok := b.display(u)
	if !ok {
		return nil, 0, 0
	}
	st := u.st
	g := b.g
	pad := d.padding
	indent := min(max(st.Align.Indent, 0), maxSide)
	left, right := pad[css.Left], pad[css.Right]
	switch d.hAlign {
	case HLeft, HJustify, HDistributed, HFill:
		left += indent
	case HRight:
		right += indent
	case HGeneral, HCenter, HCenterContinuous:
	}
	avail := u.w - left - right
	// The clip: the box, grown into the empty cells beside it while the
	// text needs room, on the sides its alignment leaves free.
	cx0, cx1 := u.x, u.x+u.w
	need := d.textW - avail
	if !d.wrap && !d.shrink && !d.value && need > 0 && !u.merged && st.Align.Rotation == 0 && !st.Align.Stacked {
		growRight := func(n float64) {
			c := u.lastC + 1
			for i := 0; n > 0 && i < maxOverflowScan && b.emptyAt(u.r, c); i, c = i+1, c+1 {
				w := g.colWidth(c)
				cx1 += w
				n -= w
			}
		}
		growLeft := func(n float64) {
			c := u.c - 1
			for i := 0; n > 0 && i < maxOverflowScan && b.emptyAt(u.r, c); i, c = i+1, c-1 {
				w := g.colWidth(c)
				cx0 -= w
				n -= w
			}
		}
		switch d.hAlign {
		case HLeft, HJustify, HDistributed, HFill, HGeneral:
			growRight(need)
		case HRight:
			growLeft(need)
		case HCenter, HCenterContinuous:
			growLeft(need / 2)
			growRight(need / 2)
		}
	}
	if st.Align.Horizontal == HCenterContinuous && !u.merged {
		// Centre across this cell and the empty cells to its right that
		// share the alignment.
		c := u.lastC + 1
		for i := 0; i < maxOverflowScan && b.emptyAt(u.r, c); i, c = i+1, c+1 {
			if s := g.style(u.r, c, g.cell(u.r, c)); s.Align.Horizontal != HCenterContinuous {
				break
			}
			cx1 += g.colWidth(c)
		}
		u = &unit{r: u.r, c: u.c, lastR: u.lastR, lastC: u.lastC, cell: u.cell, st: u.st, x: u.x, y: u.y, w: cx1 - u.x, h: u.h}
		avail = u.w - left - right
	}
	f := d.font
	if d.shrink && d.textW > avail && d.textW > 0 && avail > 0 {
		// Shrink to fit: the size times the available width over the
		// text's, then 10% less at a time while it overflows, at most
		// seven times (ScOutputData, SC_SHRINKAGAIN_MAX).
		scale := avail / d.textW
		for i := 0; i < shrinkAgainMax && d.textW*scale > avail; i++ {
			scale *= shrinkStep
		}
		f.size *= scale
		d.textW *= scale
	}
	lineH := b.m.lineHeight(f)
	// The text box, positioned in the region by the alignment.
	boxW := avail
	if !d.wrap {
		boxW = d.textW + slack
	}
	var boxX float64
	align := css.AlignLeft
	switch d.hAlign {
	case HRight:
		boxX = u.x + u.w - right - boxW
		align = css.AlignRight
	case HCenter, HCenterContinuous:
		boxX = u.x + left + float64((avail-boxW)/2)
		align = css.AlignCenter
	case HJustify:
		boxX = u.x + left
		align = css.AlignJustify
	case HDistributed:
		boxX = u.x + left
		align = css.AlignJustifyAll
	case HLeft, HGeneral, HFill:
		boxX = u.x + left
	}
	if d.wrap {
		boxX = u.x + left
	}
	if !b.doc.ExcelRules && !d.wrap && !d.value && len(d.runs) == 0 && !strings.Contains(d.text, "\n") && d.textW > 0 && st.Align.Rotation == 0 {
		// A clipped text is shortened as LibreOffice shortens it
		// (ScOutputData::LayoutStringsImpl, aShort): a left-aligned one
		// to its characters in the proportion its visible width bears to
		// its width, plus one, a right-aligned one from its end; a
		// centred one is kept whole. Excel writes the whole text and
		// clips it. A rotated text runs across the cell's height, not
		// its width, and is kept whole.
		rs := []rune(d.text)
		switch {
		case align == css.AlignLeft && boxX+d.textW > cx1+epsilon:
			ratio := (cx1 - right - boxX) / d.textW
			if n := int(ratio*float64(len(rs))) + 1; ratio > 0 && n < len(rs) {
				d.text = string(rs[:n])
				d.textW = b.m.width(f, d.text)
				boxW = d.textW + slack
			}
		case align == css.AlignRight && boxX+boxW-d.textW < cx0-epsilon:
			end := boxX + boxW
			ratio := (end - cx0 - left) / d.textW
			if n := int(ratio*float64(len(rs))) + 1; ratio > 0 && n < len(rs) {
				d.text = string(rs[len(rs)-n:])
				d.textW = b.m.width(f, d.text)
				boxW = d.textW + slack
				boxX = end - boxW
			}
		}
	}
	rotated := st.Align.Rotation != 0 && !st.Align.Stacked
	if !d.wrap && !rotated {
		// A line taller than its row runs past the row's bottom.
		ext := float64(lineH * float64(1+strings.Count(d.text, "\n")))
		b.textBottom = max(b.textBottom, u.y+max(u.h, ext+pad[css.Top]))
	}
	pitch, shift := lineH, 0.0
	padTop, padBottom := pad[css.Top], pad[css.Bottom]
	if z := b.g.zoom; b.doc.ExcelRules && z > 0 {
		// Excel keeps the top and bottom insets whole at any print scale.
		padTop, padBottom = padTop/z, padBottom/z
	}
	if b.doc.ExcelRules && !rotated && !st.Align.Stacked {
		if d.wrap {
			pitch = b.excelLine(f)
		}
		shift = b.excelShift(st.Align.Vertical, f, lineH, pitch, padBottom)
		if st.Align.Vertical == VBottom {
			// A row Excel measures sets its text above its deepest font.
			shift -= b.g.drops[u.lastR]
		}
		if len(d.runs) == 0 {
			// A cell whose own font is a script moves its line: a text node
			// takes no vertical-align of the block it fills.
			shift += b.excelScriptShift(st.Align.Vertical, f)
		}
		if shift > 0 {
			// The box moved down runs past the row's bottom, where the
			// cell's clip cuts it.
			b.textBottom = max(b.textBottom, u.y+u.h+shift)
		}
	}
	para := b.paragraph(&d, f, pitch, align, paraTurn{rotated: rotated, rtl: st.Align.RightToLeft})
	ik := innerKey{left: boxX - cx0, width: math.Max(boxW, 0.01), height: u.h, top: padTop, bottom: padBottom, vertical: st.Align.Vertical, shift: shift}
	var inner *css.Style
	if !rotated {
		inner = b.innerStyle(ik)
	} else {
		inner = b.newInnerStyle(ik)
		rot := st.Align.Rotation
		// Rotated text starts in the cell's bottom-left corner when it
		// turns counter-clockwise, its top-left when clockwise, the line
		// turned about its own corner and moved right by its height's
		// share so that it stays in the cell, as Calc sets a quarter
		// turn; other angles keep that anchor, an approximation.
		rot = min(max(rot, -90), 90)
		a := rot * math.Pi / 180
		w := d.textW + slack
		inner.Display = css.Block
		inner.Width = css.Pts(w)
		inner.Height = css.Pts(lineH)
		inner.Padding[css.Top], inner.Padding[css.Bottom] = css.Pts(0), css.Pts(0)
		inner.Left = css.Pts(u.x + pad[css.Left] + float64(lineH*math.Abs(detmath.Sin(a))) - cx0)
		if rot > 0 {
			inner.Top = css.Pts(u.h - pad[css.Bottom] - lineH)
			inner.TransformOrigin = [2]css.Dimension{{Kind: css.Percent}, {Kind: css.Percent, Value: 1}}
		} else {
			inner.Top = css.Pts(pad[css.Top])
			inner.TransformOrigin = [2]css.Dimension{{Kind: css.Percent}, {Kind: css.Percent}}
		}
		inner.Transform = []css.TransformFunc{{Kind: css.TransformRotate, V: [6]float64{-a}}}
		b.warn(warn.ContentUnsupported, warn.Approximated, "text rotated by other than a quarter turn starts in the cell's corner, an approximation of Calc's placement")
	}
	clip := b.clipStyle(clipKey{left: cx0 - rg.x0, width: math.Max(cx1-cx0, 0.01), height: u.h})
	n := &layout.Node{Kind: layout.Element, Style: clip, Role: "TD", Children: []*layout.Node{
		{Kind: layout.Element, Style: inner, Children: []*layout.Node{para}},
	}}
	if u.cell.Href != "" {
		para.Href = cellHref(u.cell.Href)
		para.Role = "Link"
	}
	return n, cx0, cx1
}

// excelScriptShift returns how far down Excel moves the text of a cell
// whose font f is a superscript or a subscript from where the script's
// own line sets it: raised half the size or lowered a fifth from the
// baseline the full-size font takes ([font.scriptShape]), a top-aligned
// superscript kept below the cell's top inset, where its own line starts.
//
// # Corpus evidence
//
//   - probe, xlsx: in rows of 36 points Excel for the web sets top-aligned
//     Calibri superscripts of 11 and 20 points on their own ascent under
//     the inset (62 and 110 printer pixels below the fill's top), and
//     subscripts a fifth of the size below the full-size font's top
//     baseline (110 and 196: 92 and 163, and 18 and 33); bottom-aligned
//     superscripts half the size above the row's bottom baseline.
func (b *pageBuilder) excelScriptShift(v VAlign, f font) float64 {
	_, up := f.scriptShape()
	if up == 0 {
		return 0
	}
	if v != VTop {
		return -float64(f.size * up)
	}
	if up > 0 {
		return 0
	}
	full := f
	full.script = Baseline
	own := b.m.lineGap(f) + b.m.lineHeight(f) - b.m.descent(f)
	at := b.m.lineGap(full) + b.m.lineHeight(full) - b.m.descent(full)
	return float64(at-own) - float64(f.size*up)
}

// excelShift returns how far down Excel sets a cell's text from where its
// box, padded 8 printer pixels at the top and 1 at the bottom, aligns
// lines pitch points apart in the font f, whose own line is lineH: a
// top-aligned first line starts at the padding, the face's line gap above
// its ascent and its leading below it; a
// centred text is centred in the padded box, the face's line gap above its
// ascent as GDI sets a line; a bottom-aligned last line
// sets its baseline the bottom baseline above the cell's bottom edge
// ([builder.bottomBaseline]), whatever the font, so that text in a
// larger font hangs below the cell and is cut there. Justified and
// distributed text is set from the top.
//
// # Corpus evidence
//
//   - litho/xlsx: fonts sets its Calibri 6, 8.5, 14, 24 and 36 point
//     cells and its Arial, Times New Roman, Courier New and Cambria cells
//     on the baseline of the Calibri 11 label beside them, 26 printer
//     pixels above the row's bottom, the descenders of the 36 point text
//     cut at the cell's bottom edge.
//   - All Excel references (litho/xlsx, libreoffice/xlsx, openxmlsdk/xlsx,
//     poi/xlsx): the first row of a page sets bottom-aligned Calibri 11
//     text 95 printer pixels below its top in rows of 121 (5,700 cells),
//     26 pixels above the bottom in rows of 127, 145, 151, 188, 228, 242
//     and 323 pixels; top-aligned Calibri 11 text 96 pixels below the top
//     (sc/tdf153767.xlsx, sc/tdf113271.xlsx, 64667.xlsx).
//   - All Excel references: centred text is set 8 pixels plus half the
//     room left by its line below the top: Calibri 14 at 119 pixels in a
//     row of 151 (sc/pivottable_duplicated_member_filter.xlsx), Calibri 12
//     at 101 in 127 (63934.xlsx), at 196.2 in 323 (fills_gradient), 337.8
//     in 605 (52716.xlsx).
//   - probe, xlsx: Excel for the web sets top-aligned Arial 11, Times New
//     Roman 11 and Arial 20 text in Calibri 11 books 0.46, 0.52 and 0.59
//     points lower than their ascent under the padding, the faces' line
//     gaps (0.36, 0.47, 0.65), and centred ones 0.20, 0.20 and 0.22 lower
//     than the middle, half of them; Calibri, without a line gap, sets on
//     its ascent.
func (b *pageBuilder) excelShift(v VAlign, f font, lineH, pitch, padBottom float64) float64 {
	half := float64((pitch - lineH) / 2)
	gap := b.m.lineGap(f)
	switch v {
	case VBottom:
		return padBottom + half + b.m.descent(f) - b.bottomBaseline(b.g.zoom)
	case VMiddle:
		return gap / 2
	case VTop, VJustify, VDistributed:
	}
	return gap - half
}

// bottomBaseline returns how far above a cell's bottom edge Excel sets
// the baseline of bottom-aligned text, in sheet points at the print scale
// z (0 for 100%): where the Default font's baseline falls in a row of the
// standard height, as the scale prints it ([excelZoomed]), when that
// font's line starts at the cell's top inset, which Excel keeps whole at
// any scale. At a small scale the baseline falls below the cell.
//
// # Corpus evidence
//
//   - All Excel references: Calibri 11 books set bottom-aligned text 26
//     printer pixels above the cell's bottom, where this gives 25.4 (121
//     less 8 and an ascent of 87.6); Arial 10 books 20 (103 less 8 and
//     75.1), Calibri 10 books 22.8 (openxmlsdk/xlsx NoExtDataE8.xlsx: 22),
//     Calibri 12 books 27 (sc/tdf155046.xlsx: 26.8).
//   - litho/xlsx: scale_percent sets its Calibri 11 text 52 printer pixels
//     below the top of rows of 61 at 50%, and 17 below the top of rows of
//     12 at 10% (this gives 51.8 and 16.6).
func (b *builder) bottomBaseline(z float64) float64 {
	if !b.baseSet {
		b.baseSet = true
		def := resolveFont(Font{}, b.doc.Default.Font)
		b.ascent = b.m.lineHeight(def) - b.m.descent(def)
		// Without a standard row the Default font's text sits on the
		// padding.
		b.base = max(b.doc.Default.Padding[css.Bottom], 0) + b.m.descent(def)
	}
	std := b.doc.StandardRowHeight
	if !numeric.Positive(std) {
		return b.base
	}
	top := b.doc.Default.Padding[css.Top]
	if z > 0 {
		std, top = excelZoomed(std, z), top/z
	}
	return float64(std-top) - b.ascent
}

// paragraph returns the block holding d's text in the font f, turned
// as t says.
func (b *pageBuilder) paragraph(d *display, f font, lineH float64, align css.TextAlign, t paraTurn) *layout.Node {
	st := b.paraStyle(f, lineH, align, d.wrap, t)
	n := &layout.Node{Kind: layout.Element, Style: st}
	if len(d.runs) == 0 {
		tn := &layout.Node{Kind: layout.Text, Style: st, Text: d.text}
		office.SymbolNode(tn, b.fonts, b.symbolRule(), b.symbols)
		n.Children = []*layout.Node{tn}
		return n
	}
	for _, r := range d.runs {
		rs := b.style()
		rf := overlay(f, r.Font)
		rf.apply(rs)
		rs.Display = css.Inline
		rs.WhiteSpace = st.WhiteSpace
		rs.LineHeight = st.LineHeight
		if !b.doc.ExcelRules && r.Font != nil {
			rs.LineHeight = css.Dimension{Kind: css.Number, Value: b.runLine(rf, d.wrap) / math.Max(rf.size, 0.01)}
		}
		if b.doc.ExcelRules && (rf.script != Baseline || !d.wrap) {
			// Excel sets a run of another size or a script on the
			// baseline the cell's alignment gives its line, without
			// growing the line.
			rs.LineHeight = css.Dimension{Kind: css.Number}
		}
		tn := &layout.Node{Kind: layout.Text, Style: rs, Text: r.Text}
		office.SymbolNode(tn, b.fonts, b.symbolRule(), b.symbols)
		n.Children = append(n.Children, &layout.Node{Kind: layout.Element, Style: rs, Children: []*layout.Node{tn}})
	}
	return n
}

// runLine returns the line of a run in the font f, wrapped or not: the
// face's own ascent and descent, so that a line of runs in several fonts
// is as tall as its tallest ascent and descent together, as LibreOffice's
// edit engine sets a line.
//
// # Corpus evidence
//
//   - litho/ods: cjk-cells prints its stacked Han characters, in Noto
//     Sans CJK JP 11 under a Liberation Sans 11 cell font, 16.02 points
//     apart, Noto Sans CJK's ascent and descent; indic-thai-cells its
//     wrapped Noto Sans Devanagari 11 lines 14.5 points apart.
func (b *pageBuilder) runLine(f font, wrap bool) float64 {
	h := b.m.lineHeight(f)
	if wrap {
		h = editLine(h)
	}
	return h
}

// stackRuns returns runs with a line break between their characters, for
// stacked text, nil for none.
func stackRuns(runs []Run) []Run {
	if len(runs) == 0 {
		return nil
	}
	out := make([]Run, 0, len(runs))
	first := true
	for _, r := range runs {
		var b strings.Builder
		for _, c := range r.Text {
			if !first {
				b.WriteByte('\n')
			}
			first = false
			b.WriteRune(c)
		}
		out = append(out, Run{Text: b.String(), Font: r.Font})
	}
	return out
}

// stack returns s with a line break between its characters, for stacked
// text.
func stack(s string) string {
	var b strings.Builder
	for i, r := range []rune(s) {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizeBreaks returns s with its CR LF and CR line breaks written LF
// and its other control characters but the tab left out, as Calc draws
// them.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/escape-unicode.xlsx, a cell that does not wrap holding
//     "Line 1\r\nLine 2\r\nLine 3\r\nLine 4", prints four lines in a
//     row grown to them.
//   - libreoffice/csv: sc/tdf80149.csv, a field holding U+0016, prints nothing for
//     it.
func normalizeBreaks(s string) string {
	if strings.ContainsRune(s, '\r') {
		s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	}
	// The other control characters draw nothing.
	return strings.Map(func(r rune) rune {
		if r != '\n' && r != '\t' && (r < 0x20 || r == 0x7f) {
			return -1
		}
		return r
	}, s)
}
