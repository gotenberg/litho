package models

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

// ColorReport describes what
// [github.com/gotenberg/litho/pkg/pdf.Writer.ConvertColors] converted.
// Concurrent reads of a value are safe; a write needs synchronization.
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
	// KOnlyBlackColors counts the colours
	// [github.com/gotenberg/litho/pkg/pdf.WithKOnlyBlack] wrote on the black
	// ink alone, of ConvertedOperators and of the annotation and default
	// appearance colours.
	KOnlyBlackColors int
	// KeptSpotColorants names, sorted, the colorants of the spot colours
	// kept with their alternate space converted. Empty under
	// [github.com/gotenberg/litho/pkg/pdf.WithSpotConversion].
	KeptSpotColorants []string
	// OutputIntent is "added" when the conversion added a GTS_PDFA1 output
	// intent of the target's profile, "replaced" when it gave the existing
	// GTS_PDFA1 intents that profile, and "" when it left the intents alone.
	OutputIntent string
	// KeptOutputIntents lists the /S of each output intent of another
	// subtype the conversion left as it was, GTS_PDFX among them.
	KeptOutputIntents []string
	// ReplacedOutputIntents lists the /OutputConditionIdentifier of each
	// intent [github.com/gotenberg/litho/pkg/pdf.WithReplacedOutputIntents]
	// pointed at the target profile.
	ReplacedOutputIntents []string
	// TargetProfile says where the profile the colours were converted to came
	// from: [ProfileSourceOutputIntent] for
	// [github.com/gotenberg/litho/pkg/pdf.OutputIntentColorTarget],
	// [ProfileSourceCaller] for
	// [github.com/gotenberg/litho/pkg/pdf.ICCColorTarget] and for a
	// [github.com/gotenberg/litho/pkg/pdf.CMYKColorTarget] read in the profile
	// of [github.com/gotenberg/litho/pkg/pdf.WithDefaultCMYKProfile], and
	// [ProfileSourceBuiltin] for litho's own profiles.
	TargetProfile string
	// SourceProfiles maps each device family the conversion read colours of
	// ("DeviceGray", "DeviceRGB", "DeviceCMYK") to where the profiles it read the
	// family through came from, sorted, once each. A source is one of:
	//
	//   - The output intent's (ISO 32000-1, 14.11.5).
	//   - The caller's.
	//   - The target's, for the target's own family.
	//   - litho's built-in one.
	//   - The built-in one standing in for an output intent or caller profile
	//     litho cannot convert through ([ProfileSourceBuiltinFallback]).
	//   - A Default colour space of the resources (8.6.5.6).
	//
	// An ICCBased space litho cannot read through its own profile, read as its
	// device family, is counted under that family.
	SourceProfiles map[string][]string
	// IntentFallbacks lists, once per profile, each profile a conversion read
	// through another table than the one for the rendering intent: a profile
	// without that table is read through its perceptual one (ICC.1:2010,
	// 6.2), or through its matrix and tone curves when it has no perceptual
	// table either. Empty when every profile held the intent's table or
	// serves every intent alike.
	IntentFallbacks []IntentFallback
	// OverprintPreserved counts the paintings under overprint (ISO 32000-1,
	// 8.6.7) that print as they did because the conversion wrote the graphics
	// state dictionaries in force at them, OverprintStates of them, with /OPM 0.
	// Colour converted into CMYK under overprint mode 1 would otherwise leave the
	// inks of its zero components unpainted.
	OverprintPreserved int
	OverprintStates    int
	// OverprintChanged counts the paintings under overprint that print otherwise
	// once converted, on a device that overprints:
	//
	//   - A spot colour converted to process colour paints every process ink
	//     where it painted its own alone.
	//   - A CMYK colour painted under overprint mode 1 paints the inks its zero
	//     components left unpainted, or stops leaving them.
	//
	// Viewers draw without overprinting by default, so the change shows only in
	// print or in an overprint preview.
	OverprintChanged int
	// BlackPointOperators counts the colour operators converted under a
	// graphics state whose /UseBlackPtComp (ISO 32000-2, Table 57) differs
	// from the conversion's own choice, which is /OFF by default and /ON under
	// [github.com/gotenberg/litho/pkg/pdf.WithoutBlackPointCompensation].
	// BlackPointInlineImages counts the inline images so converted.
	// BlackPointCopies counts the image XObjects and shadings written a second
	// time, converted under such a state, for the paintings under it.
	BlackPointOperators    int
	BlackPointInlineImages int
	BlackPointCopies       int
	// BlackPointUnhonoured counts the paintings under such a state whose colour
	// converts as the conversion's own choice asks all the same:
	//
	//   - A form XObject or a Type 3 glyph, which inherits the state but converts
	//     once for every painting.
	//   - An image or a shading painted by a stream whose resources are not its
	//     own.
	BlackPointUnhonoured int
	// IntentOperators, IntentInlineImages, IntentCopies and IntentUnhonoured
	// count as the BlackPoint fields do, for a graphics state whose rendering
	// intent, set by the ri operator or an ExtGState /RI (ISO 32000-1,
	// 8.6.5.8), differs from
	// [github.com/gotenberg/litho/pkg/pdf.WithColorIntent]'s. An image stating
	// its own /Intent converts under it and is never copied for a state's.
	IntentOperators    int
	IntentInlineImages int
	IntentCopies       int
	IntentUnhonoured   int
}

// Where a profile [github.com/gotenberg/litho/pkg/pdf.Writer.ConvertColors]
// read came from, as [ColorReport.TargetProfile], [ColorReport.SourceProfiles]
// and [IntentFallback.Profile] name it.
const (
	// ProfileSourceBuiltin is litho's built-in profile of a device family.
	ProfileSourceBuiltin = "builtin"
	// ProfileSourceBuiltinFallback is litho's built-in profile standing in
	// for an output intent or caller profile litho cannot convert through.
	ProfileSourceBuiltinFallback = "builtin-fallback"
	// ProfileSourceCaller is a profile the caller gave:
	// [github.com/gotenberg/litho/pkg/pdf.ICCColorTarget]'s, or
	// [github.com/gotenberg/litho/pkg/pdf.WithDefaultCMYKProfile]'s.
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

// IntentFallback is a profile
// [github.com/gotenberg/litho/pkg/pdf.Writer.ConvertColors] read through
// another table than the one for the rendering intent
// [github.com/gotenberg/litho/pkg/pdf.WithColorIntent] selects. IntentFallback
// values are immutable and safe for concurrent use.
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
