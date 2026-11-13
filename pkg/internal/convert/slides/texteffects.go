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
