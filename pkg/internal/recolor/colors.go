package recolor

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// sessionColors is the [Colors] a session converts its images through: the
// session's own plans, so an image and a colour operator in one space convert
// alike, and a kept spot colour is written once for both.
//
// It reads no page dictionary: the page's own /OutputIntents (ISO 32000-2,
// 14.11.5) do not change what a device space means here, as they do not for
// the colour operators, so a page carrying them converts through the
// document's reading of its device spaces ([Options.DeviceCMYK] and its
// siblings).
//
// TODO: [recolour] read a page's /OutputIntents for its device spaces, in the
// content pass and here alike, as the renderer does; no corpus file states
// them.
type sessionColors struct {
	s *session
}

// Converter returns the session's conversion of the colour space space, read
// against resources, whose default colour spaces stand in for a device space
// (ISO 32000-1, 8.6.5.6), under the rendering intent intent, an image's own
// /Intent, which overrides the graphics state's (8.6.5.8), or "" for the
// session's current one. A space that already is the target converts as the
// identity.
func (c sessionColors) Converter(ctx context.Context, space ast.PdfObject, resources, _ ast.PdfDict, intent ast.PdfName) (Converter, error) {
	var p *spacePlan
	plan := func() { p = c.s.planDefinition(ctx, space, resources) }
	if intent == "" {
		plan()
	} else {
		c.s.under(variant{bpc: c.s.bpc, intent: icc.ParseIntent(string(intent))}, plan)
	}
	switch p.kind {
	case planConvert:
		return p.conv, nil
	case planIdentity:
		if !p.fixed && p.n == c.s.t.n {
			return identity{}, nil
		}
	case planUnknown:
		return nil, p.err
	case planKeep, planPattern:
	}
	return nil, fmt.Errorf("%w: a space whose colour values stay has no conversion to the target", ErrUnsupportedSpace)
}

// variantKey spells the session's current black point compensation and
// rendering intent, which an image's converted colour table depends on
// ([Images.convertIndexed]).
func (c sessionColors) variantKey() string {
	return c.s.variantKey()
}

// paintingIntent returns what the image decoder converts a JPEG 2000 image's
// own colour with ([Images.convertJPXColour]) under the session's current
// variant, and false under the option's, which decodes as before: under the
// image's own /Intent, else relative colorimetric, the graphics state's
// initial intent (ISO 32000-1, 8.6.5.8), with the decoder's own
// compensation. Only what a graphics state changes reaches the decoder: its
// intent, set by ri or an ExtGState /RI, and its /UseBlackPtComp /ON or /OFF
// (ISO 32000-2, Table 57), each where it differs from the option's.
//
// # Corpus evidence
//
//   - All corpora: 16 files hold a JPXDecode image XObject without
//     /ColorSpace (litho 3, pdfjs 7, veraPDF 6), and 4 more name JPXDecode in
//     a content stream (facturx 3, litho inlinejpx_2b). Converted to the
//     built-in CMYK and to sRGB, and the CMYK output flattened, all 20 convert
//     byte for byte as before. Only pdfjs issue15220.pdf paints such an image
//     under another intent, /Perceptual ri, and its profile is a matrix one
//     whose intents agree.
//   - litho jpxintent_2b.pdf: the two images painted under /Perceptual ri
//     and an /RI drew L* 50 converted, where the source draws L* 13, and now
//     draw L* 13.
func (c sessionColors) paintingIntent() (icc.Intent, bool) {
	bpc, intent := c.s.differs(variant{bpc: c.s.bpc, intent: c.s.intent})
	if !bpc && !intent {
		return 0, false
	}
	decodeIntent, choice := icc.RelativeColorimetric, icc.BPCDefault
	if intent {
		decodeIntent = c.s.intent
	}
	if bpc {
		choice = map[bool]icc.BPC{false: icc.BPCOff, true: icc.BPCOn}[c.s.bpc]
	}
	return decodeIntent.WithBPC(choice), true
}

// SpotSpace returns the Separation or DeviceN space it is given as the
// session keeps it: its colorants and tints as they are, its alternate the
// target and its tint transform a sampled function into it (ISO 32000-1,
// 8.6.6.4 and 8.6.6.5). A space written as a reference is rewritten at its number, once,
// and comes back as the reference; a direct one comes back as the rewritten
// array the plan holds, the same array each time. A spot colour whose
// alternate already is the target comes back as it is.
func (c sessionColors) SpotSpace(ctx context.Context, space ast.PdfObject, _, _ ast.PdfDict) (ast.PdfObject, error) {
	// A kept spot colour is one space for every painting naming it,
	// written under the option's black point compensation and intent
	// ([session.rewriteDefinition]).
	var p *spacePlan
	c.s.under(c.s.optionVariant(), func() { p = c.s.planDefinition(ctx, space, nil) })
	switch {
	case p.kind == planUnknown:
		return nil, p.err
	case p.kind == planIdentity && p.spot:
		return space, nil
	case p.kind == planKeep && p.spot && p.def != nil:
		if _, isRef := space.(ast.PdfIndirectRef); isRef {
			if _, _, err := c.s.rewriteDefinition(ctx, space); err != nil {
				return nil, err
			}
			return space, nil
		}
		return p.def, nil
	}
	return nil, fmt.Errorf("%w: no spot colour the conversion keeps", ErrUnsupportedSpace)
}
