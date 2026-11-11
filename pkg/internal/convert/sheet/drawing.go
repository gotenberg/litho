package sheet

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// chartPicture returns the picture that draws the chart of d into its
// box rc: chart.Render paints it with the builder's fonts.
func (b *builder) chartPicture(d *Drawing, rc compose.Rect) *layout.Picture {
	c, fonts := d.Chart, b.fonts
	return &layout.Picture{
		Width:  rc.Width,
		Height: rc.Height,
		Vector: func(ctx context.Context, r compose.Rect) []compose.Fragment {
			return chart.Render(ctx, fonts, c, r)
		},
	}
}

// drawings returns the fragments of the drawings of the page's regions:
// each picture over the cells its anchors place it on, clipped to the
// region, in the coordinates of the page's cells. It also returns the
// Figure element of each drawing the page prints, in the order the sheet
// lists them ([figureOf]).
func (b *pageBuilder) drawings(regs []*region) ([]compose.Fragment, []*compose.StructElem) {
	g := b.g
	var out []compose.Fragment
	var figs []*compose.StructElem
	for i := range g.sh.Drawings {
		d := &g.sh.Drawings[i]
		rc := g.drawingRect(d)
		if rc.Width <= 0 || rc.Height <= 0 {
			continue
		}
		pic := d.Picture
		if pic == nil && d.Chart != nil {
			pic = b.chartPicture(d, rc)
		}
		if pic == nil || (pic.Source == nil && pic.Vector == nil) {
			continue
		}
		fig := figureOf(d)
		drawn := false
		for _, rg := range regs {
			// The drawing's box in the region's coordinates.
			x := rg.x0 + rc.X - g.colX(rg.cols.first)
			y := rg.y0 + rc.Y - g.rowY(rg.rows.first)
			if x >= rg.x0+rg.w || y >= rg.y0+rg.h || x+rc.Width <= rg.x0 || y+rc.Height <= rg.y0 {
				continue
			}
			r := compose.Rect{X: x, Y: y, Width: rc.Width, Height: rc.Height}
			var content []compose.Fragment
			if pic.Vector != nil {
				content = pic.Vector(b.ctx, r)
			} else {
				content = []compose.Fragment{&compose.Image{Rect: r, Source: pic.Source}}
			}
			if fig != nil && adopt(content, fig) {
				drawn = true
			}
			out = append(out, &compose.Group{
				Transform: compose.Identity,
				Clip:      &compose.Path{Ops: rectOps(rg.x0, rg.y0, rg.w, rg.h), Fill: &compose.Fill{}},
				Opacity:   1,
				Content:   content,
			})
		}
		if drawn {
			figs = append(figs, fig)
		}
	}
	return out, figs
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
func figureOf(d *Drawing) *compose.StructElem {
	if d.Decorative || (d.Shape && d.Alt == "") {
		return nil
	}
	alt := d.Alt
	if alt == "" && d.Chart != nil && d.Chart.Title != nil {
		alt = strings.Join(strings.Fields(d.Chart.Title.Text), " ")
	}
	return &compose.StructElem{Role: "Figure", Alt: alt}
}

// adopt makes parent the structure element of the fragments frags paint,
// those inside groups included, and reports whether any paints.
func adopt(frags []compose.Fragment, parent *compose.StructElem) bool {
	painted := false
	for _, fr := range frags {
		switch v := fr.(type) {
		case *compose.Text:
			v.Parent, painted = parent, true
		case *compose.Image:
			v.Parent, painted = parent, true
		case *compose.Path:
			v.Parent, painted = parent, true
		case *compose.Group:
			if adopt(v.Content, parent) {
				painted = true
			}
		}
	}
	return painted
}
