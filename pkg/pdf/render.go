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
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/plugins"
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
	fonts       []plugins.FontProvider
	systemFonts *fontFiles
	// target selects the annotations drawn.
	target models.RenderTarget
	// content selects the kinds of content drawn.
	content models.RenderContent
	// box selects the page boundary drawn.
	box models.RenderBox
	// model selects the colour model of the image returned.
	model models.RenderColorModel
	// cmykProfile is the CMYK device's profile, nil for the page's
	// DeviceCMYK reading; cmykProfileErr says why the profile given could
	// not be read.
	cmykProfile    *icc.Profile
	cmykProfileSet bool
	cmykProfileErr error
	// overprint simulates overprinting ([WithOverprintSimulation]).
	overprint bool
	// redactionReading reads colours as the redaction pixel check does
	// ([WithRenderRedactionReading]).
	redactionReading bool
}

// WithRenderBox selects the page boundary drawn. Default
// [models.RenderCropBox]. Each box is read as a viewer reads it: a bleed, trim
// or art box the page does not state, or states as no rectangle enclosing an
// area, is its crop box (ISO 32000-1, 7.7.3.3, Table 30), and every box is
// reduced to the media box (14.11.2.1). Content outside the box is cut off;
// content between a larger box and the crop box is drawn. Any other value
// makes [Reader.RenderPage] fail with [ErrInvalidRenderOptions].
func WithRenderBox(b models.RenderBox) RenderOption {
	return func(o *renderOpts) {
		o.box = b
	}
}

// WithRenderColorModel selects the colour model of the image returned. Default
// [models.RenderRGB].
//
// Under [models.RenderCMYK] the page is composited for a CMYK device. Its
// transparency blends in the device's CMYK, and the image holds the inks, over
// paper that takes none.
//
// A page group whose /CS names another space blends in the device's CMYK too.
// This is a deliberate departure from ISO 32000-1, 11.4.7 and 11.3.4, which
// blend it in that space. A CMYK device composites in its process space, and
// the difference shows only under blend modes other than Normal. Blending in
// the group's space first would convert every colour through sRGB once more on
// the way to the device.
//
// Colours reach the device as follows:
//
//   - DeviceCMYK colours reach the device unchanged.
//   - DeviceGray colours become black ink alone.
//   - A Separation or DeviceN colour whose alternate is DeviceCMYK becomes its
//     tint transform's output. A device colour space addresses the output
//     device directly (8.6.4.1, 10.3).
//   - Every other colour, those of images and shadings included, converts
//     through its sRGB into the device's profile, relative colorimetric. A CMYK
//     image of an ICCBased space is held to the sRGB gamut on the way.
//   - The background converts the same way, white to no ink.
//
// Any other value makes [Reader.RenderPage] fail with
// [ErrInvalidRenderOptions].
func WithRenderColorModel(m models.RenderColorModel) RenderOption {
	return func(o *renderOpts) {
		o.model = m
	}
}

// WithRenderCMYKProfile sets the ICC profile of the CMYK device
// [models.RenderCMYK] draws for. Without it the device is the one DeviceCMYK is
// read in for the page: the page's or the document's CMYK output intent
// (ISO 32000-2, 14.11.5), then [WithDefaultCMYKProfile], then litho's
// built-in profile of the CGATS TR 001 (SWOP) printing condition.
//
// profile holds the bytes of a CMYK ICC profile, and is copied and read
// once, here.
// [Reader.RenderPage] fails with [ErrInvalidRenderOptions] when litho cannot
// read colours through it, or when the colour model is not [models.RenderCMYK].
func WithRenderCMYKProfile(profile []byte) RenderOption {
	p, err := icc.Parse(bytes.Clone(profile))
	if err == nil && p.Components() != 4 {
		err = fmt.Errorf("profile of the %q colour space", p.Space)
	}
	return func(o *renderOpts) {
		o.cmykProfile, o.cmykProfileSet, o.cmykProfileErr = p, true, err
	}
}

// WithRenderContent draws only the kinds of content c holds, over the
// background. Default [models.RenderAllContent]. A value holding a bit no kind
// names makes [Reader.RenderPage] fail with [ErrInvalidRenderOptions].
func WithRenderContent(c models.RenderContent) RenderOption {
	return func(o *renderOpts) {
		o.content = c
	}
}

