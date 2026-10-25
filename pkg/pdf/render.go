package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/internal/render/page"
)

// DefaultRenderDPI is the resolution [Reader.RenderPage] draws at when
// [WithRenderDPI] is not given, the default of poppler's pdftoppm.
const DefaultRenderDPI = 150

// RenderOption configures [Reader.RenderPage].
// RenderOption closures must be safe to call from any goroutine.
type RenderOption func(*renderOpts)

type renderOpts struct {
	dpi        float64
	background color.Color
	// fonts and systemFonts supply the fonts a document does not embed
	// (render_fonts.go).
	fonts       []FontProvider
	systemFonts *fontFiles
	// target selects the annotations drawn.
	target RenderTarget
	// content selects the kinds of content drawn.
	content RenderContent
	// box selects the page boundary drawn.
	box RenderBox
	// model selects the colour model of the image returned.
	model RenderColorModel
	// cmykProfile is the CMYK device's profile, nil for the page's
	// DeviceCMYK reading; cmykProfileErr says why the profile given could
	// not be read.
	cmykProfile    *icc.Profile
	cmykProfileSet bool
	cmykProfileErr error
	// overprint simulates overprinting ([WithOverprintSimulation]).
	overprint bool
}

// RenderBox selects the page boundary [Reader.RenderPage] draws, by the page
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

// WithRenderBox selects the page boundary drawn. Default [RenderCropBox].
// Each box is read as a viewer reads it: a bleed, trim or art box the page
// does not state, or states as no rectangle enclosing an area, is its crop
// box (ISO 32000-1, 7.7.3.3, Table 30), and every box is reduced to the media
// box (14.11.2.1). Content outside the box is cut off; content between a
// larger box and the crop box is drawn. Any other value makes
// [Reader.RenderPage] fail with [ErrInvalidRenderOptions].
func WithRenderBox(b RenderBox) RenderOption {
	return func(o *renderOpts) {
		o.box = b
	}
}

// RenderColorModel selects the colour model of the image [Reader.RenderPage]
// returns.
type RenderColorModel uint8

const (
	// RenderRGB returns an *image.NRGBA in sRGB. It is the default.
	RenderRGB RenderColorModel = iota
	// RenderCMYK returns an *image.CMYK: amounts of ink, for a CMYK device
	// whose ICC profile [WithRenderCMYKProfile] sets.
	RenderCMYK
)

// WithRenderColorModel selects the colour model of the image returned.
// Default [RenderRGB].
//
// Under [RenderCMYK] the page is composited for a CMYK device: its
// transparency blends in the device's CMYK, and the image holds the inks,
// over paper that takes none. A page group whose /CS names another space
// blends in the device's CMYK too, a deliberate departure from ISO 32000-1,
// 11.4.7 and 11.3.4, which blend it in that space: a CMYK device composites
// in its process space, the difference shows only under blend modes other
// than Normal, and blending in the group's space first would convert every
// colour through sRGB once more on the way to the device. DeviceCMYK colours reach the
// device unchanged, DeviceGray as black ink alone, and a Separation or
// DeviceN colour whose alternate is DeviceCMYK as its tint transform's
// output: a device colour space addresses the output device directly
// (8.6.4.1, 10.3). Every other colour, those of images and shadings
// included, converts through its sRGB into the device's profile, relative
// colorimetric, so a CMYK image of an ICCBased space is held to the sRGB
// gamut on the way. The background converts the same way, white to no ink.
// Any other value makes [Reader.RenderPage] fail with
// [ErrInvalidRenderOptions].
func WithRenderColorModel(m RenderColorModel) RenderOption {
	return func(o *renderOpts) {
		o.model = m
	}
}

// WithRenderCMYKProfile sets the ICC profile of the CMYK device
// [RenderCMYK] draws for. Without it the device is the one DeviceCMYK is
// read in for the page: the page's or the document's CMYK output intent
// (ISO 32000-2, 14.11.5), then [WithDefaultCMYKProfile], then litho's
// built-in profile of the CGATS TR 001 (SWOP) printing condition.
//
// profile holds the bytes of a CMYK ICC profile, and is copied and read
// once, here.
// [Reader.RenderPage] fails with [ErrInvalidRenderOptions] when litho cannot
// read colours through it, or when the colour model is not [RenderCMYK].
func WithRenderCMYKProfile(profile []byte) RenderOption {
	p, err := icc.Parse(bytes.Clone(profile))
	if err == nil && p.Components() != 4 {
		err = fmt.Errorf("profile of the %q colour space", p.Space)
	}
	return func(o *renderOpts) {
		o.cmykProfile, o.cmykProfileSet, o.cmykProfileErr = p, true, err
	}
}

