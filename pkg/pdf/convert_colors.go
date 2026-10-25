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
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/semantics"
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
	intent RenderingIntent
	// noBPC converts without black point compensation
	// ([WithoutBlackPointCompensation]).
	noBPC        bool
	convertSpots bool
	kOnlyBlack   bool
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

// WithoutBlackPointCompensation converts without black point compensation.
// By default [Writer.ConvertColors] compensates (ISO 18619): it scales the
// source's black point to the target's, so the darkest source colour maps to
// the darkest the target reproduces rather than clipping the shadows, as the
// renderer reads a profile whose black is lighter than sRGB's, and as poppler
// and PDFium draw it. Without compensation such a profile's black converts
// to the grey it measures, and the converted page draws lighter than its
// source.
//
// It is the choice a graphics state leaves to the conversion: a PDF 2.0
// /UseBlackPtComp of /ON or /OFF (ISO 32000-2, Table 57) overrides it for the
// colours, images and shadings painted under that state, and /Default, its
// initial value, follows it ([ColorReport.BlackPointOperators]).
//
// # Corpus evidence
//
//   - All corpora (litho, pdf20, pdfcpu, pdfjs, govdocs; 2540 converted to
//     the built-in CMYK): compensating by default changes the bytes of 54
//     outputs (litho 3, pdfcpu 2, pdfjs 34, govdocs 15), every one
//     deterministic, and no flattening of an output starts or stops
//     failing. The recolor cell's checks give every one of its 401 files
//     the verdict they gave without compensation.
//   - All corpora: of the 111 pages those 54 outputs render at 72 dpi, 20
//     draw closer to their source, 89 as close, and 2 further: pdfcpu
//     pkg_testdata__testImage.pdf page 1, 1.1% of its pixels past 8 levels
//     to 2.5% under poppler, and pdfjs issue2856.pdf page 1, 29.5% to 29.7%,
//     where two compensated steps, through the CMYK target to the screen,
//     read the darks otherwise than the one step a viewer takes.
//   - litho usebpc_4.pdf: its /Default page, 100 levels from its source at
//     worst without compensation, draws 46 apart, as its /ON page does;
//     poppler reads it 94 apart without and 36 with.
func WithoutBlackPointCompensation() ColorOption {
	return func(o *colorOpts) { o.noBPC = true }
}

// WithBlackPointCompensation asks for the black point compensation
// [Writer.ConvertColors] applies by default, and changes nothing. It is kept
// so callers written when compensation was opt-in still compile.
//
// Deprecated: compensation is the default; [WithoutBlackPointCompensation]
// turns it off.
func WithBlackPointCompensation() ColorOption {
	return func(*colorOpts) {}
}

// WithSpotConversion converts spot colours, the Separation and DeviceN colour
// spaces (ISO 32000-1, 8.6.6.4 and 8.6.6.5), to the target through their tint
// transforms, so the output prints on process inks alone. By default a spot
// colour is kept, its colorants named as they were, and only its alternate
// space is converted, which is what a device without the colorant paints.
func WithSpotConversion() ColorOption {
	return func(o *colorOpts) { o.convertSpots = true }
}

// WithKOnlyBlack prints grey and black solid colours on the black ink
// alone under a CMYK target: a colour set in DeviceGray converts to k = 1 -
// gray with no cyan, magenta or yellow, the conversion ISO 32000-1, 10.3.3
// defines between the two device spaces, and black set in DeviceRGB, 0 0 0,
// to 100% K. By default they convert through their profiles like every other
// colour, and black becomes a four-colour black of about 0.4 0.4 0.8 1 under
// the built-in profile, which print practice avoids for text and line art:
// four inks misregister at a glyph's edges and add to the total ink. It
// applies to the colours colour operators set, in text, paths and stencil
// masks alike, and to the annotation and default appearance colours
// [Writer.ConvertColors] converts. Images, shadings, Indexed palettes, spot
// alternates, a Default colour space's colours (8.6.5.6), ICCBased colours and
// RGB greys still convert through their profiles. 100% K reads lighter than
// a four-colour black, so the converted page draws a dark grey on screen
// where the source drew black. Under overprint, a converted colour keeps
// what it painted: the conversion writes /OPM 0 where overprint mode 1 would
// leave the cyan, magenta and yellow under a K-only colour unpainted
// ([ColorReport.OverprintPreserved]). It has no effect under a target of
// another component count. Off by default.
func WithKOnlyBlack() ColorOption {
	return func(o *colorOpts) { o.kOnlyBlack = true }
}

