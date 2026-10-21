package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// colorTargetKind tells the targets of [ColorTarget] apart.
type colorTargetKind uint8

const (
	targetOutputIntent colorTargetKind = iota
	targetCMYK
	targetRGB
	targetGray
	targetICC
)

// ColorTarget names the colour space [Writer.ConvertColors] converts a
// document to. Build one with [OutputIntentColorTarget], [CMYKColorTarget],
// [RGBColorTarget], [GrayColorTarget] or [ICCColorTarget]; the zero value is
// [OutputIntentColorTarget].
// ColorTarget values are immutable and safe for concurrent use.
type ColorTarget struct {
	kind    colorTargetKind
	profile []byte
}

// OutputIntentColorTarget converts to the document's own output intent: the
// destination profile of its first GTS_PDFX intent, or else of its first
// intent naming one (ISO 32000-1, 14.11.5). Colours are written in the device
// family of the profile's colour space, which the intent characterises, and
// the output intents stay as they are. A document with no such intent makes
// [Writer.ConvertColors] fail with [ErrNoOutputIntent].
func OutputIntentColorTarget() ColorTarget {
	return ColorTarget{kind: targetOutputIntent}
}

// CMYKColorTarget converts to DeviceCMYK, read in the profile given to
// [WithDefaultCMYKProfile] when the Reader was opened with one, and else in
// litho's built-in printer profile of the CGATS TR 001 (SWOP) printing
// condition, the profile litho reads DeviceCMYK in by default.
func CMYKColorTarget() ColorTarget {
	return ColorTarget{kind: targetCMYK}
}

// RGBColorTarget converts to DeviceRGB, read as sRGB (IEC 61966-2-1).
func RGBColorTarget() ColorTarget {
	return ColorTarget{kind: targetRGB}
}

// GrayColorTarget converts to DeviceGray, read as litho's grey profile: the
// sRGB tone curve and white.
func GrayColorTarget() ColorTarget {
	return ColorTarget{kind: targetGray}
}

// ICCColorTarget converts to an ICCBased colour space over profile (ISO
// 32000-1, 8.6.5.5), which holds the bytes of an ICC profile of a grey, RGB
// or CMYK data colour space, and is copied. [Writer.ConvertColors] fails with
// [ErrInvalidColorTarget] when litho cannot convert colours to it.
func ICCColorTarget(profile []byte) ColorTarget {
	return ColorTarget{kind: targetICC, profile: bytes.Clone(profile)}
}

// RenderingIntent is the rendering intent of a colour conversion (ISO
// 32000-1, 8.6.5.8, Table 70; ICC.1:2010, 6.2). The zero value is
// [IntentRelativeColorimetric], the intent 8.6.5.8 makes the default.
// RenderingIntent values are immutable and safe for concurrent use.
type RenderingIntent uint8

const (
	// IntentRelativeColorimetric maps the source white to the target's and
	// keeps every colour the target can reproduce, clipping the rest.
	IntentRelativeColorimetric RenderingIntent = iota
	// IntentPerceptual compresses the source gamut into the target's,
	// keeping the relations between colours over their exact values.
	IntentPerceptual
	// IntentSaturation keeps colours vivid over their exact values.
	IntentSaturation
	// IntentAbsoluteColorimetric keeps colours exact, the source white
	// included, simulating the source medium on the target.
	IntentAbsoluteColorimetric
)

// iccIntent returns the ICC rendering intent i names, false for a value no
// constant names.
func (i RenderingIntent) iccIntent() (icc.Intent, bool) {
	switch i {
	case IntentRelativeColorimetric:
		return icc.RelativeColorimetric, true
	case IntentPerceptual:
		return icc.Perceptual, true
	case IntentSaturation:
		return icc.Saturation, true
	case IntentAbsoluteColorimetric:
		return icc.AbsoluteColorimetric, true
	}
	return 0, false
}

// ColorOption configures [Writer.ConvertColors].
// ColorOption closures must be safe to call from any goroutine.
type ColorOption func(*colorOpts)

// colorOpts holds what the [ColorOption] values set.
type colorOpts struct {
	intent       RenderingIntent
	bpc          bool
	convertSpots bool
	keepIntents  bool
	// replaceIntents is [WithReplacedOutputIntents].
	replaceIntents bool
	// jpegQuality is [WithJPEGQuality]'s, and jpegQualitySet whether it was
	// given at all, so a 0 it was given is refused rather than read as the
	// lossless default.
	jpegQuality    int
	jpegQualitySet bool
}