// RenderContent is a set of kinds of content [Reader.RenderPage] draws, told
// apart by the operator that paints them, as Ghostscript's -dFILTERTEXT,
// -dFILTERIMAGE and -dFILTERVECTOR tell them apart. Combine kinds with |.
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
	// ([WithRenderTarget] selects which), each whole whatever it paints.
	RenderAnnotations
)

// RenderAllContent holds every kind of [RenderContent]: what
// [Reader.RenderPage] draws by default.
const RenderAllContent = RenderText | RenderPaths | RenderImages | RenderShadings | RenderAnnotations

// WithRenderContent draws only the kinds of content c holds, over the
// background. Default [RenderAllContent]. A value holding a bit no kind
// names makes [Reader.RenderPage] fail with [ErrInvalidRenderOptions].
func WithRenderContent(c RenderContent) RenderOption {
	return func(o *renderOpts) {
		o.content = c
	}
}

// RenderTarget selects which annotations [Reader.RenderPage] draws over a
// page, by the annotation flags of ISO 32000-1, 12.5.3.
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

// WithRenderTarget selects the annotations drawn: [RenderForView] (the
// default) or [RenderForPrint]. Any other value makes [Reader.RenderPage]
// fail with [ErrInvalidRenderOptions].
func WithRenderTarget(t RenderTarget) RenderOption {
	return func(o *renderOpts) {
		o.target = t
	}
}

// WithRenderDPI sets the resolution, in pixels per inch: a page 612 units wide
// draws 612 pixels wide at 72 dpi (ISO 32000-1, 8.3.2.3). Default
// [DefaultRenderDPI]. A resolution that is not positive and finite makes
// [Reader.RenderPage] fail with [ErrInvalidRenderOptions].
func WithRenderDPI(dpi float64) RenderOption {
	return func(o *renderOpts) {
		o.dpi = dpi
	}
}

// WithRenderBackground sets the colour the page is drawn over. Default opaque
// white, the paper a viewer shows. A transparent colour, or nil, leaves the
// pixels the page does not paint transparent.
func WithRenderBackground(c color.Color) RenderOption {
	return func(o *renderOpts) {
		o.background = c
	}
}

// WithOverprintSimulation draws the page as a device that prints CMYK
// separations prints it, as an overprint preview shows it (ISO 32000-1,
// 8.6.7): the page composites in DeviceCMYK, read through the document's
// output intent or the default CMYK profile, and a painting under overprint
// leaves the process inks it does not paint as they were. A DeviceCMYK colour
// under overprint mode 1 leaves those its zero components name, and a
// Separation or DeviceN colour of process colorants those it does not name.
// The first four spot colorants a page paints under overprint get
// separations of their own: a colour naming one leaves the inks it does not
// name under overprint, and each separation prints through its alternate
// space once the page is drawn. A fifth spot colorant, and a spot colour
// inside a transparency group, paints through its alternate space and
// knocks out what lies under it. Without it, the default, every painting knocks out what lies under it, as
// viewers draw a page on screen and as poppler's pdftoppm draws it without
// -overprint.
func WithOverprintSimulation() RenderOption {
	return func(o *renderOpts) {
		o.overprint = true
	}
}

