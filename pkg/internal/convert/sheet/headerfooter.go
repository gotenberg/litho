package sheet

import (
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// The header and footer text of a span naming no font: Calc's header and
// footer edit engine default, Liberation Sans 10 points, whatever the
// book's default cell font.
//
// # Corpus evidence
//
//   - litho: a flat ODS with every header field, converted by Gotenberg
//     9.0.0, draws the header and footer in Liberation Sans 10 points; a
//     part is centred in the header's height, the date shows as
//     11/07/2026 and the time as 21:39:35, the file name as the
//     document's URL and the title as the document's title.
//   - ods: sc/tdf138507.ods, a book whose default cell font is "Sans",
//     prints its default header and footer in Liberation Sans.
const (
	headerFamily = "Liberation Sans"
	headerSize   = 10
)

// hfFields are the values of a page's header and footer fields.
type hfFields struct {
	page, pages int
	sheet       string
}

// spanText returns what a span shows on a page.
func (b *builder) spanText(s Span, f hfFields) string {
	switch s.Field {
	case FieldPage:
		return strconv.Itoa(f.page)
	case FieldPages:
		return strconv.Itoa(f.pages)
	case FieldSheet:
		return f.sheet
	case FieldTitle:
		return b.doc.Meta.Title
	case FieldDate:
		if b.opts.PrintDate.IsZero() {
			return ""
		}
		return b.opts.PrintDate.Format("01/02/2006")
	case FieldTime:
		if b.opts.PrintDate.IsZero() {
			return ""
		}
		return b.opts.PrintDate.Format("15:04:05")
	case FieldFile, FieldPath:
		b.warnOnce(CodeField, "the file name and path fields of headers and footers show nothing: the conversion has no file name")
		return ""
	case FieldNone:
	}
	return s.Text
}

// partNode returns the block of one header or footer part, nil when it
// shows nothing.
func (b *builder) partNode(spans []Span, f hfFields, align css.TextAlign) *layout.Node {
	if len(spans) == 0 {
		return nil
	}
	base := font{families: []string{headerFamily}, size: headerSize, color: css.Color{A: 1}}
	st := b.style()
	base.apply(st)
	st.TextAlign = align
	st.WhiteSpace = css.PreWrap
	st.OverflowWrap = css.BreakWord
	st.LineHeight = css.Dimension{Kind: css.Number, Value: b.m.lineHeight(base) / headerSize}
	n := &layout.Node{Kind: layout.Element, Style: st}
	empty := true
	for _, s := range spans {
		t := b.spanText(s, f)
		if t == "" {
			continue
		}
		empty = false
		rs := b.style()
		fnt := base
		if s.Font != nil {
			fnt = resolveFont(*s.Font, Font{Families: []string{headerFamily}, Size: headerSize})
		}
		fnt.apply(rs)
		rs.Display = css.Inline
		rs.WhiteSpace = css.PreWrap
		rs.LineHeight = st.LineHeight
		n.Children = append(n.Children, &layout.Node{
			Kind: layout.Element, Style: rs,
			Children: []*layout.Node{{Kind: layout.Text, Style: rs, Text: t}},
		})
	}
	if empty {
		return nil
	}
	return n
}

// margins returns the header and footer of page p, numbered number of
// total, in page coordinates: each part laid out across the width between
// the margins, the left part aligned left, the centre centred and the
// right right, centred down the header's height, as Calc prints them
// (ScPrintFunc::PrintHF).
func (b *builder) margins(p *pagePlan, first bool, number, total int, width, height float64) []compose.Fragment {
	hf := &p.sp.g.sh.HeaderFooter
	m := hf.Odd
	switch {
	case hf.DifferentFirst && first:
		m = hf.First
	case hf.DifferentOddEven && number%2 == 0:
		m = hf.Even
	}
	f := hfFields{page: number, pages: total, sheet: p.sp.g.sh.Name}
	ps := &p.sp.g.sh.Page
	// Calc prints the header and footer at the sheet's print scale
	// (ScPrintFunc::PrintHF draws in the zoomed twip map mode): they are
	// laid out on the page divided by the zoom and drawn multiplied by
	// it, their distances from the edges kept.
	z := p.sp.zoom
	if !(z > 0) {
		z = 1
	}
	pageW, pageH := width, height
	width, height = width/z, height/z
	left, right := p.sp.margin[css.Left]/z, p.sp.margin[css.Right]/z
	areaW := width - left - right
	if areaW <= 1 {
		return nil
	}
	root := b.style()
	root.Position = css.Relative
	root.Width = css.Pts(width)
	root.Height = css.Pts(height)
	tree := &layout.Node{Kind: layout.Element, Style: root}
	place := func(parts [3][]Span, top float64, fromBottom bool, minH float64) {
		aligns := [3]css.TextAlign{css.AlignLeft, css.AlignCenter, css.AlignRight}
		var nodes [3]*layout.Node
		any := false
		for i := range parts {
			nodes[i] = b.partNode(parts[i], f, aligns[i])
			any = any || nodes[i] != nil
		}
		if !any {
			return
		}
		h := 0.0
		heights := [3]float64{}
		for i, n := range nodes {
			if n != nil {
				heights[i] = b.measureBlock(n, areaW)
				h = max(h, heights[i])
			}
		}
		h = max(h, minH)
		y := top
		if fromBottom {
			y = height - top - h
		}
		for i, n := range nodes {
			if n == nil {
				continue
			}
			box := b.style()
			box.Position = css.Absolute
			box.Left = css.Pts(left)
			box.Width = css.Pts(areaW)
			box.Top = css.Pts(y + max(h-heights[i], 0)/2)
			tree.Children = append(tree.Children, &layout.Node{Kind: layout.Element, Style: box, Children: []*layout.Node{n}})
		}
	}
	place(m.Header, max(ps.HeaderTop, 0)/z, false, max(hf.HeaderMinHeight, 0))
	place(m.Footer, max(ps.FooterBottom, 0)/z, true, max(hf.FooterMinHeight, 0))
	if len(tree.Children) == 0 {
		return nil
	}
	res, err := layout.Layout(b.ctx, tree, b.fonts, layout.Options{
		Page:     layout.PageBox{Width: width, Height: height},
		Lang:     b.lang,
		MaxPages: maxSubPages,
	})
	if err != nil || len(res.Pages) == 0 {
		b.warn(CodeHeader, fmt.Sprintf("a header or footer of sheet %s could not be laid out", quote(p.sp.g.sh.Name)))
		return nil
	}
	frags := artifacts(res.Pages[0].Content)
	if z == 1 {
		return frags
	}
	return []compose.Fragment{&compose.Group{
		Transform: compose.Matrix{A: z, D: z},
		Clip:      &compose.Path{Ops: rectOps(0, 0, pageW/z, pageH/z), Fill: &compose.Fill{}},
		Opacity:   1,
		Content:   frags,
	}}
}

// headerShown reports a sheet whose pages show a header, footer when
// footer is set.
func headerShown(hf *HeaderFooter, footer bool) bool {
	for _, m := range []*Margins{&hf.Odd, &hf.Even, &hf.First} {
		parts := m.Header
		if footer {
			parts = m.Footer
		}
		for _, p := range parts {
			if len(p) > 0 {
				return true
			}
		}
	}
	return false
}

// measureBlock returns the height of the block n laid out w points wide.
func (b *builder) measureBlock(n *layout.Node, w float64) float64 {
	n.ID = "\x00hf"
	defer func() { n.ID = "" }()
	st := b.style()
	st.Width = css.Pts(w)
	res, err := layout.Layout(b.ctx, &layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{n}}, b.fonts, layout.Options{
		Page:     layout.PageBox{Width: w, Height: measureHeight},
		Lang:     b.lang,
		MaxPages: maxSubPages,
	})
	if err != nil {
		return 0
	}
	return res.Extents[n.ID].Height
}

