package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// fieldsetDoc returns a document of one fieldset box styled by fmods, of
// width 100px and a top border of top px, whose legend is styled by lmods
// and whose content box holds a 10px block.
func fieldsetDoc(top float64, fmods, lmods []func(*css.Style)) *Node {
	fs := sty(append([]func(*css.Style){display(css.FlowRoot), width(100), func(s *css.Style) {
		s.Border[css.Top] = css.Border{Width: px(top), Style: css.Solid, Color: black}
	}}, fmods...)...)
	lg := el(sty(append([]func(*css.Style){height(20)}, lmods...)...))
	lg.Fieldset = FieldsetLegend
	content := el(sty(display(css.FlowRoot)), el(sty(height(10))))
	content.Fieldset = FieldsetContent
	f := el(fs, lg, content)
	f.Fieldset = FieldsetBox
	return doc(f)
}

// TestFieldset_LegendCentredOnTheBorder locks the block-start placement
// of a rendered legend (HTML Standard, 15.3.13): its border box is
// centred on the block-start border, its block-start margin is not laid
// out, as Chromium lays it out, and the content starts below the lower of
// the border and the legend's margin box.
func TestFieldset_LegendCentredOnTheBorder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name          string
		top           float64
		lmods         []func(*css.Style)
		legend, start float64
	}{
		{"legend taller than the border", 4, nil, 0, 20},
		{"block-start margin", 4, []func(*css.Style){margin(20, 0, 0, 0)}, 0, 20},
		{"block-end margin", 4, []func(*css.Style){margin(0, 0, 20, 0)}, 0, 40},
		{"border taller than the legend", 60, nil, 20, 60},
		{"border and block-end margin", 30, []func(*css.Style){margin(0, 0, 30, 0)}, 5, 55},
	} {
		rb := boxesWith(t, fieldsetDoc(tc.top, nil, tc.lmods), Options{Page: page(300, 200)})
		f := kid(t, rb, 0, 0)
		lg, content := f.kids[0], f.kids[1]
		if got := (lg.y - f.y) / css.Pt; !near(got, tc.legend) {
			t.Errorf("%s: legend %vpx below the fieldset's top, want %v", tc.name, got, tc.legend)
		}
		if got := (content.y - f.y) / css.Pt; !near(got, tc.start) {
			t.Errorf("%s: content %vpx below the fieldset's top, want %v", tc.name, got, tc.start)
		}
		if got, want := f.borderH()/css.Pt, tc.start+10; !near(got, want) {
			t.Errorf("%s: fieldset %vpx tall, want %v", tc.name, got, want)
		}
	}
}

// TestFieldset_LegendPlacedInline locks the inline placement of a rendered
// legend: its fit-content width in the fieldset's content box, inside the
// fieldset's padding, placed by auto margins or else by text-align, as
// Chromium places it (HTML Standard, 15.3.13).
func TestFieldset_LegendPlacedInline(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		fmods []func(*css.Style)
		lmods []func(*css.Style)
		x     float64
	}{
		{"start", nil, nil, 0},
		{"padding", []func(*css.Style){padding(0, 9, 0, 9)}, []func(*css.Style){margin(0, 0, 0, 5)}, 14},
		{"centre", nil, []func(*css.Style){func(s *css.Style) { s.TextAlign = css.AlignCenter }}, 45},
		{"centre past the start margin", nil, []func(*css.Style){margin(0, 0, 0, 20), func(s *css.Style) { s.TextAlign = css.AlignCenter }}, 65},
		{"right", nil, []func(*css.Style){margin(0, 6, 0, 0), func(s *css.Style) { s.TextAlign = css.AlignRight }}, 84},
		{"right to left", []func(*css.Style){func(s *css.Style) { s.Direction = css.RTL }}, []func(*css.Style){func(s *css.Style) { s.Direction = css.RTL }}, 90},
		{"auto margins", nil, []func(*css.Style){margin(0, -1, 0, -1), func(s *css.Style) {
			s.Margin[css.Left], s.Margin[css.Right] = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
			s.TextAlign = css.AlignRight
		}}, 45},
	} {
		d := fieldsetDoc(4, tc.fmods, tc.lmods)
		lg := d.Children[0].Children[0].Children[0]
		lg.Children = []*Node{txt(lg.Style, "X")}
		rb := boxesWith(t, d, Options{Page: page(300, 200)})
		f := kid(t, rb, 0, 0)
		got := f.kids[0]
		if w := got.borderW() / css.Pt; !near(w, 10) {
			t.Errorf("%s: legend %vpx wide, want its fit-content 10px", tc.name, w)
		}
		if x := (got.x - f.x - f.bd[css.Left]) / css.Pt; !near(x, tc.x) {
			t.Errorf("%s: legend %vpx from the fieldset's padding edge, want %v", tc.name, x, tc.x)
		}
	}
}

