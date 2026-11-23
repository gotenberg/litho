package sheet

import (
	"cmp"
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// chartPicture returns the picture that draws the chart of d into its
// box rc: chart.Render paints it with the builder's fonts.
func (b *builder) chartPicture(d *Drawing, rc typeset.Rect) *layout.Picture {
	c, fonts := d.Chart, b.fonts
	return &layout.Picture{
		Width:  rc.Width,
		Height: rc.Height,
		Vector: func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
			return chart.Render(ctx, fonts, c, r)
		},
	}
}

// drawings returns the fragments of the drawings of the page's regions:
// each picture over the cells its anchors place it on, clipped to the
// region, in the coordinates of the page's cells. It also returns the
// Figure element of each drawing the page prints, in the order the sheet
// lists them ([figureOf]), and a link over the part of each region a
// drawing that carries one covers, its Figure inside a Link element.
func (b *pageBuilder) drawings(regs []*region) ([]typeset.Fragment, []*typeset.StructElem, []typeset.Link) {
	g := b.g
	var out []typeset.Fragment
	var figs []*typeset.StructElem
	var links []typeset.Link
	for i := range g.sh.Drawings {
		d := &g.sh.Drawings[i]
		rc := g.drawingRect(d)
		// A line along a row or a column, a shape, has no height or no
		// width; a picture of either shows nothing.
		if rc.Width < 0 || rc.Height < 0 || rc.Width+rc.Height <= 0 || (!d.Shape && (rc.Width <= 0 || rc.Height <= 0)) {
			continue
		}
		pic := d.Picture
		if pic == nil && d.Chart != nil {
			pic = b.chartPicture(d, rc)
		}
		if pic == nil || (pic.Source == nil && pic.Vector == nil) {
			continue
		}
		fig := figureOf(d, b.opts.DerivedAltText, b.lang)
		var link *typeset.StructElem
		if d.Href != "" {
			link = &typeset.StructElem{Role: "Link"}
		}
		drawn, linked := false, false
		for _, rg := range regs {
			// The drawing's box in the region's coordinates.
			x := rg.x0 + rc.X - g.layoutColX(rg.cols.first)
			if rg.rtl {
				// The columns run from the right, and the drawing with
				// them, unturned ([grid.mirrored]).
				x = rg.x0 + rg.w - (rc.X + rc.Width - g.layoutColX(rg.cols.first))
			}
			y := rg.y0 + rc.Y - g.layoutRowY(rg.rows.first)
			if x >= rg.x0+rg.w || y >= rg.y0+rg.h || before(x+rc.Width, rg.x0, rc.Width) || before(y+rc.Height, rg.y0, rc.Height) {
				continue
			}
			r := typeset.Rect{X: x, Y: y, Width: rc.Width, Height: rc.Height}
			var content []typeset.Fragment
			switch {
			case d.Picture == nil && d.Chart != nil && g.doc.ExcelRules:
				// Excel writes on a page only the chart's text that
				// reaches the page ([chart.RenderPart]).
				content = chart.RenderPart(b.ctx, b.fonts, d.Chart, r, typeset.Rect{X: rg.x0, Y: rg.y0, Width: rg.w, Height: rg.h})
			case pic.Vector != nil:
				content = pic.Vector(b.ctx, r)
			default:
				content = []typeset.Fragment{&typeset.Image{Rect: r, Source: pic.Source}}
			}
			if fig != nil && adopt(content, fig) {
				drawn = true
			}
			if link != nil {
				x0, y0 := max(x, rg.x0), max(y, rg.y0)
				x1, y1 := min(x+rc.Width, rg.x0+rg.w), min(y+rc.Height, rg.y0+rg.h)
				if x1 > x0 && y1 > y0 {
					links = append(links, typeset.Link{Rect: typeset.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}, URI: d.Href, Parent: link, Contents: cmp.Or(d.Alt, d.Href)})
					linked = true
				}
			}
			out = append(out, &typeset.Group{
				Transform: typeset.Identity,
				Clip:      &typeset.Path{Ops: rectOps(rg.x0, rg.y0, rg.w, rg.h), Fill: &typeset.Fill{}},
				Opacity:   1,
				Content:   content,
			})
		}
		switch {
		case linked && drawn:
			link.Kids = []*typeset.StructElem{fig}
			figs = append(figs, link)
		case linked:
			figs = append(figs, link)
		case drawn:
			figs = append(figs, fig)
		}
	}
	return out, figs, links
}

// before reports a drawing side ending at end, size long, before a
// region starting at start: a side of no length, a line's, ends there
// within it.
func before(end, start, size float64) bool {
	return end < start || (size > 0 && end <= start)
}

// figureOf returns the Figure element a drawing d is read as, nil for
// one drawn as an artifact. A picture or a chart is a Figure (ISO
// 32000-1, 14.8.4.5, Table 340) carrying the description its source
// gives it, a chart without one its title, as ISO 14289-1, 7.3 asks of a
// Figure's alternate description; a picture its source does not describe
// stays a Figure without one, which only the workbook's author can give
// and a PDF/UA conversion reports. A drawing its source marks decorative,
// and one of shapes alone its source does not describe, is an artifact
// (ISO 32000-1, 14.8.2.2.2; ISO 14289-1, 7.1), as the undescribed shapes
// of a slide or a text document are.
//
// With derived set, a chart its source does not describe takes the
// description [chart.AltText] derives from its model, its title first,
// in English, which the Figure states when lang, the workbook's
// language, is another ([chart.AltTextLang]). A title alone names a
// chart and says nothing of what it shows; the derived sentence keeps it
// and adds the kind, the series, the categories and the axes. A picture
// is never described so: a workbook gives a picture no caption.
func figureOf(d *Drawing, derived bool, lang string) *typeset.StructElem {
	if d.Decorative || (d.Shape && d.Alt == "") {
		return nil
	}
	if derived && d.Alt == "" && d.Chart != nil && !d.Formula {
		return &typeset.StructElem{Role: "Figure", Alt: chart.AltText(d.Chart), Lang: chart.AltTextLang(lang)}
	}
	alt := d.Alt
	if alt == "" && d.Chart != nil && d.Chart.Title != nil {
		alt = strings.Join(strings.Fields(d.Chart.Title.Text), " ")
	}
	if d.Formula {
		return &typeset.StructElem{Role: "Formula", Alt: alt}
	}
	return &typeset.StructElem{Role: "Figure", Alt: alt}
}

// adopt makes parent the structure element of the fragments frags paint,
// those inside groups included, and reports whether any paints.
func adopt(frags []typeset.Fragment, parent *typeset.StructElem) bool {
	painted := false
	for _, fr := range frags {
		switch v := fr.(type) {
		case *typeset.Text:
			v.Parent, painted = parent, true
		case *typeset.Image:
			v.Parent, painted = parent, true
		case *typeset.Path:
			v.Parent, painted = parent, true
		case *typeset.Group:
			if adopt(v.Content, parent) {
				painted = true
			}
		}
	}
	return painted
}