// WithColorIntent converts every colour with the rendering intent i. Default
// [IntentRelativeColorimetric]. A value no constant names makes
// [Writer.ConvertColors] fail with [ErrInvalidColorTarget].
func WithColorIntent(i RenderingIntent) ColorOption {
	return func(o *colorOpts) { o.intent = i }
}

// WithBlackPointCompensation scales the source's black point to the
// target's, so the darkest source colour maps to the darkest the target can
// reproduce rather than clipping the shadows (ISO 18619). Off by default.
func WithBlackPointCompensation() ColorOption {
	return func(o *colorOpts) { o.bpc = true }
}

// WithSpotConversion converts spot colours, the Separation and DeviceN colour
// spaces (ISO 32000-1, 8.6.6.4 and 8.6.6.5), to the target through their tint
// transforms, so the output prints on process inks alone. By default a spot
// colour is kept, its colorants named as they were, and only its alternate
// space is converted, which is what a device without the colorant paints.
func WithSpotConversion() ColorOption {
	return func(o *colorOpts) { o.convertSpots = true }
}

// WithoutOutputIntent leaves the document's output intents as they are. By
// default [Writer.ConvertColors] makes a target whose profile is an output or
// display device profile the document's output intent.
func WithoutOutputIntent() ColorOption {
	return func(o *colorOpts) { o.keepIntents = true }
}

// WithReplacedOutputIntents points every output intent of another subtype
// than GTS_PDFA1 whose destination profile is another profile of the
// target's component count, a GTS_PDFX intent among them, at the target's
// profile, keeping its /S and /OutputConditionIdentifier, so DeviceCMYK means
// the target everywhere. Without it, a CMYK target on a document keeping such
// an intent fails with [ErrOutputIntentConflict]. It applies under
// [WithoutOutputIntent] too, which then leaves only the PDF/A intents alone.
// [OutputIntentColorTarget] converts to the document's own intent and has
// nothing to replace.
func WithReplacedOutputIntents() ColorOption {
	return func(o *colorOpts) { o.replaceIntents = true }
}

// WithJPEGQuality re-encodes each converted image whose source filter is
// DCTDecode as a baseline JPEG (ITU-T T.81) at quality q, 1 to 100, on the
// scale of the Independent JPEG Group's libjpeg, which most encoders share.
// By default every converted image is re-encoded losslessly under
// FlateDecode, which keeps the converted samples exactly but makes a JPEG
// source several times larger: its samples were lossy already, and Flate
// compresses them poorly. A JPEG 2000 source stays lossless, and an image's
// soft mask and mask are kept as the lossless re-encoding keeps them. An
// image the lossless encoding holds in fewer bytes stays lossless. A q
// outside 1 to 100 makes [Writer.ConvertColors] fail with
// [ErrInvalidColorTarget].
func WithJPEGQuality(q int) ColorOption {
	return func(o *colorOpts) { o.jpegQuality, o.jpegQualitySet = q, true }
}

// ColorReport describes what [Writer.ConvertColors] converted.
// Values must not be shared across goroutines without synchronization.
type ColorReport struct {
	// Target is the family the converted colours are written in:
	// "DeviceGray", "DeviceRGB", "DeviceCMYK" or "ICCBased".
	Target string
	// ConvertedOperators counts the colour operators rewritten, and
	// ConvertedColorSpaces the colour space resources.
	ConvertedOperators   int
	ConvertedColorSpaces int
	// ConvertedImages counts the image XObjects and inline images
	// re-encoded in the target, and ConvertedShadings the shadings.
	ConvertedImages   int
	ConvertedShadings int
	// KeptSpotColorants names, sorted, the colorants of the spot colours
	// kept with their alternate space converted. Empty under
	// [WithSpotConversion].
	KeptSpotColorants []string
	// OutputIntent is "added" when the conversion added a GTS_PDFA1 output
	// intent of the target's profile, "replaced" when it gave the existing
	// GTS_PDFA1 intents that profile, and "" when it left the intents alone.
	OutputIntent string
	// KeptOutputIntents lists the /S of each output intent of another
	// subtype the conversion left as it was, GTS_PDFX among them.
	KeptOutputIntents []string
	// ReplacedOutputIntents lists the /OutputConditionIdentifier of each
	// intent [WithReplacedOutputIntents] pointed at the target profile.
	ReplacedOutputIntents []string
}

