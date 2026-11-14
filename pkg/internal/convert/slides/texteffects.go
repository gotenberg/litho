package slides

import "github.com/gotenberg/litho/pkg/internal/css"

// textEffects returns the effects of a run of style rs for the layout to
// draw under its glyphs, by PowerPoint's rules (css.Office.TextEffects):
// bitmaps with no structure parent, artifacts (ISO 14289-1, 7.1), as
// PowerPoint marks them, the glyphs staying text. It returns nil for a
// run without effects and on Impress's rules, which draw no text effect.
//
// # Corpus evidence
//
//   - litho/pptx: text-fill-effects prints in PowerPoint its runs with an
//     outer shadow, a glow and a reflection as bitmaps marked as
//     artifacts under the text, which stays text.
func (b *builder) textEffects(rs *RunStyle) *css.OfficeEffects {
	if rs.Effects == nil || b.doc.Effects != EffectsPowerPoint {
		return nil
	}
	fx := *rs.Effects
	fx.Rules = css.OfficePowerPoint
	return &fx
}

// textPaint returns how a run of style rs paints its glyphs otherwise
// than filled in its colour (css.Office.TextPaint), by PowerPoint's rules:
// the glyphs stay text under the outlines that show them. It returns nil
// for a run that sets none and on Impress's rules.
//
// # Corpus evidence
//
//   - litho/pptx: text-fill-effects prints in PowerPoint its runs of a
//     gradient fill, of no fill with a line, and of a fill with a line as
//     the glyphs' outlines filled with a shading or stroked, over text
//     painted invisible; litho drew the first in one colour and the second
//     not at all.
//   - openxmlsdk/pptx: presentation/Text_withEffects_100chars prints in
//     PowerPoint its title, filled near white, inside its orange line;
//     litho drew it white on white under its glow.
func (b *builder) textPaint(rs *RunStyle) *css.OfficeTextPaint {
	if rs.Paint == nil || b.doc.Effects != EffectsPowerPoint {
		return nil
	}
	return rs.Paint
}