// TestFieldset_ContentBoxTakesThePadding locks the anonymous fieldset
// content box: it takes the fieldset's padding as its own, its padding box
// covers the fieldset's below the legend, and it fills a fieldset whose
// height a max-height bounds (HTML Standard, 15.3.13).
func TestFieldset_ContentBoxTakesThePadding(t *testing.T) {
	t.Parallel()
	d := fieldsetDoc(4, []func(*css.Style){padding(5, 7, 5, 7), func(s *css.Style) { s.MaxHeight = css.Pts(px(30)) }}, nil)
	d.Children[0].Children[0].Children[1].Children[0].Style.Height = css.Pts(px(80))
	rb := boxesWith(t, d, Options{Page: page(300, 200)})
	f := kid(t, rb, 0, 0)
	c := f.kids[1]
	if got, want := rect(c), [4]float64{rect(f)[0], rect(f)[1] + 20, 114, 30 + 10 - 16}; got != want {
		t.Errorf("content box %v px, want %v: the fieldset's padding box below the legend", got, want)
	}
	if got := (c.contentX() - f.contentX()) / css.Pt; !near(got, 0) {
		t.Errorf("content %vpx from the fieldset's content edge, want 0", got)
	}
}

// TestFieldset_BorderNotDrawnBehindTheLegend locks the painting of a
// fieldset with a rendered legend (HTML Standard, 15.3.13): no border
// behind the legend's border box, the border and the background drawn
// from where the border is centred on the legend.
func TestFieldset_BorderNotDrawnBehindTheLegend(t *testing.T) {
	t.Parallel()
	fs := sty(display(css.FlowRoot), width(6), border(1, css.Solid, black), bg(lime), padding(0, 0, 0, 0))
	lg := el(sty(width(2), height(3), margin(0, 0, 0, 1)))
	lg.Fieldset = FieldsetLegend
	content := el(sty(display(css.FlowRoot)), el(sty(height(1))))
	content.Fieldset = FieldsetContent
	f := el(fs, lg, content)
	f.Fieldset = FieldsetBox
	assertPixels(t, run(t, doc(f), page(10, 6)), 1, `
		__________
		KKGGKKKK__
		KGGGGGGK__
		KGGGGGGK__
		KKKKKKKK__
		__________
	`)
}

// TestFieldset_ContentBoxHoldsPositionedBoxes locks that the content box
// of a fieldset that holds positioned descendants holds them in its stead,
// below the legend, as Chromium lays them out.
func TestFieldset_ContentBoxHoldsPositionedBoxes(t *testing.T) {
	t.Parallel()
	d := fieldsetDoc(4, []func(*css.Style){func(s *css.Style) { s.Position = css.Relative }}, nil)
	abs := el(sty(width(5), height(5), func(s *css.Style) {
		s.Position = css.Absolute
		s.Top = css.Pts(0)
	}))
	content := d.Children[0].Children[0].Children[1]
	content.Children = append(content.Children, abs)
	rb := boxesWith(t, d, Options{Page: page(300, 200)})
	f := kid(t, rb, 0, 0)
	a := f.kids[1].kids[1]
	if got := (a.y - f.y) / css.Pt; !near(got, 20) {
		t.Errorf("positioned box %vpx below the fieldset's top, want 20: the content box's top", got)
	}
}

// TestFieldset_InlineFlexTakesItsContentBaseline locks that an inline
// fieldset whose content box is a flex container takes that container's
// first baseline, as an inline flex container does.
func TestFieldset_InlineFlexTakesItsContentBaseline(t *testing.T) {
	t.Parallel()
	d := fieldsetDoc(4, []func(*css.Style){display(css.InlineBlock)}, []func(*css.Style){display(css.DisplayNone)})
	f := d.Children[0].Children[0]
	f.Children = f.Children[1:]
	content := f.Children[0]
	content.Style.Display = css.Flex
	content.Style.FlexDirection = css.FlexColumn
	content.Children = []*Node{el(sty(), txt(sty(), "X")), el(sty(), txt(sty(), "X"))}
	d.Children[0].Children = []*Node{el(sty(), txt(inlineSty(), "Y"), f)}
	rb := boxesWith(t, d, Options{Page: page(300, 200)})
	fb := kid(t, rb, 0, 0).kids[1]
	if fieldsetPart(fb) != FieldsetBox || !fb.hasBaseline {
		t.Fatalf("box %+v, want the fieldset with a baseline", fb.node)
	}
	if got := fb.baseline / css.Pt; !near(got, 4+8) {
		t.Errorf("baseline %vpx below the fieldset's top, want 12: the first line's", got)
	}
}