// ConvertColors converts every colour the document paints to target: the
// colour operators of every page, form XObject, pattern, Type 3 glyph and
// annotation appearance, their colour space resources, images and inline
// images, and shadings. The conversion goes through ICC profiles: the
// source's (an ICCBased space's own, the Default colour space of ISO 32000-1,
// 8.6.5.6 the resources give a device family, or else litho's reading of the
// device families) to the target's, with the rendering intent
// [WithColorIntent] selects. It is lossy by nature: a colour outside the
// target's gamut moves to one inside it, and a conversion back does not
// restore it.
//
// Spot colours, Separation and DeviceN, are kept by default: their colorants
// stay named, so a device that has the ink still prints it, and their
// alternate space is converted to the target. [WithSpotConversion] converts
// them to process colour. A target whose profile is an output or display
// device profile becomes the document's GTS_PDFA1 output intent: the existing
// GTS_PDFA1 intents take its profile, or one is added. An intent of any other
// subtype, a PDF/X intent among them, is kept as it was and named in
// [ColorReport.KeptOutputIntents]: only the producer can restate the printing
// condition a document was prepared for. [WithReplacedOutputIntents] points
// those over another profile of the target's component count at the target's
// profile, keeping their subtype and printing condition, where a CMYK target
// otherwise refuses them. [WithoutOutputIntent] leaves the GTS_PDFA1 intents
// alone. The [OutputIntentColorTarget] changes no intent.
//
// It runs eagerly rather than queueing, because the caller must learn whether
// the document converts before writing it, and it forces a full rewrite.
// Three ways it refuses, each leaving the [Writer] exactly as it was:
// [ErrInvalidColorTarget] or [ErrNoOutputIntent] when the target names no
// profile litho converts to, [ErrOutputIntentConflict] when a CMYK target
// meets a kept output intent over another CMYK profile, through which viewers
// would read the converted DeviceCMYK ([WithReplacedOutputIntents] points such
// intents at the target), and [ErrColorConversionRefused] when a colour the
// document paints cannot be converted: converting part of a document leaves
// colours in two spaces where the caller asked for one.
//
// It converts the document as the Writer holds it when it runs: every edit
// queued before it, a [Writer.Redact] included, is converted with the rest.
// Content an edit adds after it, a stamp, inserted pages, or the content
// [Writer.AutoTag] rebuilds from the source, is not converted, so call it
// after them. Like every edit, it comes before a remediation, which is the
// last edit on a Writer (after one it returns [ErrEditAfterRemediation]); a
// PDF/A remediation after it then reads the converted colours through the
// output intent it set. A signature the source carries is dropped from the
// rewrite, as for every full rewrite (see [Writer.DroppedSignatures]).
func (w *Writer) ConvertColors(ctx context.Context, target ColorTarget, opts ...ColorOption) (_ *ColorReport, err error) {
	defer recoverToError(w.logger(), "Writer.ConvertColors", &err)

	if w.closed {
		return nil, ErrWriterClosed
	}
	if w.consumed {
		return nil, ErrWriterConsumed
	}
	if err := w.refuseAfterRemediation("ConvertColors"); err != nil {
		return nil, err
	}
	var o colorOpts
	for _, opt := range opts {
		opt(&o)
	}
	intent, ok := o.intent.iccIntent()
	if !ok {
		return nil, fmt.Errorf("%w: rendering intent %d", ErrInvalidColorTarget, o.intent)
	}
	if o.jpegQualitySet && (o.jpegQuality < 1 || o.jpegQuality > 100) {
		return nil, fmt.Errorf("%w: JPEG quality %d, outside 1 to 100", ErrInvalidColorTarget, o.jpegQuality)
	}

	if err := w.promotePageTree(ctx); err != nil {
		return nil, err
	}
	doc := w.reader.doc
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Build on what earlier stages produced, as [Writer.Redact] does, so the
	// conversion reads the document as the caller staged it.
	seedAllocator(alloc, w.rewriteOverrides)
	for i, m := range w.unfoldedMutations() {
		if applyErr := m.apply(ctx, alloc, doc); applyErr != nil {
			return nil, fmt.Errorf("applying staged mutation %d before colour conversion: %w", w.folded+i, applyErr)
		}
	}
	staged := alloc.Writes()
	preTrailer := maps.Clone(doc.Index.Trailer)
	restoreTrailer := func() { doc.Index.Trailer = preTrailer }

	resolved, err := resolveColorTarget(ctx, alloc, doc, target)
	if err != nil {
		restoreTrailer()
		return nil, err
	}
	// ISO 32000-1, 14.11.5: an intent's destination profile is what the
	// device colours of a document mean, and viewers read DeviceCMYK through
	// the first intent of four components. A kept intent over another CMYK
	// profile would make the converted colours print as another device's.
	var conflicts []mutations.OutputIntentConflict
	if resolved.components == 4 && target.kind != targetOutputIntent {
		conflicts = mutations.ConflictingOutputIntents(ctx, alloc, doc, resolved.profile, resolved.components)
		if len(conflicts) > 0 && !o.replaceIntents {
			restoreTrailer()
			return nil, fmt.Errorf("%w: the %s intent %q; WithReplacedOutputIntents points it at the target profile",
				ErrOutputIntentConflict, conflicts[0].Subtype, conflicts[0].Condition)
		}
	}

	engine := w.recolor
	if engine == nil {
		engine = recolorDocument
	}
	result, err := engine(ctx, alloc, doc, pages, recolorRequest{
		family:                 resolved.family,
		profile:                resolved.profile,
		components:             resolved.components,
		intent:                 intent,
		blackPointCompensation: o.bpc,
		convertSpots:           o.convertSpots,
		jpegQuality:            o.jpegQuality,
	})
	if err != nil {
		restoreTrailer()
		if ctxErr := ctx.Err(); ctxErr != nil && errors.Is(err, ctxErr) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: %w", ErrColorConversionRefused, err)
	}

	report := &ColorReport{
		Target:               resolved.family,
		ConvertedOperators:   result.operators,
		ConvertedColorSpaces: result.colorSpaces,
		ConvertedImages:      result.images,
		ConvertedShadings:    result.shadings,
	}
	if !o.convertSpots {
		report.KeptSpotColorants = slices.Compact(slices.Sorted(slices.Values(result.keptSpots)))
	}
	spec := mutations.OutputIntentSpec{
		Profile:    resolved.profile,
		Components: resolved.components,
		Condition:  resolved.condition,
		Registry:   resolved.registry,
		Info:       resolved.info,
	}
	setIntent := !o.keepIntents && resolved.setsIntent
	if setIntent || len(conflicts) > 0 {
		// One profile object for every intent the conversion writes, as ISO
		// 19005-2, 6.2.3 asks of the intents of a PDF/A file.
		spec.ProfileRef = alloc.Alloc(mutations.ProfileStream(spec))
	}
	if setIntent {
		intentResult, intentErr := mutations.SetOutputIntent(ctx, alloc, doc, spec)
		if intentErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("%w: %w", ErrColorConversionRefused, intentErr)
		}
		report.OutputIntent, report.KeptOutputIntents = intentResult.Action, intentResult.Kept
	}
	if len(conflicts) > 0 {
		replaced, replaceErr := mutations.RetargetOutputIntents(ctx, alloc, doc, spec.Profile, spec.Components, spec.ProfileRef)
		if replaceErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("%w: %w", ErrColorConversionRefused, replaceErr)
		}
		for _, c := range replaced {
			report.ReplacedOutputIntents = append(report.ReplacedOutputIntents, c.Condition)
			if i := slices.Index(report.KeptOutputIntents, c.Subtype); i >= 0 {
				report.KeptOutputIntents = slices.Delete(report.KeptOutputIntents, i, i+1)
			}
		}
	}

	// Nothing converted and no intent set: leave the Writer untouched rather
	// than force a rewrite of a file the conversion did not change.
	if alloc.Writes() == staged {
		restoreTrailer()
		return report, nil
	}
	w.rewriteOverrides = alloc.Pending()
	w.folded = len(w.mutations)
	w.forceRewrite = true
	return report, nil
}