// WithoutOutputIntent leaves the document's output intents as they are. By
// default [Writer.ConvertColors] makes a target whose profile is an output or
// display device profile the document's output intent. A grey or CMYK
// conversion then names the target as the blending space of each page with no
// group, unless the kept intent already characterises a device of its family
// or the document claims PDF/A-1 (see [Writer.ConvertColors]).
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
// DCTDecode or JPXDecode as a baseline JPEG (ITU-T T.81) at quality q, 1 to
// 100, on the scale of the Independent JPEG Group's libjpeg, which most
// encoders share. By default every converted image is re-encoded losslessly
// under FlateDecode, which keeps the converted samples exactly but makes a
// JPEG or JPEG 2000 source several times larger: Flate compresses a codec's
// samples poorly. A JPEG 2000 source with a component deeper than 8 bits
// stays lossless, and an image's soft mask and mask are kept as the lossless
// re-encoding keeps them. An image the lossless encoding holds in fewer bytes
// stays lossless. A q outside 1 to 100 makes [Writer.ConvertColors] fail
// with [ErrInvalidColorTarget].
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
	// KOnlyBlackColors counts the colours [WithKOnlyBlack] wrote on the
	// black ink alone, of ConvertedOperators and of the annotation and
	// default appearance colours.
	KOnlyBlackColors int
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
	// TargetProfile says where the profile the colours were converted to
	// came from: [ProfileSourceOutputIntent] for [OutputIntentColorTarget],
	// [ProfileSourceCaller] for [ICCColorTarget] and for a
	// [CMYKColorTarget] read in the profile of [WithDefaultCMYKProfile], and
	// [ProfileSourceBuiltin] for litho's own profiles.
	TargetProfile string
	// SourceProfiles maps each device family the conversion read colours of
	// ("DeviceGray", "DeviceRGB", "DeviceCMYK") to where the profiles it read
	// the family through came from, sorted, once each: the output intent's
	// (ISO 32000-1, 14.11.5), the caller's, the target's for the target's own
	// family, litho's built-in one, the built-in one standing in for an
	// output intent or caller profile litho cannot convert through
	// ([ProfileSourceBuiltinFallback]), or a Default colour space of the
	// resources (8.6.5.6). An ICCBased space litho cannot read through its
	// own profile, read as its device family, is counted under that family.
	SourceProfiles map[string][]string
	// IntentFallbacks lists, once per profile, each profile a conversion read
	// through another table than the one for the rendering intent: a profile
	// without that table is read through its perceptual one (ICC.1:2010,
	// 6.2), or through its matrix and tone curves when it has no perceptual
	// table either. Empty when every profile held the intent's table or
	// serves every intent alike.
	IntentFallbacks []IntentFallback
	// OverprintPreserved counts the paintings under overprint (ISO
	// 32000-1, 8.6.7) that print as they did because the conversion wrote
	// the graphics state dictionaries in force at them, OverprintStates of
	// them, with /OPM 0: colour converted into CMYK under overprint mode 1
	// would otherwise leave the inks of its zero components unpainted.
	OverprintPreserved int
	OverprintStates    int
	// OverprintChanged counts the paintings under overprint that print
	// otherwise once converted, on a device that overprints: a spot colour
	// converted to process colour paints every process ink where it
	// painted its own alone, and a CMYK colour painted under overprint mode
	// 1 paints the inks its zero components left unpainted, or stops
	// leaving them. Viewers draw without overprinting by default, so the
	// change shows only in print or in an overprint preview.
	OverprintChanged int
	// BlackPointOperators counts the colour operators converted under a
	// graphics state whose /UseBlackPtComp (ISO 32000-2, Table 57) differs
	// from the conversion's own choice: /OFF by default, /ON under
	// [WithoutBlackPointCompensation]. BlackPointInlineImages counts the inline images so converted, and
	// BlackPointCopies the image XObjects and shadings written a second
	// time, converted under such a state, for the paintings under it.
	BlackPointOperators    int
	BlackPointInlineImages int
	BlackPointCopies       int
	// BlackPointUnhonoured counts the paintings under such a state whose
	// colour converts as the conversion's own choice asks all the same: a form XObject or a
	// Type 3 glyph, which inherits the state but converts once for every
	// painting, and an image or a shading painted by a stream whose
	// resources are not its own.
	BlackPointUnhonoured int
}