// WithRenderTarget selects the annotations drawn: [models.RenderForView] (the
// default) or [models.RenderForPrint]. Any other value makes
// [Reader.RenderPage] fail with [ErrInvalidRenderOptions].
func WithRenderTarget(t models.RenderTarget) RenderOption {
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
// separations prints it, as an overprint preview shows it (ISO 32000-1, 8.6.7).
//
// The page composites in DeviceCMYK, read through the document's output intent
// or the default CMYK profile. A painting under overprint leaves the process
// inks it does not paint as they were:
//
//   - A DeviceCMYK colour under overprint mode 1 leaves the inks its zero
//     components name.
//   - A Separation or DeviceN colour of process colorants leaves the inks it
//     does not name.
//
// The first four spot colorants a page paints under overprint get separations
// of their own. A colour naming one leaves the inks it does not name under
// overprint, and each separation prints through its alternate space once the
// page is drawn. A fifth spot colorant, and a spot colour inside a transparency
// group, paints through its alternate space and knocks out what lies under it.
//
// Without this option, the default, every painting knocks out what lies under
// it. Viewers draw a page this way on screen, and so does poppler's pdftoppm
// without -overprint.
func WithOverprintSimulation() RenderOption {
	return func(o *renderOpts) {
		o.overprint = true
	}
}

// WithRenderRedactionReading draws the colours as the pixel check of
// [Writer.Redact] reads a blanked image, not as a viewer shows them.
//
// The reading differs from a viewer's in three ways:
//
//   - Every painting is under the identity transfer function (ISO 32000-1,
//     10.5).
//   - Every colour is under the /Default black point compensation, whatever a
//     graphics state's /UseBlackPtComp asks (ISO 32000-2, Table 57).
//   - An image with no /Intent of its own is under relative colorimetric, the
//     graphics state's initial intent (ISO 32000-1, 8.6.5.8), whatever the ri
//     operator set.
//
// Redaction blanks an image's covered samples to the darkest colour of its
// colour space. A transfer function maps that colour to any other, white
// included. An uncompensated profile whose black is lighter than sRGB's draws
// it as a grey. So does an ri operator selecting a table that lifts the black
// (an output profile's perceptual A2B0 may read full black ink at L* 50). Under
// this reading the blank of an image with no /Intent draws black or near it,
// and every lighter sample stays lighter.
//
// A caller that checks a redaction's pixels itself, under
// [WithoutRedactionPixelCheck], draws the page's images with it over black to
// tell a blanked sample from a kept one, as the check does.
func WithRenderRedactionReading() RenderOption {
	return func(o *renderOpts) {
		o.redactionReading = true
	}
}

// RenderPage draws the given 1-based page and returns it as an *image.NRGBA, or
// an *image.CMYK under [WithRenderColorModel] ([models.RenderCMYK]).
//
// The image's (0, 0) is the top-left corner of the page's crop box, or of the
// boundary [WithRenderBox] selects, as displayed: its /Rotate and /UserUnit
// applied, each side rounded up to whole pixels. The renderer is litho's own.
//
// # Supported
//
// RenderPage draws:
//
//   - Paths, strokes, clipping, images and inline images with their masks,
//     shadings, shading and tiling patterns, and form XObjects.
//   - Text in every rendering mode, Type 3 fonts included.
//   - The appearance streams of the page's annotations ([WithRenderTarget]
//     selects which).
//   - Colours converted to sRGB as litho decodes images. Every colour space of
//     ISO 32000-1, 8.6 is honoured, as are the default colour spaces of the
//     resources and the output intents of the page and the document.
//     Uncalibrated DeviceCMYK goes through [WithDefaultCMYKProfile] or litho's
//     built-in printer profile.
//   - Transparency: constant alpha, the sixteen blend modes (ColorDodge and
//     ColorBurn by the edition of ISO 32000 the document's version names), soft
//     masks, isolated and knockout transparency groups and text knockout.
//   - Blending in the grey or CMYK space a transparency group's /CS names where
//     its content paints with transparency, and in sRGB otherwise, or in the
//     device's CMYK under [models.RenderCMYK].
//   - An annotation with no appearance stream, as ISO 32000-1 defines it from
//     its dictionary, for the Square, Circle, Line, Polygon, PolyLine, Ink,
//     text markup and Link types. A Text annotation draws as litho's own
//     drawing of the predefined icon its /Name names (12.5.6.4, Table 172, an
//     icon each viewer draws its own way, 24 units square from the upper-left
//     corner of /Rect, upright). Nothing is written into the document.
//   - Glyphs from the programs the document embeds.
//
// # Unsupported
//
// RenderPage does not draw:
//
//   - Overprinting, unless [WithOverprintSimulation] asks for it.
//   - An annotation with no appearance stream of the other icon and text types
//     (FreeText, Stamp and the like).
//   - Content a document's default optional content configuration hides, nor
//     the kinds of content [WithRenderContent] leaves out.
//   - The glyphs of a font the document does not embed. litho bundles no font
//     program, so such a font draws its glyphs as hollow boxes as wide as the
//     widths it declares, unless [WithRenderFontProvider],
//     [WithRenderFontFiles] or [WithRenderSystemFonts] supplies a program for
//     it.
//
// An image, shading, pattern or form the file damages is left out of the page,
// as a viewer leaves it out, and logged at Debug. So is an image whose mask
// cannot be read, rather than painted showing what the mask hides. A mask past
// [models.Limits.MaxImagePixels] is read at the resolution the page draws it
// at. A soft mask of the graphics state whose group would pass
// [models.Limits.MaxRenderPixels] is drawn at a coarser resolution, and hides
// what it masks when even that does not fit. A transparency group past it
// paints its content through the group's alpha and soft mask, without the
// group.
//
// # Errors
//
// RenderPage returns:
//
//   - [ErrInvalidPageNumber] for a page outside the document.
//   - [ErrInvalidRenderOptions] for a bad resolution, target, content, box,
//     colour model or CMYK profile.
//   - [ErrRenderTooLarge] when the page passes [models.Limits.MaxRenderPixels].
//
// # Concurrency
//
// RenderPage is safe for concurrent use with every other read of the [Reader],
// RenderPage included: each call draws into its own raster and reads the
// document through its caches, which serialise themselves.
//
// [Reader.RenderPageReport] draws the same page and also says what the drawing
// approximated.
func (r *Reader) RenderPage(ctx context.Context, pageNum int, opts ...RenderOption) (_ image.Image, err error) {
	defer pageError(pageNum, &err)
	defer recoverToError(r.logger(), "Reader.RenderPage", &err)
	img, _, err := r.renderPage(ctx, pageNum, opts)
	return img, err
}

// RenderPageReport draws the page as [Reader.RenderPage] draws it, with the
// same options, errors and image, and returns a [models.RenderReport] counting
// what the image approximates or leaves out. A caller that shows the image as
// the page, or compares it against another renderer's, reads the report to
// know where it may differ.
//
// RenderPageReport is safe for concurrent use with every other read of the
// [Reader], as [Reader.RenderPage] is.
func (r *Reader) RenderPageReport(ctx context.Context, pageNum int, opts ...RenderOption) (_ image.Image, _ models.RenderReport, err error) {
	defer pageError(pageNum, &err)
	defer recoverToError(r.logger(), "Reader.RenderPageReport", &err)
	img, rep, err := r.renderPage(ctx, pageNum, opts)
	if err != nil {
		return nil, models.RenderReport{}, err
	}
	return img, renderReport(r.logger(), rep), nil
}

// renderReport translates the renderer's report into the public one. A key
// of [page.Report.Unsupported] it does not know is logged at Debug rather
// than dropped silently; TestRenderReportCoversEveryKey keeps the switch
// complete.
func renderReport(logger *slog.Logger, rep page.Report) models.RenderReport {
	out := models.RenderReport{CoarseMasks: rep.CoarseMasks, SynthesizedAnnotations: rep.Synthesized}
	for _, k := range slices.Sorted(maps.Keys(rep.Unsupported)) {
		n := rep.Unsupported[k]
		switch k {
		case page.PlaceholderGlyph:
			out.PlaceholderGlyphs += n
		case page.UnreadableGlyph:
			out.UnreadableGlyphs += n
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
	o := renderOpts{dpi: DefaultRenderDPI, background: color.White, content: models.RenderAllContent}
	for _, opt := range opts {
		opt(&o)
	}
	var mode annot.Mode
	switch o.target {
	case models.RenderForView:
		mode = annot.View
	case models.RenderForPrint:
		mode = annot.Print
	default:
		return nil, page.Report{}, fmt.Errorf("%w: render target %d", ErrInvalidRenderOptions, o.target)
	}
	if o.content&^models.RenderAllContent != 0 {
		return nil, page.Report{}, fmt.Errorf("%w: render content %#x", ErrInvalidRenderOptions, uint8(o.content))
	}
	if o.box > models.RenderArtBox {
		return nil, page.Report{}, fmt.Errorf("%w: render box %d", ErrInvalidRenderOptions, o.box)
	}
	if o.model != models.RenderRGB && o.model != models.RenderCMYK {
		return nil, page.Report{}, fmt.Errorf("%w: colour model %d", ErrInvalidRenderOptions, o.model)
	}
	if o.cmykProfileSet {
		switch {
		case o.model != models.RenderCMYK:
			return nil, page.Report{}, fmt.Errorf("%w: a CMYK profile for an RGB render", ErrInvalidRenderOptions)
		case o.cmykProfileErr != nil:
			return nil, page.Report{}, fmt.Errorf("%w: CMYK profile: %w", ErrInvalidRenderOptions, o.cmykProfileErr)
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
		Omit:        models.RenderAllContent &^ o.content,
		Box:         o.box,
		CMYK:        o.model == models.RenderCMYK,
		CMYKProfile: o.cmykProfile,
		Overprint:   o.overprint,

		NoTransfer:        o.redactionReading,
		DefaultBlackPoint: o.redactionReading,
	})
	if err != nil {
		// The renderer's sentinels are the public ones, and its message says
		// what failed: wrapping it would name the failure twice.
		if errors.Is(err, ErrInvalidRenderOptions) || errors.Is(err, ErrRenderTooLarge) {
			return nil, page.Report{}, err
		}
		return nil, page.Report{}, fmt.Errorf("rendering: %w", err)
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
func renderImage(ctx context.Context, doc *document.Document, pg *document.Page, m models.RenderColorModel, opts page.Options) (image.Image, page.Report, error) {
	res, report, err := page.RenderGroup(ctx, doc, pg, opts)
	if err != nil {
		return nil, report, err
	}
	if m == models.RenderCMYK {
		img, err := res.CMYK(ctx)
		return img, report, err
	}
	canvas, err := res.Canvas(ctx)
	if err != nil {
		return nil, report, err
	}
	return canvas.Image(), report, nil
}
