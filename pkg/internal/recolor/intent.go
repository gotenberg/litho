package recolor

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// IntentReport says what the rendering intents the content states changed
// in a conversion. ISO 32000-1, 8.6.5.8 has the ri operator and an ExtGState
// /RI set the intent the colours painted after them convert under, and an
// image's /Intent override it for the image (8.9.5, Table 89): they override
// [Options.Intent], which the colours painted where the content states none
// convert under. A name 8.6.5.8 does not define selects relative
// colorimetric, as it does for the renderer.
//
// # Corpus evidence
//
//   - All corpora: 112 files state an intent other than relative
//     colorimetric (ri, /RI or an image's /Intent). Converted to the built-in
//     CMYK, 85 convert otherwise than before, and rendered at 36 dpi against
//     their sources, pages 1 to 3, the pixels more than 16 levels apart fall
//     from 395052 to 391921 (12 files nearer, 11 farther), and flattened
//     from 421201 to 417503; converted to sRGB, 58 convert otherwise, from
//     12568 to 6202 (pdfjs issue2856 and litho imageintent_2b, 3366 and 3000
//     to 0). The worst move is govdocs thread_400_400032, 104 pixels of a
//     saturated blue the perceptual table maps 20 levels from its source.
//   - litho, pdf20: the 225 other files that convert to the built-in CMYK
//     convert byte for byte as before.
type IntentReport struct {
	// Operators counts the colour operators converted under a graphics
	// state whose intent differs from the option's, those written again
	// after the ri or gs that set it among them.
	Operators int
	// InlineImages counts the inline images converted under such a state.
	InlineImages int
	// Copies counts the image XObjects and shadings painted under such a
	// state, written a second time converted with its intent and painted
	// through a resource name of their own, the first conversion serving
	// every painting under the option's. An image stating its own /Intent
	// converts under it everywhere and is never copied for a state's.
	Copies int
	// Unhonoured counts the paintings under such a state whose colour still
	// converts with the option's intent: a form XObject or a Type 3 glyph,
	// which inherits the state (ISO 32000-1, 8.10.1, 9.6.5) but whose content
	// converts once for every painting, and an image or a shading painted by
	// a stream without resources of its own, which a copy's name cannot be
	// added to.
	Unhonoured int
}

// intentName reads the /RI of the graphics state dictionary name of the
// stream's resources (ISO 32000-1, 8.4.5, Table 58), and false when it states
// none. Any name sets the intent, one 8.6.5.8 does not define selecting
// relative colorimetric, as the renderer's graphics state machine reads it.
func (cr *contentRewriter) intentName(ctx context.Context, name ast.PdfName) (icc.Intent, bool) {
	all, err := cr.s.resolveDict(ctx, cr.res["ExtGState"])
	if err != nil || all == nil {
		return 0, false
	}
	gs, err := cr.s.resolveDict(ctx, all[name])
	if err != nil || gs == nil {
		return 0, false
	}
	if _, ok := gs["RI"]; !ok {
		return 0, false
	}
	v, err := cr.s.resolveName(ctx, gs["RI"])
	if err != nil {
		return 0, false
	}
	return icc.ParseIntent(string(v)), true
}

// ownIntent reports whether an image dictionary states its own /Intent, which
// overrides the graphics state's (ISO 32000-1, 8.9.5, Table 89), as the image
// decoder reads it ([pdfimage.ImageIntent]).
func ownIntent(ctx context.Context, s *session, dict ast.PdfDict) bool {
	_, own := pdfimage.ImageIntent(ctx, s.doc, dict)
	return own
}