// Where a profile [Writer.ConvertColors] read came from, as
// [ColorReport.TargetProfile], [ColorReport.SourceProfiles] and
// [IntentFallback.Profile] name it.
const (
	// ProfileSourceBuiltin is litho's built-in profile of a device family.
	ProfileSourceBuiltin = "builtin"
	// ProfileSourceBuiltinFallback is litho's built-in profile standing in
	// for an output intent or caller profile litho cannot convert through.
	ProfileSourceBuiltinFallback = "builtin-fallback"
	// ProfileSourceCaller is a profile the caller gave: [ICCColorTarget]'s,
	// or [WithDefaultCMYKProfile]'s.
	ProfileSourceCaller = "caller"
	// ProfileSourceOutputIntent is the destination profile of the document's
	// output intent (ISO 32000-1, 14.11.5).
	ProfileSourceOutputIntent = "output-intent"
	// ProfileSourceTarget is the target's profile, which the target's own
	// device family means when nothing else names a profile for it.
	ProfileSourceTarget = "target"
	// ProfileSourceDefaultSpace is a Default colour space of the resources
	// (ISO 32000-1, 8.6.5.6) read in place of a device family.
	ProfileSourceDefaultSpace = "default-space"
	// ProfileSourceDocument is a profile the document embeds, an ICCBased
	// space's (8.6.5.5).
	ProfileSourceDocument = "document"
)

