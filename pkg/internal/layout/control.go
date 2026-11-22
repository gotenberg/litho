package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// ControlKind is the widget a form control renders as (HTML Standard,
// 15.5).
type ControlKind uint8

// The widgets a [Control] sizes.
const (
	// ControlTextField is a single-line text entry widget (15.5.15).
	ControlTextField ControlKind = iota
	// ControlTextArea is a multiline text entry widget (15.5.17).
	ControlTextArea
	// ControlDropDown is a select shown as a one-line drop-down box
	// (15.5.16).
	ControlDropDown
	// ControlListBox is a select shown as a list box (15.5.16).
	ControlListBox
	// ControlButton is a button: a button element, an input of type
	// submit, reset or button, or a file control's button (15.5.2 and
	// 15.5.12 to 15.5.14).
	ControlButton
	// ControlCheckBox is a checkbox (15.5.6).
	ControlCheckBox
	// ControlRadio is a radio button (15.5.7).
	ControlRadio
	// ControlRange is a slider (15.5.11).
	ControlRange
	// ControlProgress is a progress bar (15.5.18).
	ControlProgress
	// ControlMeter is a gauge (15.5.19).
	ControlMeter
	// ControlColor is a colour well (15.5.4).
	ControlColor
	// ControlDate is a date, month, week or local date and time field,
	// which shows a calendar (15.5.8 and 15.5.9).
	ControlDate
	// ControlTime is a time field, which shows a clock (15.5.10).
	ControlTime
	// ControlFile is a file upload control, its button and its text
	// (15.5.13).
	ControlFile
)

// Control is what sizes a form control by default, its preferred size
// under field-sizing: fixed (CSS Form Control Styling 1, 7.1): the width
// and height of its content box when its style leaves them auto, from the
// primary font of its style ([engine.controlStyle]). Under field-sizing:
// content its content sizes it.
type Control struct {
	Kind ControlKind
	// Chars is the character width of a text field, its size attribute,
	// and the cols of a textarea.
	Chars int
	// Rows is the rows of a textarea, and the display size of a list box.
	Rows int
	// Labels are the labels of a select's options and option group
	// headings, the widest of which sizes it, and the text a date or time
	// field is as wide as.
	Labels []ControlLabel
	// Fields is the number of fields a date or time field shows.
	Fields int
	// Checked is the checkedness of a checkbox or a radio button, and
	// Disabled whether the control is disabled (HTML Standard,
	// 4.10.18.5).
	Checked, Disabled bool
	// Value places a range's thumb and fills a progress bar or a meter,
	// from 0 to 1; a progress bar without a value is indeterminate at -1.
	Value float64
	// Level is a meter's region.
	Level MeterLevel
	// Swatch is a colour well's colour.
	Swatch typeset.Color
}

// ControlLabel is one row of a select: an option's label, or an option
// group's heading, set bold, and indented by Indent points.
type ControlLabel struct {
	Text   string
	Bold   bool
	Indent float64
}

// Sizes of a control's parts that are not in characters, in CSS pixels.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints, with
//     border and padding 0, a textarea 15 pixels wider than its cols in
//     characters at every font and size, a drop-down box 20 pixels wider
//     than its widest option, an empty one 20 pixels wide, a list box 4
//     pixels wider than its widest option, its rows one pixel taller than
//     the larger of their line and 1.2 em; a drop-down box mostly 2
//     pixels taller than its line.
const (
	// textAreaScrollbar is the room a textarea keeps for its vertical
	// scroll bar, which HTML Standard, 15.5.17 adds to its width.
	textAreaScrollbar = 15
	// dropDownArrow is the room a drop-down box keeps for its button, and
	// dropDownBlock what it adds to its line's height, half above and half
	// below.
	dropDownArrow = 20
	dropDownBlock = 2
	// listBoxInline is the inline padding of a list box's option, both
	// sides, and listBoxRowEnd its block-end padding.
	listBoxInline = 4
	listBoxRowEnd = 1
	// listBoxRowMin is the least height of a list box's row, in em.
	listBoxRowMin = 1.2
)