// RenderPage draws the given 1-based page and returns it as an *image.NRGBA,
// or an *image.CMYK under [WithRenderColorModel] ([RenderCMYK]), whose (0, 0)
// is the top-left corner of the page's crop box, or of the boundary
// [WithRenderBox] selects, as displayed: its /Rotate and /UserUnit applied,
// each side rounded up to whole pixels.
//
// The renderer is litho's own. It draws paths, strokes, clipping, images and
// inline images with their masks, shadings, shading and tiling patterns, form
// XObjects, text in every rendering mode, Type 3 fonts included, and the
// appearance streams of the page's annotations ([WithRenderTarget] selects
// which), in colours converted to sRGB as litho decodes images: every colour
// space of ISO 32000-1, 8.6, the default colour spaces of the resources and
// the output intents of the page and the document honoured, uncalibrated
// DeviceCMYK through [WithDefaultCMYKProfile] or litho's built-in printer
// profile. It draws transparency: constant alpha, the sixteen blend modes
// (ColorDodge and ColorBurn by the edition of ISO 32000 the document's
// version names), soft masks, isolated and knockout transparency groups and
// text knockout, blending in the grey or CMYK space a transparency group's
// /CS names where its content paints with transparency, and in sRGB
// otherwise, or in the device's CMYK under [RenderCMYK]. It draws without
// overprinting unless [WithOverprintSimulation] asks for it. An annotation
// with no appearance stream
// draws as ISO 32000-1 defines it from its dictionary for the Square,
// Circle, Line, Polygon, PolyLine, Ink, text markup and Link types, as
// litho's own drawing of the predefined icon its /Name names for the Text
// type (12.5.6.4, Table 172, an icon each viewer draws its own way, 24 units
// square from the upper-left corner of /Rect, upright), and not at all for
// the other icon and text types (FreeText, Stamp and the like); nothing is
// written into the document. Content a document's default
// optional content configuration hides is not drawn, nor are the kinds of
// content [WithRenderContent] leaves out.
//
// Glyphs are drawn from the programs the document embeds. litho bundles no
// font program, so a font the document does not embed draws its glyphs as
// hollow boxes as wide as the widths it declares, unless
// [WithRenderFontProvider], [WithRenderFontFiles] or [WithRenderSystemFonts]
// supplies a program for it.
//
// It returns [ErrInvalidPageNumber] for a page outside the document,
// [ErrInvalidRenderOptions] for a bad resolution, target, content, box,
// colour model or CMYK profile, and
// [ErrRenderTooLarge] when the page passes [Limits.MaxRenderPixels]. An image,
// shading, pattern or form the file damages is left out of the page, as a
// viewer leaves it out, and logged at Debug; so is an image whose mask
// cannot be read, rather than painted showing what the mask hides. A mask
// past [Limits.MaxImagePixels] is read at the resolution the page draws it
// at. A soft mask of the graphics state whose group would pass
// [Limits.MaxRenderPixels] is drawn at a coarser resolution, and hides what
// it masks when even that does not fit; a transparency group past it paints
// its content through the group's alpha and soft mask, without the group.
//
// RenderPage is safe for concurrent use with every other read of the
// [Reader], RenderPage included: each call draws into its own raster and
// reads the document through its caches, which serialise themselves.
//
// [Reader.RenderPageReport] draws the same page and also says what the
// drawing approximated.
func (r *Reader) RenderPage(ctx context.Context, pageNum int, opts ...RenderOption) (_ image.Image, err error) {
	defer recoverToError(r.logger(), "Reader.RenderPage", &err)
	img, _, err := r.renderPage(ctx, pageNum, opts)
	return img, err
}

// RenderReport says what [Reader.RenderPageReport] drew other than the file
// asks, or did not draw: the parts of the image a caller should not take for
// the page's exact look. Every count is zero for a page drawn as the file
// describes it.
// Values must not be shared across goroutines without synchronization.
type RenderReport struct {
	// PlaceholderGlyphs counts the glyphs drawn as hollow boxes, their font
	// neither embedded nor supplied by [WithRenderFontProvider],
	// [WithRenderFontFiles] or [WithRenderSystemFonts] (ISO 32000-1, 9.6.2.2
	// leaves a substitute to the reader).
	PlaceholderGlyphs int
	// SynthesizedAnnotations counts the annotations drawn from an appearance
	// litho built from the annotation dictionary, the file holding no
	// appearance stream: the Square, Circle, Line, Polygon, PolyLine, Ink,
	// text markup, Link and Text types. ISO 32000-1, 12.5.5 leaves their look
	// to the viewer, so another viewer may draw them otherwise; a Text
	// annotation's icon (12.5.6.4) is each viewer's own.
	SynthesizedAnnotations int
	// AnnotationsWithoutAppearance counts the annotations not drawn at all:
	// they hold no appearance stream and their look is a viewer's icon or
	// text (FreeText, Stamp and the like).
	AnnotationsWithoutAppearance int
	// StaleAppearances counts the form field widgets drawn from the
	// appearance stream the file holds although the form's
	// /NeedAppearances asks the viewer to build new ones (ISO 32000-1,
	// 12.7.2): litho builds none, so the field may show an out-of-date value.
	StaleAppearances int
	// TransparencyOverBudget counts the transparency groups, soft masks and
	// knockout text objects (ISO 32000-1, 11.4, 11.6.5.2, 9.3.8) whose
	// rasters would pass [Limits.MaxRenderPixels], drawn other than the file
	// asks: a group's content through the group's alpha and soft mask
	// without the group, a soft mask hiding what it masks, a knockout text
	// object glyph by glyph.
	TransparencyOverBudget int
	// CoarseMasks counts the soft masks drawn at a coarser resolution than
	// the page to fit [Limits.MaxRenderPixels]: their effect is kept,
	// approximated within a few pixels of where the mask changes.
	CoarseMasks int
	// Skipped counts the constructs the file damages, left out of the page
	// as a viewer leaves them out: an image that does not decode or whose
	// mask does not, a shading or pattern that does not compile, a form past
	// [Limits.MaxFormDepth] or in a cycle.
	Skipped int
}

