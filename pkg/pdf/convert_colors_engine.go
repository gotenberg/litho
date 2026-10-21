package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/recolor"
)

// recolorRequest is what [Writer.ConvertColors] asks the colour conversion
// engine for: the target space and the options that shape the conversion.
type recolorRequest struct {
	// family is the family the converted colours are written in:
	// DeviceGray, DeviceRGB, DeviceCMYK, or ICCBased over profile.
	family string
	// profile is the target's ICC profile, the destination of every
	// transform, and components its channel count.
	profile    []byte
	components int
	intent     icc.Intent
	// blackPointCompensation and convertSpots are the options of
	// [WithBlackPointCompensation] and [WithSpotConversion].
	blackPointCompensation bool
	convertSpots           bool
	// jpegQuality is the quality of [WithJPEGQuality], 0 for lossless.
	jpegQuality int
}

// recolorResult counts what the engine converted.
type recolorResult struct {
	operators, colorSpaces, images, shadings int
	// keptSpots names the colorants of the Separation and DeviceN spaces
	// the engine kept, their alternate space converted.
	keptSpots []string
}

// recolorEngine rewrites the colours of every page of doc, and of everything
// they draw, to req's target, queueing the edits on alloc. It returns an
// error naming the construct when it cannot convert a colour, and queues
// nothing it would have to take back: [Writer.ConvertColors] discards alloc
// on an error.
type recolorEngine func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, req recolorRequest) (recolorResult, error)

// errColorsLeft is what [recolorDocument] refuses with when the conversion
// leaves a colour, an image or a space unconverted.
var errColorsLeft = errors.New("colours left unconverted")

// recolorDocument is the built-in engine: package recolor's conversion of the
// content, resources and images of every page, through the ICC engine.
//
// The device spaces of the source mean what the renderer reads them as: the
// destination profile of the document's first output intent of their
// component count (ISO 32000-1, 14.11.5, as [Reader.RenderPage] reads them),
// and for DeviceCMYK, lacking one, the profile [WithDefaultCMYKProfile] gave,
// else litho's built-in profiles. The rendering intent is the request's,
// relative colorimetric unless [WithColorIntent] says otherwise, the intent
// the renderer draws the source with (8.6.5.8), so the converted document
// draws as the source drew within the target's gamut.
//
// A conversion that leaves anything unconverted, an image whose samples do
// not decode, a colour space it cannot read, is refused whole: a document
// half in the target is not what the caller asked for.
func recolorDocument(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page, req recolorRequest) (recolorResult, error) {
	target := recolor.Target{Family: req.family, Profile: req.profile}
	if req.family == graphics.FamilyICCBased {
		stream, err := iccProfileStream(req.profile, req.components)
		if err != nil {
			return recolorResult{}, err
		}
		target.ColorSpace = ast.PdfArray{ast.PdfName(graphics.FamilyICCBased), alloc.Alloc(stream)}
	}
	gray, rgb, cmyk := deviceProfiles(ctx, alloc, doc)
	report, err := recolor.Content(ctx, doc, alloc, pages, target, recolor.Options{
		Intent:                 req.intent,
		BlackPointCompensation: req.blackPointCompensation,
		ConvertSpots:           req.convertSpots,
		JPEGQuality:            req.jpegQuality,
		DeviceGray:             gray,
		DeviceRGB:              rgb,
		DeviceCMYK:             cmyk,
	})
	if err != nil {
		return recolorResult{}, err
	}
	if n := len(report.Skipped); n > 0 {
		first := report.Skipped[0]
		where := "a direct object"
		if first.Object != 0 {
			where = fmt.Sprintf("object %d", first.Object)
		}
		return recolorResult{}, fmt.Errorf("%w: %d, the first %s (%s): %w", errColorsLeft, n, first.What, where, first.Err)
	}
	return recolorResult{
		operators:   report.Operators,
		colorSpaces: report.Spaces,
		images:      report.Images.Converted + report.Images.Palettes,
		shadings:    report.Shadings,
		keptSpots:   report.KeptColorants,
	}, nil
}

// deviceProfiles returns the profiles the device spaces of doc mean: the
// destination profile of its first output intent of each component count,
// and for DeviceCMYK the caller's default CMYK profile lacking one. nil
// leaves a space to litho's built-in reading.
func deviceProfiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (gray, rgb, cmyk []byte) {
	if ref, ok := mutations.CatalogRef(ctx, doc); ok {
		if catalog, err := mutations.LiveCatalogDict(ctx, alloc, doc, ref); err == nil {
			// An output intent that does not resolve characterises nothing,
			// so its error leaves the device spaces to the next one.
			intents, _ := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
			for _, entry := range intents {
				d, err := document.ResolveDict(ctx, doc, entry)
				if err != nil || d == nil {
					continue
				}
				profile := decodedStream(ctx, alloc, doc, d["DestOutputProfile"])
				p, err := icc.Parse(profile)
				if err != nil {
					continue
				}
				switch p.Components() {
				case 1:
					gray = firstProfile(gray, profile)
				case 3:
					rgb = firstProfile(rgb, profile)
				case 4:
					cmyk = firstProfile(cmyk, profile)
				}
			}
		}
	}
	if cmyk == nil && doc.CMYKProfile != nil {
		cmyk = bytes.Clone(doc.CMYKProfile)
	}
	return gray, rgb, cmyk
}

// firstProfile returns first, or next when first is nil: the first profile found.
func firstProfile(first, next []byte) []byte {
	if first != nil {
		return first
	}
	return next
}

// iccProfileStream writes profile as an ICC profile stream of n components
// (ISO 32000-1, 8.6.5.5, Table 66), FlateDecode-compressed.
func iccProfileStream(profile []byte, n int) (ast.PdfStream, error) {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(profile); err != nil {
		return ast.PdfStream{}, fmt.Errorf("compressing the target profile: %w", err)
	}
	if err := w.Close(); err != nil {
		return ast.PdfStream{}, fmt.Errorf("compressing the target profile: %w", err)
	}
	return ast.NewStream(ast.PdfDict{
		"N":      ast.PdfInt(n),
		"Filter": ast.PdfName("FlateDecode"),
		"Length": ast.PdfInt(buf.Len()),
	}, buf.Bytes()), nil
}
