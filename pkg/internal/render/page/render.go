package page

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/internal/render/colorconv"
	"github.com/gotenberg/litho/pkg/internal/render/glyph"
	"github.com/gotenberg/litho/pkg/internal/render/shade"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// maxRenderOperators bounds the operators one render executes, across the
// page and every form XObject it paints, each painting counted as often as it
// runs. Anti-DoS, not spec-mandated: ISO 32000-1, 8.10 lets forms paint forms
// any number of times, so a few kilobytes of forms that each paint the next
// twice run 2^depth times their operators, which [limits.Limits.MaxFormDepth]
// bounds only in depth. 1<<26 is a hundred times the operators of the
// heaviest real page, 608,043 on page 1 of pdfjs issue21089.pdf.
const maxRenderOperators = 1 << 26

// opsPerContextCheck is how many operators run between two checks of ctx.
const opsPerContextCheck = 100

// clipBudgetShare bounds the clip masks one render keeps alive at once: their
// pixels together may reach clipBudgetShare times the render pixel budget,
// past which the render fails with [ErrTooLarge]. Anti-DoS, not
// spec-mandated: each q that sets a clip keeps a mask the size of the page
// until its Q (ISO 32000-1, 8.4.2), so a stream of nested q W n holds as many
// masks as it nests. A mask holds 4 bytes a pixel against the page's 16, so
// eight page budgets of masks hold twice the memory of the largest page, and
// a page at 72 dpi a fraction of the budget keeps hundreds of nested clips.
const clipBudgetShare = 8

// Unsupported constructs, the keys of [Report.Unsupported].
const (
	// PlaceholderGlyph counts glyphs drawn as the hollow box of a font
	// neither the document nor [Options.Fonts] supplies a program for
	// (ISO 32000-1, 9.6.2.2 leaves a substitute to the reader).
	PlaceholderGlyph = "placeholder glyph"
	// UnsupportedSoftMask counts paintings under a soft mask (11.6.5.2),
	// painted unmasked.
	UnsupportedSoftMask = "soft mask"
	// UnsupportedBlendMode counts paintings under a blend mode other than
	// Normal (11.3.5), painted with Normal.
	UnsupportedBlendMode = "blend mode"
	// UnsupportedGroup counts form XObjects that are transparency groups
	// (11.6.6), painted as plain content.
	UnsupportedGroup = "transparency group"
	// UnsupportedAnnotation counts annotations with no appearance stream
	// (ISO 32000-1, 12.5.5), which draw nothing: a viewer may build one from
	// the annotation's other entries, litho does not.
	UnsupportedAnnotation = "annotation without appearance"
	// StaleAppearance counts widget annotations drawn from the appearance
	// stream the file holds although the form's /NeedAppearances asks the
	// viewer to construct a new one (ISO 32000-1, 12.7.2, Table 218): litho
	// builds none, so it draws what is stored, possibly out of date.
	StaleAppearance = "stale widget appearance"
	// Skipped counts constructs the file damages: an image that does not
	// decode, a shading or pattern that does not compile, a form past the
	// form-depth budget or in a cycle. A viewer skips them too.
	Skipped = "skipped"
)

// Options configures [Render].
type Options struct {
	// DPI is the resolution, in pixels per inch.
	DPI float64
	// Background is the unpremultiplied RGBA colour, each channel in [0, 1],
	// the page is drawn over. The zero value leaves it transparent.
	Background [4]float32
	// Fonts supplies programs for the fonts the document does not embed
	// ([glyph.Options.Substitutes]); nil supplies none, and their glyphs
	// draw as placeholders.
	Fonts glyph.Provider
	// Annotations selects the annotation appearances drawn over the page:
	// those a viewer shows on screen ([annot.View], the zero value, as
	// pdftoppm draws them) or those it prints ([annot.Print]), by the flags
	// of ISO 32000-1, 12.5.3.
	Annotations annot.Mode
}

// Report says what a render left out.
type Report struct {
	// Unsupported counts the constructs drawn without some of their effect,
	// or not at all, by the keys above.
	Unsupported map[string]int
}

func (r *Report) count(key string) {
	if r.Unsupported == nil {
		r.Unsupported = make(map[string]int)
	}
	r.Unsupported[key]++
}

// Render draws pg at opts.DPI and returns the canvas, its (0, 0) the top-left
// corner of the page's crop box as displayed ([PageGeometry]).
//
// The canvas and every raster at its size are held to the document's render
// pixel budget ([limits.Limits.MaxRenderPixels]): a page past it fails with
// [ErrTooLarge] before anything is drawn. A construct the file damages is
// skipped and counted in the report, as a viewer skips it, and content that
// does not parse leaves the page blank; the render fails only on ctx or a
// budget.
func Render(ctx context.Context, doc *document.Document, pg *document.Page, opts Options) (*surface.Canvas, Report, error) {
	var report Report
	g, err := PageGeometry(pg, opts.DPI)
	if err != nil {
		return nil, report, err
	}
	budget := doc.Limits().WithDefaults().MaxRenderPixels
	canvas, err := surface.NewCanvas(g.Width, g.Height, budget)
	if err != nil {
		return nil, report, fmt.Errorf("%w: %w", ErrTooLarge, err)
	}
	if opts.Background[3] > 0 {
		bg := opts.Background
		canvas.Fill(bg[0], bg[1], bg[2], bg[3])
	}
	ops, err := content.ParsePageContent(ctx, doc, pg.Dict, content.SplitGluedOperators(), content.InlineImageData())
	if err != nil {
		if fatal(err) {
			return nil, report, fmt.Errorf("parsing page content: %w", err)
		}
		// A viewer draws a page whose content it cannot read as blank paper,
		// its annotations still over it, rather than failing the document.
		report.count(Skipped)
		doc.Logger.Debug("render: skipping page content", slog.Any("err", err))
		ops = nil
	}
	m := graphics.NewMachine([4]float64{})
	m.Doc = doc
	r := &renderer{
		ctx:        ctx,
		doc:        doc,
		page:       pg,
		canvas:     canvas,
		device:     g.Matrix,
		budget:     budget,
		clipLimit:  clipBudgetShare * budget,
		report:     &report,
		m:          m,
		clips:      []*clip{newClip(g.Width, g.Height)},
		shadings:   make(map[ast.PdfIndirectRef]*shade.Shading),
		patterns:   make(map[ast.PdfIndirectRef]*shade.Pattern),
		fonts:      glyph.NewCache(doc, glyph.Options{Substitutes: opts.Fonts}),
		colorCache: &colorconv.Cache{},
		glyphs:     make(map[glyphKey]glyph.Glyph),
		type3:      make(map[glyphKey][]content.Operation),
	}
	if err := r.run(ops, pg.Resources, g.Matrix, 0); err != nil {
		return nil, report, err
	}
	if err := r.annotations(g, opts.Annotations); err != nil {
		return nil, report, err
	}
	return canvas, report, nil
}

// fatal reports whether err ends the render: a cancelled or expired ctx, or a
// budget. Any other error damages one construct, which is skipped.
func fatal(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTooLarge)
}

// skip logs and counts a construct the file damages.
func (r *renderer) skip(what string, err error) {
	r.report.count(Skipped)
	r.doc.Logger.Debug("render: skipping "+what, slog.Any("err", err))
}
