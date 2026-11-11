package docx

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// gradientTile is the height in points of the tile Word prints a page
// background's VML gradient in: the gradient runs from the fill colour to
// the second colour over half the tile and back over the other half, and
// the tile repeats down the page.
//
// # Corpus evidence
//
//   - litho/docx doc-background: Word prints the gradient from #FFF2CC to
//     #DEEAF6 over 96 points and back over the next 96, four times down a
//     Letter page.
const gradientTile = 192

// pageBackground returns the page background a w:background element sets
// (ECMA-376 Part 1, 17.2.1), painted on every page under the content, nil
// for none or a white one. Its colour is w:color, else its theme colour
// with tint and shade; a v:background gradient fill (Part 4, 14.1.2.5
// fill type gradient) paints from its fill colour to color2 in Word's
// tiles of [gradientTile]. A picture fill paints the colour alone.
//
// Word prints the background whatever w:displayBackgroundShape says, which
// governs the views only (17.15.1.31).
//
// # Corpus evidence
//
//   - openxmlsdk/docx PageColor-lightgreen, document_-_HexColor_-_RGB_-_Document_Background,
//     Moon_Tregillus: Word prints the w:color over the whole page, the last
//     without w:displayBackgroundShape.
//   - libreoffice/docx ooxmlexport/fdo74357: Word prints even a #FFFFF8
//     background; painted, layout rises from 0.02 to 0.96.
func (d *document) pageBackground(bg *ooxml.Element) *css.Style {
	if bg == nil {
		return nil
	}
	spec := d.parseColor(bg, "color")
	if spec.auto {
		return nil
	}
	white := css.Color{R: 1, G: 1, B: 1, A: 1}
	st := css.Initial()
	st.BackgroundColor = spec.c
	if vb := bg.Child(ooxml.NSV, "background"); vb != nil {
		fill := vb.Child(ooxml.NSV, "fill")
		if attrOr(fill, "type") == "gradient" || attrOr(fill, "type") == "gradientRadial" {
			c1, ok1 := ooxml.VMLColor(attrOr(vb, "fillcolor"))
			if !ok1 {
				c1 = spec.c
			}
			c2, ok2 := ooxml.VMLColor(attrOr(fill, "color2"))
			if ok2 {
				st.BackgroundGradient = &css.Gradient{Kind: css.LinearGradient, Angle: 180, Stops: []css.GradientStop{
					{Color: c1, Position: css.Dimension{Kind: css.Percent, Value: 0}},
					{Color: c2, Position: css.Dimension{Kind: css.Percent, Value: 0.5}},
					{Color: c1, Position: css.Dimension{Kind: css.Percent, Value: 1}},
				}}
				st.BackgroundSize = css.BackgroundSize{Width: css.Dimension{Kind: css.Percent, Value: 1}, Height: css.Pts(gradientTile)}
				st.BackgroundAttachment = css.AttachFixed
			}
		}
	}
	if st.BackgroundGradient == nil && st.BackgroundColor == white {
		return nil
	}
	return st
}
