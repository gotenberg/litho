package sheet

import (
	"math"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxOverflowScan bounds how many columns a cell's text is followed into
// its empty neighbours. Anti-DoS guard, not spec-mandated: a line of text
// crosses a few dozen columns at most.
const maxOverflowScan = 256

// maxCellText caps the characters of a cell drawn. Anti-DoS guard, not
// spec-mandated: Excel stops at 32,767 characters a cell.
const maxCellText = 32767

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
// refuses, with a warning once per code.
func (b *builder) format(code string) *numfmt.Format {
	if f, ok := b.formats[code]; ok {
		return f
	}
	f, err := numfmt.Parse(code)
	if err != nil {
		b.warn(CodeFormat, "the number format "+quote(code)+" is not understood and shows as General")
		f = numfmt.General
	}
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

// value returns the numfmt value of v.
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

// display returns what the unit u shows in its box, or false when it
// shows nothing.
func (b *pageBuilder) display(u *unit) (display, bool) {
	c := u.cell
	if !hasContent(c) {
		return display{}, false
	}
	st := u.st
	d := display{font: resolveFont(st.Font, b.doc.Default.Font), hAlign: st.Align.Horizontal, padding: b.padding(st)}
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
		// style:direction ttb; ECMA-376 Part 1, 18.8.1 textRotation 255).
		d.text = stack(d.text)
		d.runs = nil
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
		for _, line := range strings.Split(d.text, "\n") {
			w = max(w, b.m.width(d.font, line))
		}
		return w
	}
	runs := make([]text.ParagraphRun, 0, len(d.runs))
	for _, r := range d.runs {
		runs = append(runs, text.ParagraphRun{Text: r.Text, Style: overlay(d.font, r.Font).textStyle(b.m.lang)})
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
		for i := 0; i < 7 && d.textW*scale > avail; i++ {
			scale *= 0.9
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
		boxX = u.x + left + (avail-boxW)/2
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
	if !d.wrap && !d.value && len(d.runs) == 0 && !strings.Contains(d.text, "\n") && d.textW > 0 {
		// A clipped text is shortened as LibreOffice shortens it
		// (ScOutputData::LayoutStringsImpl, aShort): a left-aligned one
		// to its characters in the proportion its visible width bears to
		// its width, plus one, a right-aligned one from its end; a
		// centred one is kept whole.
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
	para := b.paragraph(&d, f, lineH, align)
	inner := b.style()
	inner.Position = css.Absolute
	inner.Left = css.Pts(boxX - cx0)
	inner.Top = css.Pts(0)
	inner.Width = css.Pts(math.Max(boxW, 0.01))
	inner.Height = css.Pts(u.h)
	inner.BoxSizing = css.BorderBox
	inner.Padding[css.Top] = css.Pts(pad[css.Top])
	inner.Padding[css.Bottom] = css.Pts(pad[css.Bottom])
	inner.Display = css.Flex
	inner.FlexDirection = css.FlexColumn
	switch st.Align.Vertical {
	case VTop, VJustify, VDistributed:
		inner.JustifyContent = css.BoxFlexStart
	case VMiddle:
		inner.JustifyContent = css.BoxCenter
	case VBottom:
		inner.JustifyContent = css.BoxFlexEnd
	}
	if rot := st.Align.Rotation; rot != 0 && !st.Align.Stacked {
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
		inner.Left = css.Pts(u.x + pad[css.Left] + lineH*math.Abs(math.Sin(a)) - cx0)
		if rot > 0 {
			inner.Top = css.Pts(u.h - pad[css.Bottom] - lineH)
			inner.TransformOrigin = [2]css.Dimension{{Kind: css.Percent}, {Kind: css.Percent, Value: 1}}
		} else {
			inner.Top = css.Pts(pad[css.Top])
			inner.TransformOrigin = [2]css.Dimension{{Kind: css.Percent}, {Kind: css.Percent}}
		}
		inner.Transform = []css.TransformFunc{{Kind: css.TransformRotate, V: [6]float64{-a}}}
		para.Style.TextAlign = css.AlignLeft
		b.warnOnce(CodeRotation, "text rotated by other than a quarter turn starts in the cell's corner, an approximation of Calc's placement")
	}
	clip := b.style()
	clip.Position = css.Absolute
	clip.Left = css.Pts(cx0 - rg.x0)
	clip.Top = css.Pts(0)
	clip.Width = css.Pts(math.Max(cx1-cx0, 0.01))
	clip.Height = css.Pts(u.h)
	clip.Overflow = css.OverflowHidden
	if st.Align.RightToLeft {
		para.Style.Direction = css.RTL
	}
	n := &layout.Node{Kind: layout.Element, Style: clip, Role: "TD", Children: []*layout.Node{
		{Kind: layout.Element, Style: inner, Children: []*layout.Node{para}},
	}}
	if u.cell.Href != "" {
		para.Href = cellHref(u.cell.Href)
	}
	return n, cx0, cx1
}

// paragraph returns the block holding d's text in the font f.
func (b *pageBuilder) paragraph(d *display, f font, lineH float64, align css.TextAlign) *layout.Node {
	st := b.style()
	f.apply(st)
	st.Display = css.Block
	st.TextAlign = align
	st.TextAlignLast = css.AlignAuto
	if align == css.AlignJustifyAll {
		st.TextAlign = css.AlignJustify
		st.TextAlignLast = css.AlignJustify
	}
	st.LineHeight = css.Dimension{Kind: css.Number, Value: lineH / math.Max(f.size, 0.01)}
	st.WhiteSpace = css.Pre
	if d.wrap {
		st.WhiteSpace = css.PreWrap
		st.OverflowWrap = css.BreakWord
		st.LineHeight.Value = editLine(lineH) / math.Max(f.size, 0.01)
	}
	n := &layout.Node{Kind: layout.Element, Style: st}
	if len(d.runs) == 0 {
		n.Children = []*layout.Node{{Kind: layout.Text, Style: st, Text: d.text}}
		return n
	}
	for _, r := range d.runs {
		rs := b.style()
		overlay(f, r.Font).apply(rs)
		rs.Display = css.Inline
		rs.WhiteSpace = st.WhiteSpace
		rs.LineHeight = st.LineHeight
		n.Children = append(n.Children, &layout.Node{
			Kind: layout.Element, Style: rs,
			Children: []*layout.Node{{Kind: layout.Text, Style: rs, Text: r.Text}},
		})
	}
	return n
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
//   - xlsx: sc/escape-unicode.xlsx, a cell that does not wrap holding
//     "Line 1\r\nLine 2\r\nLine 3\r\nLine 4", prints four lines in a
//     row grown to them.
//   - csv: sc/tdf80149.csv, a field holding U+0016, prints nothing for
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
