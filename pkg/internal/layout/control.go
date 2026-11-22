package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
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
	// headings, the widest of which sizes it.
	Labels []ControlLabel
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
	// dropDownBlock what it adds to its line's height.
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
	var w, h float64
	switch c.Kind {
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
		w = math.Ceil(e.labelsWidth(c.Labels, st, lang)/css.Pt + dropDownArrow)
		h = lh + dropDownBlock*css.Pt
	case ControlListBox:
		w = math.Round(e.labelsWidth(c.Labels, st, lang)/css.Pt + listBoxInline)
		row := max(lh, float64(listBoxRowMin*size)) + listBoxRowEnd*css.Pt
		h = float64(row * float64(max(c.Rows, 1)))
	}
	out := *st
	if setW {
		out.Width = css.Pts(float64(w*css.Pt) + boxSizingEdges(st, true))
	}
	if setH {
		out.Height = css.Pts(h + boxSizingEdges(st, false))
	}
	if clip && out.Overflow == css.OverflowVisible {
		out.Overflow = css.OverflowHidden
	}
	return &out
}

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