// controlStyle returns the style of the box of the form control c styled
// st in language lang: st itself under field-sizing: content, else a copy
// whose width and height, where st leaves them auto, are the control's
// default size, and which clips what passes a textarea's or a list box's
// rows, as their scroll containers do.
//
// A text field is (size − 1) × avg + max wide, avg and max the average
// and maximum character widths of the primary font (HTML Standard,
// 15.5.15, converting a character width to pixels), and one line tall; a
// textarea cols × avg wide plus a scroll bar and rows lines tall
// (15.5.17); a drop-down box as wide as its widest option and its button,
// and one line tall and a little more; a list box as wide as its widest
// option and as many rows tall as Rows (15.5.16). The widths are Blink's:
// avg and max read at the font size cut to 1/64 pixel, max rounded to a
// whole pixel, avg too when it rounds up, the width rounded up to a whole
// pixel. A font that records no average width takes the advance of 0 as
// avg and no max, as Blink does.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints 164 text
//     fields in Liberation Sans, Serif and Mono, DejaVu Sans, Sans Mono and
//     Serif and Noto Sans, from 10 to 30 pixels, sizes 1 to 100, with
//     border and padding 0: 160 as wide as this rule gives, four one pixel
//     wider (Liberation Serif at 13.333 pixels, size 20); each one line
//     tall; textareas of 10, 20 and 40 columns by the same avg, their rows
//     lines tall; drop-down boxes as wide as their widest option and 20
//     pixels, and 2 pixels taller than their line in 28 of 33 boxes, 1 or
//     3 in the others.
func (e *engine) controlStyle(c *Control, st *css.Style, lang string) *css.Style {
	switch c.Kind { //nolint:exhaustive // the fields are sized below
	case ControlCheckBox, ControlRadio, ControlRange:
		return widgetBoxStyle(c, st)
	case ControlButton, ControlProgress, ControlMeter, ControlColor:
		// Sized by their content or by the user agent sheet.
		return st
	case ControlDropDown:
		st = dropDownPadding(st)
	}
	if st.FieldSizing != css.FieldFixed {
		return st
	}
	setW, setH := st.Width.Kind == css.Auto, st.Height.Kind == css.Auto
	clip := c.Kind == ControlTextArea || c.Kind == ControlListBox
	if !setW && !setH && (!clip || st.Overflow != css.OverflowVisible) {
		return st
	}
	size := e.fontSize(st)
	if size <= 0 {
		return st
	}
	m := e.faceMetrics(st, lang)
	px := float64(math.Floor(size/css.Pt*64) / 64)
	avg, maxW := m.AvgCharWidth/size*px, m.MaxCharWidth/size*px
	if !(avg > 0) {
		// A face without an average width: the advance of 0 alone.
		avg, maxW = e.fonts.Aspect(e.textStyle(st, lang), text.AdjustChWidth)*px, 0
	} else {
		avg = max(avg, math.Round(avg))
		maxW = math.Round(maxW)
	}
	lh := e.lineHeight(st, lang)
	// above and below are what the control adds above and below its line
	// inside its content box, as padding when its padding is a length.
	var w, h, above, below float64
	switch c.Kind { //nolint:exhaustive // the other controls return above: their widget or their content sizes them
	case ControlTextField:
		chars := max(c.Chars, 1)
		if maxW > 0 {
			w = math.Ceil(float64(avg*float64(chars-1)) + maxW)
		} else {
			w = math.Ceil(avg * float64(chars))
		}
		h = lh
	case ControlTextArea:
		w = math.Ceil(float64(avg*float64(max(c.Chars, 1))) + textAreaScrollbar)
		h = float64(lh * float64(max(c.Rows, 1)))
	case ControlDropDown:
		w = e.labelsWidth(c.Labels, st, lang) / css.Pt
		if st.Appearance == css.AppearanceNone {
			// No arrow and no room for it.
			w = math.Ceil(w)
		} else {
			// The room for the arrow is in the padding.
			w = math.Ceil(w+dropDownArrow) - dropDownArrow
		}
		h = lh
		above, below = dropDownBlock/2*css.Pt, dropDownBlock/2*css.Pt
	case ControlListBox:
		w = math.Round(e.labelsWidth(c.Labels, st, lang)/css.Pt + listBoxInline)
		row := max(lh, float64(listBoxRowMin*size)) + listBoxRowEnd*css.Pt
		h = float64(row * float64(max(c.Rows, 1)))
	case ControlFile:
		// As wide as 34 zeros, its height its content's.
		w = math.Ceil(float64(fileChars * e.labelsWidth([]ControlLabel{{Text: "0"}}, st, lang) / css.Pt))
		setH = false
	case ControlDate, ControlTime:
		// The fields' text, a pixel of padding either side of each
		// field, then the room of the picker's icon, which scales with
		// the font.
		room := dateIconRoom
		if c.Kind == ControlTime {
			room = timeIconRoom
		}
		scale := size / css.Pt / smallControl
		w = math.Ceil(e.labelsWidth(c.Labels, st, lang)/css.Pt + float64(2*c.Fields) + float64(room*scale))
		h = lh
		if c.Kind == ControlTime {
			above, below = float64(scale*css.Pt), float64((timeIconBlock-1)*scale*css.Pt)
		}
	}
	out := *st
	if setH && (above > 0 || below > 0) {
		if t, b := out.Padding[css.Top], out.Padding[css.Bottom]; t.Kind == css.Points && b.Kind == css.Points {
			out.Padding[css.Top], out.Padding[css.Bottom] = css.Pts(t.Value+above), css.Pts(b.Value+below)
		} else {
			h += above + below
		}
	}
	if setW {
		out.Width = css.Pts(float64(w*css.Pt) + boxSizingEdges(&out, true))
	}
	if setH {
		out.Height = css.Pts(h + boxSizingEdges(&out, false))
	}
	if clip && out.Overflow == css.OverflowVisible {
		out.Overflow = css.OverflowHidden
	}
	return &out
}