// RenderPageReport draws the page as [Reader.RenderPage] draws it, with the
// same options, errors and image, and returns a [RenderReport] counting what
// the image approximates or leaves out. A caller that shows the image as the
// page, or compares it against another renderer's, reads the report to know
// where it may differ.
//
// RenderPageReport is safe for concurrent use with every other read of the
// [Reader], as [Reader.RenderPage] is.
func (r *Reader) RenderPageReport(ctx context.Context, pageNum int, opts ...RenderOption) (_ image.Image, _ RenderReport, err error) {
	defer recoverToError(r.logger(), "Reader.RenderPageReport", &err)
	img, rep, err := r.renderPage(ctx, pageNum, opts)
	if err != nil {
		return nil, RenderReport{}, err
	}
	return img, renderReport(r.logger(), rep), nil
}

// renderReport translates the renderer's report into the public one. A key
// of [page.Report.Unsupported] it does not know is logged at Debug rather
// than dropped silently; TestRenderReportCoversEveryKey keeps the switch
// complete.
func renderReport(logger *slog.Logger, rep page.Report) RenderReport {
	out := RenderReport{CoarseMasks: rep.CoarseMasks, SynthesizedAnnotations: rep.Synthesized}
	for _, k := range slices.Sorted(maps.Keys(rep.Unsupported)) {
		n := rep.Unsupported[k]
		switch k {
		case page.PlaceholderGlyph:
			out.PlaceholderGlyphs += n
		case page.TransparencyOverBudget:
			out.TransparencyOverBudget += n
		case page.UnsupportedAnnotation:
			out.AnnotationsWithoutAppearance += n
		case page.StaleAppearance:
			out.StaleAppearances += n
		case page.Skipped:
			out.Skipped += n
		default:
			logger.Debug("render: report key without a public field", slog.String("key", k), slog.Int("count", n))
		}
	}
	return out
}