// resolvedColorTarget is a [ColorTarget] read against the document.
type resolvedColorTarget struct {
	family     string
	profile    []byte
	components int
	// setsIntent reports whether the target becomes the document's output
	// intent, and condition, registry and info are the entries it names its
	// printing condition with (ISO 32000-1, 14.11.5, Table 365).
	setsIntent                bool
	condition, registry, info string
}

// The printing condition litho's built-in CMYK profile characterises, as
// pkg/internal/remediation names it for the CMYK output intents it adds:
// the CGATS TR 001 entry of the ICC characterization data registry. ISO
// 32000-1, 14.11.5, Table 365 reads /OutputConditionIdentifier as a name from
// the registry /RegistryName names.
const (
	builtinCMYKCondition = "CGATS TR 001"
	iccRegistry          = "http://www.color.org"
)

// customCondition is the /OutputConditionIdentifier of a printing condition
// no registry names, which the profile alone characterises, as
// pkg/internal/remediation writes it.
const customCondition = "Custom"

// resolveColorTarget reads target against doc: the family the converted
// colours are written in, the profile they are converted to, and whether and
// how it becomes the output intent.
func resolveColorTarget(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, target ColorTarget) (resolvedColorTarget, error) {
	var t resolvedColorTarget
	switch target.kind {
	case targetOutputIntent:
		profile, err := outputIntentProfile(ctx, alloc, doc)
		if err != nil {
			return t, err
		}
		t.profile = profile
	case targetCMYK:
		t.profile, t.condition, t.registry = icc.CMYKPrinterProfile(), builtinCMYKCondition, iccRegistry
		if doc.CMYKProfile != nil {
			t.profile, t.condition, t.registry = bytes.Clone(doc.CMYKProfile), customCondition, ""
		}
	case targetRGB:
		t.profile, t.condition = icc.SRGBProfile(), "sRGB IEC61966-2.1"
	case targetGray:
		t.profile, t.condition = icc.SGrayProfile(), customCondition
	case targetICC:
		t.profile, t.condition = target.profile, customCondition
	default:
		return t, fmt.Errorf("%w: unknown target", ErrInvalidColorTarget)
	}

	p, err := icc.Parse(t.profile)
	if err != nil {
		return t, fmt.Errorf("%w: %w", ErrInvalidColorTarget, err)
	}
	t.components = p.Components()
	switch t.components {
	case 1:
		t.family = graphics.FamilyDeviceGray
	case 3:
		t.family = graphics.FamilyDeviceRGB
	case 4:
		t.family = graphics.FamilyDeviceCMYK
	default:
		return t, fmt.Errorf("%w: profile of the %q colour space", ErrInvalidColorTarget, p.Space)
	}
	if target.kind == targetICC {
		t.family = graphics.FamilyICCBased
	}
	// ISO 32000-1, 14.11.5 makes /DestOutputProfile the profile of an output
	// device; ISO 19005-2, 6.2.3 admits the output (prtr) and display (mntr)
	// device classes of ICC.1:2010, 7.2.5, Table 18. The output intent target
	// already is the document's intent.
	t.setsIntent = target.kind != targetOutputIntent && deviceProfileClass(t.profile)
	return t, nil
}