// IntentFallback is a profile [Writer.ConvertColors] read through another
// table than the one for the rendering intent [WithColorIntent] selects.
// IntentFallback values are immutable and safe for concurrent use.
type IntentFallback struct {
	// Target is true for the target's profile, false for a source's.
	Target bool
	// Components is the number of components of the profile's data colour
	// space: 1, 3 or 4.
	Components int
	// Profile says where the profile came from, one of the ProfileSource
	// constants.
	Profile string
	// Table is the table the profile was read through: "perceptual" or
	// "matrix" (its matrix and tone curves).
	Table string
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
// The device colours an annotation dictionary states, its /C and /IC and the
// /BC and /BG of its /MK (ISO 32000-1, Tables 164 and 189), are converted
// too: a viewer draws an annotation with no appearance stream from them
// (12.5.6), and so does [Writer.FlattenTransparency]. So is the text colour of
// each default appearance string, the /DA of an annotation, of a form field
// and of the interactive form (12.7.3.3), whose other operators stay: a viewer
// regenerating a field's appearance, as [Writer.FlattenForm] does, paints its
// text with it. A transparency group takes the target as its blending space
// (11.4.7, Table 147). A page group that names none, or names a /ColorSpace
// resource, which outside a content stream names no space (8.6.3), takes a
// grey or CMYK target. A page with no
// group gets one blending in a grey or CMYK target when the document's first
// output intent, after the conversion, characterises no device of the
// target's family, as under [WithoutOutputIntent]: the reader's device would
// otherwise choose the space, and a flattening would composite in sRGB. A
// document claiming PDF/A-1 gets no such group, since ISO 19005-1, 6.4
// forbids one.
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

	setIntent := !o.keepIntents && resolved.setsIntent

	engine := w.recolor
	if engine == nil {
		engine = recolorDocument
	}
	result, err := engine(ctx, alloc, doc, pages, recolorRequest{
		family:                 resolved.family,
		profile:                resolved.profile,
		components:             resolved.components,
		intent:                 intent,
		blackPointCompensation: !o.noBPC,
		convertSpots:           o.convertSpots,
		kOnlyBlack:             o.kOnlyBlack,
		jpegQuality:            o.jpegQuality,
		groupPages:             groupConvertedPages(ctx, alloc, doc, resolved.components, setIntent),
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
		KOnlyBlackColors:     result.kOnlyBlack,
		TargetProfile:        resolved.source,
		SourceProfiles:       result.sourceProfiles,
		IntentFallbacks:      result.intentFallbacks,
		OverprintPreserved:   result.overprintPreserved,
		OverprintStates:      result.overprintStates,
		OverprintChanged:     result.overprintChanged,

		BlackPointOperators:    result.blackPoint.Operators,
		BlackPointInlineImages: result.blackPoint.InlineImages,
		BlackPointCopies:       result.blackPoint.Copies,
		BlackPointUnhonoured:   result.blackPoint.Unhonoured,
	}
	for i := range report.IntentFallbacks {
		if report.IntentFallbacks[i].Target {
			report.IntentFallbacks[i].Profile = resolved.source
		}
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
	// source is where profile came from ([ColorReport.TargetProfile]).
	source string
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
		t.profile, t.source = profile, ProfileSourceOutputIntent
	case targetCMYK:
		t.profile, t.condition, t.registry = icc.CMYKPrinterProfile(), builtinCMYKCondition, iccRegistry
		t.source = ProfileSourceBuiltin
		if doc.CMYKProfile != nil {
			t.profile, t.condition, t.registry = bytes.Clone(doc.CMYKProfile), customCondition, ""
			t.source = ProfileSourceCaller
		}
	case targetRGB:
		t.profile, t.condition, t.source = icc.SRGBProfile(), "sRGB IEC61966-2.1", ProfileSourceBuiltin
	case targetGray:
		t.profile, t.condition, t.source = icc.SGrayProfile(), customCondition, ProfileSourceBuiltin
	case targetICC:
		t.profile, t.condition, t.source = target.profile, customCondition, ProfileSourceCaller
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

// groupConvertedPages reports whether a conversion to a target of components
// gives each page with no /Group a page group blending in the target
// ([recolor.Options.GroupPages]): a grey or CMYK target the document's output
// intent will not characterise after the conversion, which setIntent says
// makes the target its GTS_PDFA1 intent, in a document that claims no
// conformance to ISO 19005-1.
//
// ISO 32000-1, 11.4.7 and Table 147 leave the blending space of a page with
// no group to the reader's device, which 14.11.5 has the output intent name,
// and a flattening composites such a page in the device of the first output
// intent of the document whose profile reads, or in sRGB when it names no
// grey or CMYK device ([Writer.FlattenTransparency]). A conversion that makes
// the target that intent needs no group; one that leaves another intent, as
// [WithoutOutputIntent] does, or none, would composite a converted page in
// another space. ISO 19005-1, 6.4 forbids a transparency group on a page, so
// a PDF/A-1 document gets none (ISO 19005-1, 6.7.11 makes pdfaid:part its
// claim).
func groupConvertedPages(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, components int, setIntent bool) bool {
	if components == 3 || semantics.DeclaredPDFAPart(ctx, doc) == 1 {
		return false
	}
	family := graphics.FamilyDeviceCMYK
	if components == 1 {
		family = graphics.FamilyDeviceGray
	}
	return intendedFamily(ctx, alloc, doc, family, setIntent) != family
}

// intendedFamily returns the device family the first output intent of the
// document with a readable profile characterises once the conversion has set
// its intents ([pdfimage.OutputIntentsFamily]), "" for none. With setIntent
// the GTS_PDFA1 intents take the target, of family target, in place
// ([mutations.SetOutputIntent]), and one is added at the end when there is
// none. A replaced intent of another subtype keeps its family.
func intendedFamily(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, target string, setIntent bool) string {
	var intents ast.PdfArray
	if ref, ok := mutations.CatalogRef(ctx, doc); ok {
		catalog, err := mutations.LiveCatalogDict(ctx, alloc, doc, ref)
		if err != nil {
			doc.Logger.Debug("convert colors: reading the catalog for its output intents", slog.Any("err", err))
		} else if catalog["OutputIntents"] != nil {
			intents, err = document.ResolveArray(ctx, doc, catalog["OutputIntents"])
			if err != nil {
				doc.Logger.Debug("convert colors: reading the output intents", slog.Any("err", err))
			}
		}
	}
	for _, entry := range intents {
		if setIntent {
			// ISO 32000-2, Table 401: GTS_PDFA1 is the subtype
			// [mutations.SetOutputIntent] writes the target under.
			if d, err := document.ResolveDict(ctx, doc, entry); err == nil && d != nil {
				if s, _ := document.ResolveName(ctx, doc, d["S"]); s == "GTS_PDFA1" {
					return target
				}
			}
		}
		if f := pdfimage.OutputIntentsFamily(ctx, doc, ast.PdfArray{entry}); f != "" {
			return f
		}
	}
	if setIntent {
		return target
	}
	return ""
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