// dropDownPadding returns the style of a drop-down box styled st whose
// arrow shows: its text 4 pixels in from its left padding edge and the
// rest of [dropDownArrow], which holds the arrow, at its right, as
// padding. Under appearance: none it shows no arrow and keeps no room.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets the text of
//     a drop-down box 4 pixels in from its border, as wide as its text
//     and 20 pixels, under an author border too; under appearance: none
//     at its border, as wide as its text.
func dropDownPadding(st *css.Style) *css.Style {
	l, r := st.Padding[css.Left], st.Padding[css.Right]
	if st.Appearance == css.AppearanceNone || l.Kind != css.Points || r.Kind != css.Points {
		return st
	}
	out := *st
	out.Padding[css.Left] = css.Pts(l.Value + dropDownText*css.Pt)
	out.Padding[css.Right] = css.Pts(r.Value + (dropDownArrow-dropDownText)*css.Pt)
	return &out
}

// dropDownText is the room before a drop-down box's text, in CSS pixels.
const dropDownText = 4

// The room a date or time field keeps for its picker's icon, at the
// control font's 13.333 pixels: its gap and its width beside a date, a
// month, a week or a local date and time, beside a time; and what a time
// field adds to its line's height for its clock.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints date,
//     month, week and local date and time fields 31.5 to 31.7 pixels
//     wider than their text and a pixel of padding either side of each
//     field, time fields 26.7 to 26.8 pixels wider and 3 pixels taller
//     than their line, their text a pixel lower than a text field's.
const (
	// fileChars is how many zeros a file upload control is as wide as.
	fileChars     = 34
	smallControl  = 40.0 / 3
	dateIconRoom  = 31.6
	timeIconRoom  = 26.7
	timeIconBlock = 3
)

