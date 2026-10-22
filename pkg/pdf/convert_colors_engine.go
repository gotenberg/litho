package pdf

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"slices"

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
	// sourceProfiles and intentFallbacks are [ColorReport.SourceProfiles]
	// and [ColorReport.IntentFallbacks].
	sourceProfiles  map[string][]string
	intentFallbacks []IntentFallback
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
	gray, rgb, cmyk, cmykFromCaller := deviceProfiles(ctx, alloc, doc)
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

		sourceProfiles:  sourceProfiles(report.DeviceProfiles, cmykFromCaller),
		intentFallbacks: intentFallbacks(report.IntentFallbacks, cmykFromCaller),
	}, nil
}

// deviceFamilies maps the data colour space signatures of package recolor to
// the device family names a [ColorReport] uses.
var deviceFamilies = map[string]string{ //nolint:gochecknoglobals // static lookup table, ICC.1:2010 Table 19 to ISO 32000-1 8.6.4
	recolor.SpaceGray: graphics.FamilyDeviceGray,
	recolor.SpaceRGB:  graphics.FamilyDeviceRGB,
	recolor.SpaceCMYK: graphics.FamilyDeviceCMYK,
}

// profileSource names where a profile package recolor read came from, as a
// [ColorReport] spells it: a profile the options gave a device space is the
// output intent's, except DeviceCMYK's when cmykFromCaller.
func profileSource(space, provenance string, cmykFromCaller bool) string {
	switch provenance {
	case recolor.ProfileGiven:
		if space == recolor.SpaceCMYK && cmykFromCaller {
			return ProfileSourceCaller
		}
		return ProfileSourceOutputIntent
	case recolor.ProfileTarget:
		return ProfileSourceTarget
	case recolor.ProfileBuiltin:
		return ProfileSourceBuiltin
	case recolor.ProfileBuiltinFallback:
		return ProfileSourceBuiltinFallback
	case recolor.ProfileDefaultSpace:
		return ProfileSourceDefaultSpace
	}
	return ProfileSourceDocument
}

// sourceProfiles translates package recolor's DeviceProfiles.
func sourceProfiles(in map[string][]string, cmykFromCaller bool) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for space, provenances := range in {
		family, ok := deviceFamilies[space]
		if !ok {
			continue
		}
		var sources []string
		for _, p := range provenances {
			sources = append(sources, profileSource(space, p, cmykFromCaller))
		}
		slices.Sort(sources)
		out[family] = slices.Compact(sources)
	}
	return out
}

// intentFallbacks translates package recolor's IntentFallbacks.
func intentFallbacks(in []recolor.IntentFallback, cmykFromCaller bool) []IntentFallback {
	if len(in) == 0 {
		return nil
	}
	out := make([]IntentFallback, 0, len(in))
	for _, f := range in {
		fb := IntentFallback{Target: f.Target, Table: f.Table.String()}
		switch f.Space {
		case recolor.SpaceGray:
			fb.Components = 1
		case recolor.SpaceCMYK:
			fb.Components = 4
		default:
			fb.Components = 3
		}
		// The target's own provenance is [ColorReport.TargetProfile], which
		// [Writer.ConvertColors] fills in.
		if !f.Target {
			fb.Profile = profileSource(f.Space, f.Profile, cmykFromCaller)
		}
		out = append(out, fb)
	}
	return out
}

// deviceProfiles returns the profiles the device spaces of doc mean: the
// destination profile of its first output intent of each component count,
// and for DeviceCMYK the caller's default CMYK profile lacking one. nil
// leaves a space to litho's built-in reading. cmykFromCaller reports a
// DeviceCMYK profile that is the caller's rather than an output intent's.
func deviceProfiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (gray, rgb, cmyk []byte, cmykFromCaller bool) {
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
		cmyk, cmykFromCaller = bytes.Clone(doc.CMYKProfile), true
	}
	return gray, rgb, cmyk, cmykFromCaller
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
