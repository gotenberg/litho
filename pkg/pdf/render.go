package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"maps"
	"slices"

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

// RenderPage draws the given 1-based page and returns it as an *image.NRGBA
// whose (0, 0) is the top-left corner of the page's crop box as displayed:
// its /Rotate and /UserUnit applied, each side rounded up to whole pixels.
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
// otherwise. An annotation with no appearance stream
// draws as ISO 32000-1 defines it from its dictionary for the Square,
// Circle, Line, Polygon, PolyLine, Ink, text markup and Link types, and not
// at all for the icon and text types (Text, FreeText, Stamp and the like);
// nothing is written into the document. Content a document's default
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
// [ErrInvalidRenderOptions] for a bad resolution or target, and
// [ErrRenderTooLarge] when the page passes [Limits.MaxRenderPixels]. An image,
// shading, pattern or form the file damages is left out of the page, as a
// viewer leaves it out, and logged at Debug.
//
// RenderPage is safe for concurrent use with every other read of the
// [Reader], RenderPage included: each call draws into its own raster and
// reads the document through its caches, which serialise themselves.
func (r *Reader) RenderPage(ctx context.Context, pageNum int, opts ...RenderOption) (_ image.Image, err error) {
	defer recoverToError(r.logger(), "Reader.RenderPage", &err)
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
		return nil, fmt.Errorf("page %d: %w: render target %d", pageNum, ErrInvalidRenderOptions, o.target)
	}
	if o.content&^RenderAllContent != 0 {
		return nil, fmt.Errorf("page %d: %w: render content %#x", pageNum, ErrInvalidRenderOptions, uint8(o.content))
	}
	pg, err := r.resolvePage(ctx, pageNum)
	if err != nil {
		return nil, err
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
	canvas, report, err := page.Render(ctx, r.doc, pg, page.Options{
		DPI:         o.dpi,
		Background:  bg,
		Fonts:       renderFontProvider(r.logger(), providers),
		Annotations: mode,
		Omit:        omittedContent(o.content),
	})
	if err != nil {
		switch {
		case errors.Is(err, page.ErrInvalidOptions):
			return nil, fmt.Errorf("page %d: %w: %v", pageNum, ErrInvalidRenderOptions, err)
		case errors.Is(err, page.ErrTooLarge):
			return nil, fmt.Errorf("page %d: %w: %v", pageNum, ErrRenderTooLarge, err)
		}
		return nil, fmt.Errorf("rendering page %d: %w", pageNum, err)
	}
	if len(report.Unsupported) > 0 {
		attrs := make([]any, 0, len(report.Unsupported)+1)
		attrs = append(attrs, slog.Int("page", pageNum))
		for _, k := range slices.Sorted(maps.Keys(report.Unsupported)) {
			attrs = append(attrs, slog.Int(k, report.Unsupported[k]))
		}
		r.logger().Debug("render: drawn without some constructs", attrs...)
	}
	return canvas.Image(), nil
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
