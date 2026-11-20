package models

// RenderBox selects the page boundary
// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPage] draws, by the page
// boundaries of ISO 32000-1, 14.11.2.
type RenderBox uint8

const (
	// RenderCropBox draws the crop box, the region a viewer shows and
	// prints. It is the default, and what pdftoppm draws.
	RenderCropBox RenderBox = iota
	// RenderMediaBox draws the whole medium the page is drawn on.
	RenderMediaBox
	// RenderBleedBox draws the bleed box, the region a production
	// environment clips the page to, bleed included.
	RenderBleedBox
	// RenderTrimBox draws the trim box, the finished page.
	RenderTrimBox
	// RenderArtBox draws the art box, the page's meaningful content.
	RenderArtBox
)

// RenderColorModel selects the colour model of the image
// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPage] returns.
type RenderColorModel uint8

const (
	// RenderRGB returns an *image.NRGBA in sRGB. It is the default.
	RenderRGB RenderColorModel = iota
	// RenderCMYK returns an *image.CMYK: amounts of ink, for a CMYK device
	// whose ICC profile
	// [github.com/gotenberg/litho/pkg/pdf.WithRenderCMYKProfile] sets.
	RenderCMYK
)

// RenderContent is a set of kinds of content
// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPage] draws, told apart by
// the operator that paints them, as Ghostscript's -dFILTERTEXT, -dFILTERIMAGE
// and -dFILTERVECTOR tell them apart. Combine kinds with |.
//
// Content of a kind left out paints nothing and keeps every other effect: a
// path still clips (ISO 32000-1, 8.5.4) and text in a clipping mode still
// clips (9.3.6). The kind is the painting operator's: a Type 3 glyph is text
// whatever its description paints, the cells of a tiling pattern belong to
// the fill or stroke they paint, an annotation is drawn whole with its
// appearance stream, and a form XObject's content is told apart
// operator by operator.
type RenderContent uint8

const (
	// RenderText is what the text-showing operators paint (ISO 32000-1,
	// 9.4.3).
	RenderText RenderContent = 1 << iota
	// RenderPaths is what the path-painting operators fill and stroke (ISO
	// 32000-1, 8.5.3), in a colour or a pattern.
	RenderPaths
	// RenderImages is what image XObjects and inline images paint, image
	// masks included (ISO 32000-1, 8.9).
	RenderImages
	// RenderShadings is what the sh operator paints (ISO 32000-1, 8.7.4.2).
	RenderShadings
	// RenderAnnotations is the annotation appearances drawn over the page
	// ([github.com/gotenberg/litho/pkg/pdf.WithRenderTarget] selects which),
	// each whole whatever it paints.
	RenderAnnotations
)

// RenderAllContent holds every kind of [RenderContent]: what
// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPage] draws by default.
const RenderAllContent = RenderText | RenderPaths | RenderImages | RenderShadings | RenderAnnotations

// RenderTarget selects which annotations
// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPage] draws over a page, by
// the annotation flags of ISO 32000-1, 12.5.3.
type RenderTarget uint8

const (
	// RenderForView draws the annotations a viewer shows on screen: those
	// without the /NoView flag. It is the default, and what poppler's
	// pdftoppm draws.
	RenderForView RenderTarget = iota
	// RenderForPrint draws the annotations a viewer prints: those with the
	// /Print flag.
	RenderForPrint
)

// RenderReport says what
// [github.com/gotenberg/litho/pkg/pdf.Reader.RenderPageReport] drew other than
// the file asks, or did not draw: the parts of the image a caller should not
// take for the page's exact look. Every count is zero for a page drawn as the
// file describes it. Concurrent reads of a value are safe; a write needs
// synchronization.
type RenderReport struct {
	// PlaceholderGlyphs counts the glyphs drawn as hollow boxes, their font
	// neither embedded nor supplied by
	// [github.com/gotenberg/litho/pkg/pdf.WithRenderFontProvider],
	// [github.com/gotenberg/litho/pkg/pdf.WithRenderFontFiles] or
	// [github.com/gotenberg/litho/pkg/pdf.WithRenderSystemFonts] (ISO 32000-1,
	// 9.6.2.2 leaves a substitute to the reader).
	PlaceholderGlyphs int `json:"placeholderGlyphs"`
	// UnreadableGlyphs counts the glyphs left out because their font program,
	// embedded or supplied, could not draw them: a glyph description cut
	// short, out of range, or a composite that contains itself. They paint
	// nothing and clip nothing, as MuPDF and PDFium leave them out; another
	// viewer may draw part of them. A glyph that would paint nothing anyway,
	// invisible or left out by
	// [github.com/gotenberg/litho/pkg/pdf.WithRenderContent], is not counted.
	UnreadableGlyphs int `json:"unreadableGlyphs"`
	// SynthesizedAnnotations counts the annotations drawn from an appearance
	// litho built from the annotation dictionary, the file holding no
	// appearance stream: the Square, Circle, Line, Polygon, PolyLine, Ink,
	// text markup, Link and Text types. ISO 32000-1, 12.5.5 leaves their look
	// to the viewer, so another viewer may draw them otherwise; a Text
	// annotation's icon (12.5.6.4) is each viewer's own.
	SynthesizedAnnotations int `json:"synthesizedAnnotations"`
	// AnnotationsWithoutAppearance counts the annotations not drawn at all:
	// they hold no appearance stream and their look is a viewer's icon or
	// text (FreeText, Stamp and the like).
	AnnotationsWithoutAppearance int `json:"annotationsWithoutAppearance"`
	// StaleAppearances counts the form field widgets drawn from the
	// appearance stream the file holds although the form's
	// /NeedAppearances asks the viewer to build new ones (ISO 32000-1,
	// 12.7.2): litho builds none, so the field may show an out-of-date value.
	StaleAppearances int `json:"staleAppearances"`
	// TransparencyOverBudget counts the transparency groups, soft masks and
	// knockout text objects (ISO 32000-1, 11.4, 11.6.5.2, 9.3.8) whose rasters
	// would pass [Limits.MaxRenderPixels],
	// drawn other than the file asks: a group's content through the group's
	// alpha and soft mask without the group, a soft mask hiding what it masks,
	// a knockout text object glyph by glyph.
	TransparencyOverBudget int `json:"transparencyOverBudget"`
	// CoarseMasks counts the soft masks drawn at a coarser resolution than the
	// page to fit [Limits.MaxRenderPixels]:
	// their effect is kept, approximated within a few pixels of where the mask
	// changes.
	CoarseMasks int `json:"coarseMasks"`
	// Skipped counts the constructs the file damages, left out of the page
	// as a viewer leaves them out: an image that does not decode or whose
	// mask does not, a shading or pattern that does not compile, a form past
	// [Limits.MaxFormDepth] or in a cycle.
	Skipped int `json:"skipped"`
}
