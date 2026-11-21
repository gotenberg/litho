package slides

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// fitSlack absorbs the rounding of laid out widths and heights, in points.
const fitSlack = 1e-6

// TextFits reports whether the text of s, a shape of a SmartArt diagram
// being sized, fits the text rectangle of its geometry less its insets at
// the sizes its runs state, laid out as [Convert] draws a shape's text with
// no scaling: its widest word no wider than the rectangle, so that no word
// breaks across lines (its widest line, for text that does not wrap), and
// its lines no taller. A shape without text fits.
// fonts is the presentation's font set; the call draws nothing and keeps
// nothing of s.
func TextFits(ctx context.Context, fonts *text.Fonts, s *Shape) bool {
	body := s.Text
	if body == nil || len(body.Paragraphs) == 0 {
		return true
	}
	b := &builder{ctx: ctx, mem: limits.BudgetFrom(ctx), doc: &Document{}, fonts: fonts, symbols: office.NewSymbolCodes(), warned: map[string]bool{}, metrics: map[string]text.Metrics{}}
	inner := insetRect(shape.TextRect(s.Geometry, s.Box), body.Insets)
	w, h := inner.Width, inner.Height
	if body.Vertical.Vertical() {
		w, h = h, w
	}
	if w <= 0 || h <= 0 {
		return false
	}
	if b.contentWidth(body, noFit, body.Wrap) > w+fitSlack {
		return false
	}
	// A character bullet wider than its hanging indent pushes the first
	// line's text past it (see [builder.bullet]): the widest word of that
	// line has that much less room.
	for i := range body.Paragraphs {
		if i >= maxParagraphs {
			break
		}
		p := &body.Paragraphs[i]
		over := b.bulletOverhang(p)
		if over <= 0 {
			continue
		}
		one := *body
		one.Paragraphs = body.Paragraphs[i : i+1]
		if b.contentWidth(&one, noFit, body.Wrap)+over > w+fitSlack {
			return false
		}
	}
	lay := b.layoutBody(body, w, h, noFit)
	return lay != nil && lay.height <= h+fitSlack
}

// bulletOverhang returns how far a start-aligned paragraph's character
// bullet reaches past its hanging indent, in points at no scaling: the
// distance the first line's text moves right of the margin; 0 for none.
func (b *builder) bulletOverhang(p *Paragraph) float64 {
	bu := p.Bullet
	if bu == nil || bu.Kind != BulletChar || !(p.Indent < 0) || len(p.Runs) == 0 {
		return 0
	}
	if a := p.Align; a != css.AlignAuto && a != css.AlignStart && a != css.AlignLeft && a != css.AlignJustify {
		return 0
	}
	first := p.Runs[0].Style
	rel := bu.Scale
	if !numeric.Positive(rel) || rel > maxBulletScale {
		rel = 1
	}
	size := min(max(first.Size, 0), maxFontSize)
	if size == 0 {
		size = defaultFontSize
	}
	size *= rel
	if bu.Points > 0 && bu.Points <= maxFontSize {
		size = bu.Points
	}
	size = max(size, 1)
	rs := first
	rs.Size = size
	fams := rs.Families
	if len(bu.Families) > 0 {
		fams = bu.Families
	}
	label, _, recoded := symbolfont.Recode(bu.Char, fams)
	if label == "" {
		return 0
	}
	rs.Families = recoded
	bw := b.width(label, b.textStyle(&rs, 1))
	if !b.doc.ImpressBullets {
		if adv, ok := officeAdvance(bu.Char, fams); ok {
			bw = adv * size
		}
	}
	return max(bw+p.Indent, 0)
}