// measureHeight is the height of the page a header part is measured on,
// in points: taller than any header.
const measureHeight = 14400

// artifacts returns frags with no structure element: headers and footers
// are pagination artifacts (ISO 32000-1, 14.8.2.2.2).
func artifacts(frags []compose.Fragment) []compose.Fragment {
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Text:
			v.Parent = nil
		case *compose.Image:
			v.Parent = nil
		case *compose.Path:
			v.Parent = nil
		case *compose.Group:
			artifacts(v.Content)
		}
	}
	return frags
}

// growMargins makes room for a header or footer whose height follows its
// content (Calc's ScPrintFunc::UpdateHFHeight): the tallest part of every
// variant laid out across the page between the margins, plus the
// spacing, moves the cells down from the header, or up from the footer,
// when it needs more room than the margin leaves.
func (b *builder) growMargins(sp *sheetPlan) {
	hf := &sp.g.sh.HeaderFooter
	ps := &sp.g.sh.Page
	areaW := sp.width - sp.margin[css.Left] - sp.margin[css.Right]
	if areaW <= 1 {
		return
	}
	f := hfFields{page: 1, pages: 1, sheet: sp.g.sh.Name}
	variants := []Margins{hf.Odd}
	if hf.DifferentOddEven {
		variants = append(variants, hf.Even)
	}
	if hf.DifferentFirst {
		variants = append(variants, hf.First)
	}
	tallest := func(footer bool) float64 {
		h := 0.0
		aligns := [3]css.TextAlign{css.AlignLeft, css.AlignCenter, css.AlignRight}
		for _, m := range variants {
			parts := m.Header
			if footer {
				parts = m.Footer
			}
			for i := range parts {
				if n := b.partNode(parts[i], f, aligns[i]); n != nil {
					h = max(h, b.measureBlock(n, areaW))
				}
			}
		}
		return h
	}
	if hf.GrowHeader && headerShown(hf, false) && ps.HeaderTop >= 0 {
		if need := tallest(false) + max(hf.HeaderSpacing, 0); need > sp.margin[css.Top]-ps.HeaderTop {
			sp.margin[css.Top] = min(ps.HeaderTop+need, maxSide)
		}
	}
	if hf.GrowFooter && headerShown(hf, true) && ps.FooterBottom >= 0 {
		if need := tallest(true) + max(hf.FooterSpacing, 0); need > sp.margin[css.Bottom]-ps.FooterBottom {
			sp.margin[css.Bottom] = min(ps.FooterBottom+need, maxSide)
		}
	}
}
