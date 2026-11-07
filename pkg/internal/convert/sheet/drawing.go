package sheet

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// chartPicture returns the picture that draws the chart of d, nil when
// charts are not drawn: the drawing's box is kept empty and a warning
// names it.
func (b *builder) chartPicture(d *Drawing) *layout.Picture {
	b.warn(CodeChart, fmt.Sprintf("chart %s not drawn", quote(d.Name)))
	return nil
}

// drawings returns the fragments of the drawings of the page's regions:
// each picture over the cells its anchors place it on, clipped to the
// region, in the coordinates of the page's cells.
func (b *pageBuilder) drawings(regs []*region) []compose.Fragment {
	g := b.g
	var out []compose.Fragment
	for i := range g.sh.Drawings {
		d := &g.sh.Drawings[i]
		rc := g.drawingRect(d)
		if rc.Width <= 0 || rc.Height <= 0 {
			continue
		}
		pic := d.Picture
		if pic == nil && d.Chart != nil {
			pic = b.chartPicture(d)
		}
		if pic == nil || (pic.Source == nil && pic.Vector == nil) {
			continue
		}
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
			out = append(out, &compose.Group{
				Transform: compose.Identity,
				Clip:      &compose.Path{Ops: rectOps(rg.x0, rg.y0, rg.w, rg.h), Fill: &compose.Fill{}},
				Opacity:   1,
				Content:   content,
			})
		}
	}
	return out
}