// widgetBoxStyle returns the style of the box of a checkbox, a radio
// button or a range c styled st: drawn as its widget, a checkbox and a
// radio button are 13 by 13 pixels where st leaves their size auto and
// keep no border or padding; a range is 129 by 16 pixels.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 prints check
//     boxes and radio buttons 13 by 13 pixels at any font size, the same
//     under a 3 pixel border or a padding, ranges 129 by 16 pixels, under
//     appearance: none too.
func widgetBoxStyle(c *Control, st *css.Style) *css.Style {
	w, h := float64(widgetBox), float64(widgetBox)
	if c.Kind == ControlRange {
		w, h = rangeWidth, rangeHeight
	}
	themed := c.Kind == ControlRange || st.Appearance != css.AppearanceNone
	setW, setH := themed && st.Width.Kind == css.Auto, themed && st.Height.Kind == css.Auto
	strip := c.Kind != ControlRange && st.Appearance != css.AppearanceNone && hasEdges(st)
	if !setW && !setH && !strip {
		return st
	}
	out := *st
	if strip {
		out.Border = [4]css.Border{}
		out.Padding = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	}
	if setW {
		out.Width = css.Pts(w * css.Pt)
	}
	if setH {
		out.Height = css.Pts(h * css.Pt)
	}
	return &out
}

// hasEdges reports a style whose border or padding takes room.
func hasEdges(st *css.Style) bool {
	for s := range 4 {
		if p := st.Padding[s]; p.Kind != css.Points || p.Value != 0 {
			return true
		}
		if b := st.Border[s]; b.Style != css.BorderNone && b.Style != css.BorderHidden && b.Width > 0 {
			return true
		}
	}
	return false
}

// The default sizes of a checkbox's and a radio button's box, and of a
// range's, in CSS pixels.
const (
	widgetBox   = 13
	rangeWidth  = 129
	rangeHeight = 16
)

// boxSizingEdges returns what a width (horizontal) or height of st adds to
// its content box: the padding and border of that axis under box-sizing:
// border-box, 0 under content-box. A percentage padding counts 0.
func boxSizingEdges(st *css.Style, horizontal bool) float64 {
	if st.BoxSizing != css.BorderBox {
		return 0
	}
	sides := [2]css.Side{css.Top, css.Bottom}
	if horizontal {
		sides = [2]css.Side{css.Left, css.Right}
	}
	sum := 0.0
	for _, s := range sides {
		if p := st.Padding[s]; p.Kind == css.Points {
			sum += max(p.Value, 0)
		}
		if b := st.Border[s]; b.Style != css.BorderNone && b.Style != css.BorderHidden {
			sum += max(b.Width, 0)
		}
	}
	return sum
}

// labelsWidth returns the advance of the widest of labels set in st, in
// points, its indent included; a bold label is set at weight 700.
func (e *engine) labelsWidth(labels []ControlLabel, st *css.Style, lang string) float64 {
	ts := e.textStyle(st, lang)
	bold := ts
	bold.Weight = max(bold.Weight, boldWeight)
	widest := 0.0
	for _, l := range labels {
		if l.Text == "" {
			widest = max(widest, l.Indent)
			continue
		}
		style := ts
		if l.Bold {
			style = bold
		}
		p, _, err := e.fonts.Shape(e.ctx, []text.ParagraphRun{{Text: l.Text, Style: style}}, text.ParagraphOptions{})
		if err != nil {
			e.shapeFailed(err, "a form control's option", -1)
			continue
		}
		if p.Len() > 0 {
			widest = max(widest, p.Line(0, p.Len(), false).Width+l.Indent)
		}
	}
	return widest
}

// boldWeight is the weight of bold text (CSS Fonts 4, 2.2).
const boldWeight = 700
