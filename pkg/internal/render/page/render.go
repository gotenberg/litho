package page

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
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
	// TransparencyOverBudget counts transparency groups, soft masks and the
	// knockout groups of text objects (ISO 32000-1, 11.4, 11.6.5.2, 9.3.8)
	// whose rasters would pass the render pixel budget: their content is
	// painted into the enclosing group as plain content, unmasked, or glyph
	// by glyph.
	TransparencyOverBudget = "transparency over budget"
	// UnsupportedAnnotation counts annotations with no appearance stream
	// (ISO 32000-1, 12.5.5) litho cannot build one for, which draw nothing:
	// Text, FreeText, Stamp and the other types whose look is a viewer's
	// icon or text. The geometric, markup and Link types are drawn from
	// their dictionaries (package render/annot).
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
	// Omit names the kinds of content left out of the page; the zero value
	// draws everything. See [Content].
	Omit Content
}

// Content is a set of kinds of content, told apart by the operator that
// paints them, as Ghostscript's FILTERTEXT, FILTERIMAGE and FILTERVECTOR
// tell them apart.
//
// A kind left out paints nothing, but keeps every other effect: a path
// still clips (ISO 32000-1, 8.5.4), text in a clipping mode still adds its
// glyphs to the clip at ET (9.3.6), and text still advances the text
// position. The kind is the outermost painting operator's: a Type 3 glyph
// description is text however it paints (9.6.5), and the cells of a tiling
// pattern are the fill or stroke they paint (8.7.3.1). An annotation appearance
// is the annotation (12.5.5), drawn whole. A soft mask's group
// (11.6.5.2) shapes what it masks rather than painting, so it is drawn
// whole. A form XObject is a container: its content is told apart operator
// by operator.
type Content uint8

const (
	// Text is what the text-showing operators paint (9.4.3, Table 109).
	Text Content = 1 << iota
	// Paths is what the path-painting operators paint (8.5.3.1, Table 60),
	// in a colour, a shading pattern or a tiling pattern.
	Paths
	// Images is what Do paints for an image XObject and what BI paints,
	// stencil masks included (8.9).
	Images
	// Shadings is what sh paints (8.7.4.2).
	Shadings
	// Annotations is the annotation appearances drawn over the content
	// (12.5.5), each whole whatever kinds it paints.
	Annotations
)

// AllContent holds every kind of [Content].
const AllContent = Text | Paths | Images | Shadings | Annotations

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
		ctx:         ctx,
		doc:         doc,
		page:        pg,
		width:       g.Width,
		height:      g.Height,
		device:      g.Matrix,
		budget:      budget,
		clipLimit:   clipBudgetShare * budget,
		report:      &report,
		m:           m,
		clips:       []*clip{newClip(g.Width, g.Height)},
		edition:     blend.EditionFor(doc.Version),
		offscreen:   &offscreen{},
		masks:       newMaskCache(),
		normalForms: make(map[int]bool),
		shadings:    make(map[ast.PdfIndirectRef]*shade.Shading),
		patterns:    make(map[ast.PdfIndirectRef]*shade.Pattern),
		fonts:       glyph.NewCache(doc, glyph.Options{Substitutes: opts.Fonts}),
		colorCache:  &colorconv.Cache{},
		glyphs:      make(map[glyphKey]glyph.Glyph),
		type3:       make(map[glyphKey][]content.Operation),
		omit:        opts.Omit,
	}
	// 11.4.7: the page is an isolated group over a transparent backdrop,
	// composited with the medium afterwards; its own /Group may make it a
	// knockout group.
	attrs, _, _ := r.groupAttrs(pg.Dict)
	var root *blend.Group
	if attrs.Knockout {
		root, err = blend.NewGroup(image.Rect(0, 0, g.Width, g.Height), blend.Backdrop{}, blend.Attributes{Isolated: true, Knockout: true}, budget)
	} else {
		root, err = blend.NewPage(g.Width, g.Height, budget)
	}
	if err != nil {
		return nil, report, fmt.Errorf("%w: %w", ErrTooLarge, err)
	}
	r.groups = []frame{{group: root, direct: root.Direct()}}
	if err := r.run(ops, pg.DrawResources, g.Matrix, 0); err != nil {
		return nil, report, err
	}
	if opts.Omit&Annotations == 0 {
		// An appearance stream is the annotation (12.5.5): drawn whole.
		r.omit = 0
		if err := r.annotations(g, opts.Annotations); err != nil {
			return nil, report, err
		}
	}
	canvas := root.Canvas()
	if bg := opts.Background; bg[3] > 0 {
		underlay(canvas, bg)
	}
	return canvas, report, nil
}

// underlay composites c over the unpremultiplied colour bg, the medium the
// page group is composited with (ISO 32000-1, 11.4.7).
func underlay(c *surface.Canvas, bg [4]float32) {
	a := min(bg[3], 1)
	r, g, b := bg[0]*a, bg[1]*a, bg[2]*a
	for i := 0; i < len(c.Pix); i += 4 {
		switch keep := 1 - c.Pix[i+3]; {
		case keep <= 0:
			continue
		case keep >= 1:
			c.Pix[i], c.Pix[i+1], c.Pix[i+2], c.Pix[i+3] = r, g, b, a
			continue
		}
		keep := 1 - c.Pix[i+3]
		c.Pix[i] += r * keep
		c.Pix[i+1] += g * keep
		c.Pix[i+2] += b * keep
		c.Pix[i+3] += a * keep
	}
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