// renderPage draws the page for [Reader.RenderPage] and
// [Reader.RenderPageReport], logging at Debug what the render approximated.
func (r *Reader) renderPage(ctx context.Context, pageNum int, opts []RenderOption) (image.Image, page.Report, error) {
	o := renderOpts{dpi: DefaultRenderDPI, background: color.White, content: RenderAllContent}
	for _, opt := range opts {
		opt(&o)
	}
	var mode annot.Mode
	switch o.target {
	case RenderForView:
		mode = annot.View
	case RenderForPrint:
		mode = annot.Print
	default:
		return nil, page.Report{}, fmt.Errorf("page %d: %w: render target %d", pageNum, ErrInvalidRenderOptions, o.target)
	}
	if o.content&^RenderAllContent != 0 {
		return nil, page.Report{}, fmt.Errorf("page %d: %w: render content %#x", pageNum, ErrInvalidRenderOptions, uint8(o.content))
	}
	box, ok := renderBoxes[o.box]
	if !ok {
		return nil, page.Report{}, fmt.Errorf("page %d: %w: render box %d", pageNum, ErrInvalidRenderOptions, o.box)
	}
	if o.model != RenderRGB && o.model != RenderCMYK {
		return nil, page.Report{}, fmt.Errorf("page %d: %w: colour model %d", pageNum, ErrInvalidRenderOptions, o.model)
	}
	if o.cmykProfileSet {
		switch {
		case o.model != RenderCMYK:
			return nil, page.Report{}, fmt.Errorf("page %d: %w: a CMYK profile for an RGB render", pageNum, ErrInvalidRenderOptions)
		case o.cmykProfileErr != nil:
			return nil, page.Report{}, fmt.Errorf("page %d: %w: CMYK profile: %w", pageNum, ErrInvalidRenderOptions, o.cmykProfileErr)
		}
	}
	pg, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, page.Report{}, err
	}
	var bg [4]float32
	if o.background != nil {
		nc, ok := color.NRGBAModel.Convert(o.background).(color.NRGBA)
		if ok {
			bg = [4]float32{float32(nc.R) / 255, float32(nc.G) / 255, float32(nc.B) / 255, float32(nc.A) / 255}
		}
	}
	providers := o.fonts
	if o.systemFonts != nil {
		providers = append(slices.Clip(providers), o.systemFonts)
	}
	img, report, err := renderImage(ctx, r.doc, pg, o.model, page.Options{
		DPI:         o.dpi,
		Background:  bg,
		Fonts:       renderFontProvider(r.logger(), providers),
		Annotations: mode,
		Omit:        omittedContent(o.content),
		Box:         box,
		CMYK:        o.model == RenderCMYK,
		CMYKProfile: o.cmykProfile,
		Overprint:   o.overprint,
	})
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		case errors.Is(err, page.ErrInvalidOptions):
			return nil, page.Report{}, fmt.Errorf("page %d: %w: %v", pageNum, ErrInvalidRenderOptions, err)
		case errors.Is(err, page.ErrTooLarge):
			return nil, page.Report{}, fmt.Errorf("page %d: %w: %v", pageNum, ErrRenderTooLarge, err)
		}
		return nil, page.Report{}, fmt.Errorf("rendering page %d: %w", pageNum, err)
	}
	if len(report.Unsupported) > 0 {
		attrs := make([]any, 0, len(report.Unsupported)+1)
		attrs = append(attrs, slog.Int("page", pageNum))
		for _, k := range slices.Sorted(maps.Keys(report.Unsupported)) {
			attrs = append(attrs, slog.Int(k, report.Unsupported[k]))
		}
		r.logger().Debug("render: drawn without some constructs", attrs...)
	}
	if report.CoarseMasks > 0 {
		r.logger().Debug("render: soft masks drawn at a coarser resolution", slog.Int("page", pageNum), slog.Int("masks", report.CoarseMasks))
	}
	return img, report, nil
}

// renderImage draws pg with opts and returns it in the colour model m: the
// page group's sRGB canvas as an *image.NRGBA, or its inks as an
// *image.CMYK.
func renderImage(ctx context.Context, doc *document.Document, pg *document.Page, m RenderColorModel, opts page.Options) (image.Image, page.Report, error) {
	res, report, err := page.RenderGroup(ctx, doc, pg, opts)
	if err != nil {
		return nil, report, err
	}
	if m == RenderCMYK {
		img, err := res.CMYK(ctx)
		return img, report, err
	}
	canvas, err := res.Canvas(ctx)
	if err != nil {
		return nil, report, err
	}
	return canvas.Image(), report, nil
}

// renderBoxes maps each [RenderBox] to the renderer's.
var renderBoxes = map[RenderBox]page.Box{
	RenderCropBox:  page.CropBox,
	RenderMediaBox: page.MediaBox,
	RenderBleedBox: page.BleedBox,
	RenderTrimBox:  page.TrimBox,
	RenderArtBox:   page.ArtBox,
}

// omittedContent returns the kinds of content the renderer leaves out when
// [Reader.RenderPage] draws c.
func omittedContent(c RenderContent) page.Content {
	var omit page.Content
	for _, k := range []struct {
		public   RenderContent
		internal page.Content
	}{
		{RenderText, page.Text},
		{RenderPaths, page.Paths},
		{RenderImages, page.Images},
		{RenderShadings, page.Shadings},
		{RenderAnnotations, page.Annotations},
	} {
		if c&k.public == 0 {
			omit |= k.internal
		}
	}
	return omit
}
