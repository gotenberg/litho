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
// inline images with their masks, shadings and shading patterns, form
// XObjects, and text in every rendering mode, Type 3 fonts included, in
// colours converted to sRGB as litho decodes images: every colour space of
// ISO 32000-1, 8.6, the default colour spaces of the resources and the output
// intents of the page and the document honoured, uncalibrated DeviceCMYK
// through [WithDefaultCMYKProfile] or litho's built-in printer profile. It
// does not draw tiling patterns, annotations, or the effect of soft masks,
// blend modes and transparency groups yet; content using them is drawn
// without them. Content a document's default optional content configuration
// hides is not drawn.
//
// Glyphs are drawn from the programs the document embeds. litho bundles no
// font program, so a font the document does not embed draws its glyphs as
// hollow boxes as wide as the widths it declares, unless
// [WithRenderFontProvider], [WithRenderFontFiles] or [WithRenderSystemFonts]
// supplies a program for it.
//
// It returns [ErrInvalidPageNumber] for a page outside the document,
// [ErrInvalidRenderOptions] for a bad resolution, and [ErrRenderTooLarge] when
// the page passes [Limits.MaxRenderPixels]. An image, shading or form the
// file damages is left out of the page, as a viewer leaves it out, and logged
// at Debug.
//
// RenderPage is safe for concurrent use with every other read of the
// [Reader], RenderPage included: each call draws into its own raster and
// reads the document through its caches, which serialise themselves.
func (r *Reader) RenderPage(ctx context.Context, pageNum int, opts ...RenderOption) (_ image.Image, err error) {
	defer recoverToError(r.logger(), "Reader.RenderPage", &err)
	o := renderOpts{dpi: DefaultRenderDPI, background: color.White}
	for _, opt := range opts {
		opt(&o)
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
		DPI:        o.dpi,
		Background: bg,
		Fonts:      renderFontProvider(r.logger(), providers),
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