// deviceProfileClass reports whether profile's device class (ICC.1:2010,
// 7.2.5) is an output or a display device's.
func deviceProfileClass(profile []byte) bool {
	if len(profile) < 16 {
		return false
	}
	class := string(profile[12:16])
	return class == "prtr" || class == "mntr"
}

// outputIntentProfile returns the decoded destination profile of the
// document's output intent: its first GTS_PDFX intent's, the one PDF/X
// prints through (ISO 15930), or else that of the first intent naming one.
func outputIntentProfile(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) ([]byte, error) {
	catalogRef, ok := mutations.CatalogRef(ctx, doc)
	if !ok {
		return nil, ErrNoOutputIntent
	}
	catalog, err := mutations.LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNoOutputIntent, err)
	}
	intents, err := document.ResolveArray(ctx, doc, catalog["OutputIntents"])
	if err != nil || len(intents) == 0 {
		return nil, ErrNoOutputIntent
	}
	var first []byte
	for _, entry := range intents {
		d, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || d == nil {
			continue
		}
		profile := decodedStream(ctx, alloc, doc, d["DestOutputProfile"])
		if profile == nil {
			continue
		}
		// An /S that resolves to no name names no PDF/X intent, so its error
		// reads as the empty name.
		if s, _ := document.ResolveName(ctx, doc, d["S"]); s == "GTS_PDFX" {
			return profile, nil
		}
		if first == nil {
			first = profile
		}
	}
	if first == nil {
		return nil, ErrNoOutputIntent
	}
	return first, nil
}

// decodedStream returns the decoded data of the stream obj names, preferring
// a version an earlier edit queued on alloc, or nil.
func decodedStream(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) []byte {
	var stream ast.PdfStream
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if pending, isStream := alloc.Pending()[ref.ObjectNum].(ast.PdfStream); isStream {
			stream = pending
		}
	}
	if stream.Dict == nil {
		s, err := document.ResolveStream(ctx, doc, obj)
		if err != nil || s.Dict == nil {
			return nil
		}
		stream = s
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// An intent whose profile does not decode cannot be converted to,
		// and the next intent is tried, so the error is only logged.
		doc.Logger.Debug("output intent profile does not decode", slog.Any("err", err))
		return nil
	}
	return data
}
