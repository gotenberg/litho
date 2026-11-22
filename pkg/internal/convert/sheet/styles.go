package sheet

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// maxSharedStyles caps the styles each table of [cellStyles] keeps. Memory
// guard, not spec-mandated: a page holds a few dozen distinct cell boxes,
// one for each column width, row height and text width it prints, and a
// style past the cap is made for its cell alone, as one equal to it.
const maxSharedStyles = 1024

// cellStyles holds the styles the cells of one page share. A sheet has
// many cells and few distinct boxes: the cells of a column share a clip,
// numbers of one width share a text box, the cells of a format share a
// paragraph. Each style is made once from its key, before any is
// allocated, so a cell costs a map lookup instead of a [css.Style]; the
// layout reads a shared style and never writes it, and equal styles lay
// out alike (see [css.Styles]), so sharing changes no output.
type cellStyles struct {
	clip  map[clipKey]*css.Style
	inner map[innerKey]*css.Style
	para  map[paraKey]*css.Style
}

// clipKey is what a cell's clipping box depends on: its left edge in its
// region and its size, in points.
type clipKey struct {
	left, width, height float64
}

// innerKey is what a cell's text box depends on, unrotated: its place in
// the clip, its size, its top and bottom padding and how it aligns its
// text vertically.
type innerKey struct {
	left, width, height float64
	top, bottom         float64
	vertical            VAlign
	// shift moves the box down from where it aligns its text, under
	// Excel's rules ([pageBuilder.excelShift]).
	shift float64
}

// paraTurn says what a cell's paragraph turns from its alignment: a
// rotated text sets its line left-aligned, a right-to-left one sets its
// direction.
type paraTurn struct {
	rotated, rtl bool
}

// paraKey is what a cell's paragraph style depends on: its font, its line
// height, its alignment, whether it wraps and its turn.
type paraKey struct {
	// families is the first family of the font's list and n its length:
	// two lists of one backing array and length are one list. Lists equal
	// in content but apart in memory make two keys, and two equal styles.
	families *string
	n        int
	size     float64
	bold     bool
	italic   bool
	under    Underline
	strike   bool
	color    css.Color
	script   Script
	lineH    float64
	align    css.TextAlign
	wrap     bool
	turn     paraTurn
}

// shared returns the style m holds for k, made by mk and kept when m has
// room. *m is made on first use.
func shared[K comparable](m *map[K]*css.Style, k K, mk func() *css.Style) *css.Style {
	if st, ok := (*m)[k]; ok {
		return st
	}
	st := mk()
	if *m == nil {
		*m = map[K]*css.Style{}
	}
	if len(*m) < maxSharedStyles {
		(*m)[k] = st
	}
	return st
}

// clipStyle returns the style of a cell's clipping box.
func (b *pageBuilder) clipStyle(k clipKey) *css.Style {
	return shared(&b.styles.clip, k, func() *css.Style {
		st := b.style()
		st.Position = css.Absolute
		st.Left = css.Pts(k.left)
		st.Top = css.Pts(0)
		st.Width = css.Pts(k.width)
		st.Height = css.Pts(k.height)
		st.Overflow = css.OverflowHidden
		return st
	})
}

// innerStyle returns the style of an unrotated cell's text box.
func (b *pageBuilder) innerStyle(k innerKey) *css.Style {
	return shared(&b.styles.inner, k, func() *css.Style { return b.newInnerStyle(k) })
}

// newInnerStyle returns a new style of a cell's text box, for its caller
// to write.
func (b *pageBuilder) newInnerStyle(k innerKey) *css.Style {
	st := b.style()
	st.Position = css.Absolute
	st.Left = css.Pts(k.left)
	st.Top = css.Pts(k.shift)
	st.Width = css.Pts(k.width)
	st.Height = css.Pts(k.height)
	st.BoxSizing = css.BorderBox
	st.Padding[css.Top] = css.Pts(k.top)
	st.Padding[css.Bottom] = css.Pts(k.bottom)
	st.Display = css.Flex
	st.FlexDirection = css.FlexColumn
	switch k.vertical {
	case VTop, VJustify, VDistributed:
		st.JustifyContent = css.BoxFlexStart
	case VMiddle:
		st.JustifyContent = css.BoxCenter
	case VBottom:
		st.JustifyContent = css.BoxFlexEnd
	}
	return st
}

// paraStyle returns the style of a cell's paragraph in the font f, its
// lines lineH points apart, aligned by align, wrapping or not, turned as
// t says.
func (b *pageBuilder) paraStyle(f font, lineH float64, align css.TextAlign, wrap bool, t paraTurn) *css.Style {
	k := paraKey{
		n: len(f.families), size: f.size, bold: f.bold, italic: f.italic, under: f.under, strike: f.strike,
		color: f.color, script: f.script, lineH: lineH, align: align, wrap: wrap, turn: t,
	}
	if len(f.families) > 0 {
		k.families = &f.families[0]
	}
	return shared(&b.styles.para, k, func() *css.Style {
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
		if wrap {
			st.WhiteSpace = css.PreWrap
			st.OverflowWrap = css.BreakWord
			if !b.doc.ExcelRules {
				st.LineHeight.Value = editLine(lineH) / math.Max(f.size, 0.01)
			}
		}
		if t.rotated {
			st.TextAlign = css.AlignLeft
		}
		if t.rtl {
			st.Direction = css.RTL
		}
		return st
	})
}
