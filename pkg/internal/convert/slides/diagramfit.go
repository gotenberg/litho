package slides

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/shape"
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
	lay := b.layoutBody(body, w, h, noFit)
	return lay != nil && lay.height <= h+fitSlack
}
