package pdf

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// maxComposedPages caps the pages one [Compose] writes. Anti-DoS guard, not
// spec-mandated: ISO 32000-1 sets no page count limit, but every page a
// placement's TargetPage implies is allocated, so an unbounded TargetPage
// would let one number in a request allocate millions of pages. 100,000 pages
// is far beyond a sheet of labels or an imposition job, and costs a few tens
// of megabytes at most.
const maxComposedPages = 100_000

// Placement draws a page of a source document on a page [Compose] writes.
// Values must not be shared across goroutines without synchronization.
type Placement struct {
	// Source is the document the page comes from. Required. The caller keeps
	// ownership and keeps it open until Compose returns.
	Source *Reader
	// Page is the 1-based source page. Zero is treated as 1.
	Page int
	// TargetPage is the 1-based composed page the source page is drawn on.
	// Zero is treated as 1.
	TargetPage int
	// X and Y position, in points, the bottom-left corner of the source page
	// as a viewer shows it: its crop box, turned by its /Rotate.
	X, Y float64
	// Scale is the uniform scale factor. Zero is treated as 1. Negative is
	// refused.
	Scale float64
	// Rotation is the counter-clockwise rotation in degrees around (X, Y).
	Rotation float64
}

// Compose writes to dst a new document of pages width by height points, and
// draws each placement's source page on its target page, in order: a later
// placement paints over an earlier one where they overlap.
//
// The output has as many pages as the highest [Placement.TargetPage] names,
// at least one, at most 100,000. A page no placement names is blank. Every
// page's /MediaBox is [0 0 width height]. width and height must each lie
// between 3 and 14,400 points, the page sizes ISO 32000-1, Annex C.2, Table
// C.1 allows. Otherwise Compose fails with [ErrInvalidPageSize].
//
// A placement draws the source page as a viewer shows it, as
// [Writer.StampPDF] draws one: clipped to its crop box (ISO 32000-1, 14.11.2),
// the media box when it has none, and turned by its /Rotate (ISO 32000-1,
// 7.7.3.3, Table 30). Before Scale and Rotation apply, it spans the Width and
// Height [Reader.Page] reports for the source page, with its bottom-left
// corner at (X, Y). The source page's /UserUnit is not applied: the page is
// drawn at one point per unit, where [Reader.Page] multiplies the size by it.
// The placed content is the composed page's own, not an artifact, so
// [Reader.ExtractText] finds the sources' text without [WithArtifacts].
//
// # What is not drawn
//
//   - Annotations, form fields and their widgets: a link, a comment or a
//     field's appearance on a source page does not reach the output, and a
//     source's signatures are dropped (see [WithDroppedSignatures]).
//   - The sources' structure trees, bookmarks, page labels, metadata,
//     document information, attachments and other catalog entries. The
//     output is untagged. Only the sources' optional content groups are
//     carried, so a layer a source hides stays hidden.
//
// # Options
//
// Compose honours [WithVersion], [WithXRefStream], [WithObjectStreams],
// [WithDedup], [WithImageRecompression], [WithFontSubsetting],
// [WithEncryption], [WithWriteLogger], [WithDroppedSignatures] and
// [WithCompactionSkipped]. The output declares at least the highest version
// of its sources. [WithBookmarks] and [WithDefaultPageSize] do not apply:
// the output has no outline, and every page has a media box. A
// [WithDefaultPageSize] out of range still fails with [ErrInvalidPageSize].
// The output is written in the clear unless [WithEncryption] is passed, even
// when a source is encrypted.
//
// Identical sources, placements and options write identical bytes, unless
// the output is encrypted.
//
// # Errors
//
// Compose fails with [ErrInvalidPlacement], naming the placement by its index
// in placements, when placements is empty, a Source is nil, a Page lies
// outside its source, a TargetPage or a Scale is negative, a TargetPage lies
// past 100,000, or X, Y, Scale or Rotation is not a finite number. Both
// sentinels satisfy [IsInvalidInput]. A source that cannot be read fails with
// the error reading it.
//
// # Concurrency
//
// Compose reads a copy of each source's document, as [Merge] does, so the
// sources are left as they were and other goroutines may read them
// meanwhile. Concurrent calls are safe with distinct dst. One Reader may
// appear in several placements. The caller closes the sources, after Compose
// returns.
func Compose(ctx context.Context, width, height float64, placements []Placement, dst io.Writer, opts ...WriteOption) (err error) {
	var lg *slog.Logger
	for _, pl := range placements {
		if pl.Source != nil {
			lg = pl.Source.logger()
			break
		}
	}
	defer recoverToError(lg, "pdf.Compose", &err)
	cfg := writeOpts{}
	for _, o := range opts {
		o(&cfg)
	}
	if cfg.pageSizeErr != nil {
		return cfg.pageSizeErr
	}
	mediaBox, err := composedMediaBox(width, height)
	if err != nil {
		return err
	}
	pageCount, err := checkPlacements(placements)
	if err != nil {
		return err
	}

	// One copy of each distinct Reader, in the order placements first name
	// them, so the copier, and the output's object numbers, follow the
	// placements alone. A Reader named twice is read through one copy, which
	// lets its placements share the objects they both reach.
	index := make(map[*Reader]int)
	var forks []*Reader
	internal := make([]mutations.ComposePlacement, len(placements))
	for i, pl := range placements {
		srcIdx, ok := index[pl.Source]
		if !ok {
			srcIdx = len(forks)
			index[pl.Source] = srcIdx
			forks = append(forks, pl.Source.fork())
		}
		internal[i] = mutations.ComposePlacement{
			Source:     srcIdx,
			Page:       max(pl.Page, 1) - 1,
			TargetPage: max(pl.TargetPage, 1) - 1,
			X:          pl.X,
			Y:          pl.Y,
			Scale:      pl.Scale,
			Rotation:   pl.Rotation,
		}
		if pl.Scale == 0 {
			internal[i].Scale = 1
		}
	}
	if cfg.logger != nil {
		forks[0].doc.Logger = cfg.logger
	}
	forks[0].doc.Logger.Debug("compose started",
		slog.Int("placements", len(placements)),
		slog.Int("sources", len(forks)),
		slog.Int("pages", pageCount),
	)

	sources := make([]mutations.MergeSource, len(forks))
	for i, src := range forks {
		// As in Merge, a page a copy writes directly in /Kids is numbered
		// there (see [Writer.promotePageTree]).
		if err := src.doc.PromoteDirectKids(ctx); err != nil {
			return fmt.Errorf("numbering the pages a source writes in /Kids: %w", err)
		}
		pages, err := src.resolvePages(ctx)
		if err != nil {
			return fmt.Errorf("resolving source pages: %w", err)
		}
		sources[i] = mutations.MergeSource{Doc: src.doc, Pages: pages}
	}
	for i, pl := range internal {
		if n := len(sources[pl.Source].Pages); pl.Page >= n {
			return fmt.Errorf("%w: placement %d: page %d lies outside its source's %d pages", ErrInvalidPlacement, i, pl.Page+1, n)
		}
	}

	if err := mutations.Compose(ctx, mutations.ComposeConfig{
		MediaBox:   mediaBox,
		Pages:      pageCount,
		Sources:    sources,
		Placements: internal,
		Version:    cfg.version,
		XRefStream: cfg.xrefStream,
		Compact:    cfg.compactOptions(),
		Encrypt:    cfg.internalEncrypt(),
	}, dst); err != nil {
		return err
	}
	if cfg.droppedSignatures != nil {
		docs := make([]*document.Document, len(sources))
		for i, src := range sources {
			docs[i] = src.Doc
		}
		*cfg.droppedSignatures = droppedSignaturesOf(ctx, docs...)
	}
	if cfg.compactionSkipped != nil {
		*cfg.compactionSkipped = false
	}
	return nil
}

// composedMediaBox returns the /MediaBox of a composed page width by height
// points, or [ErrInvalidPageSize] when a side lies outside the sizes ISO
// 32000-1, Annex C.2, Table C.1 allows.
func composedMediaBox(width, height float64) (ast.PdfArray, error) {
	if !(width >= minPageSize && width <= maxPageSize && height >= minPageSize && height <= maxPageSize) {
		return nil, fmt.Errorf("%w: %g by %g, each side must lie between %g and %g", ErrInvalidPageSize, width, height, minPageSize, maxPageSize)
	}
	return ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), pageExtent(width), pageExtent(height)}, nil
}

// checkPlacements refuses the placements [Compose] cannot draw before any
// source is read, and returns the page count of the output.
func checkPlacements(placements []Placement) (int, error) {
	if len(placements) == 0 {
		return 0, fmt.Errorf("%w: no placements", ErrInvalidPlacement)
	}
	pages := 1
	for i, pl := range placements {
		var problem string
		switch {
		case pl.Source == nil:
			problem = "no source"
		case pl.Page < 0:
			problem = fmt.Sprintf("page %d is negative", pl.Page)
		case pl.TargetPage < 0:
			problem = fmt.Sprintf("target page %d is negative", pl.TargetPage)
		case pl.TargetPage > maxComposedPages:
			problem = fmt.Sprintf("target page %d lies past the %d pages a composition may hold", pl.TargetPage, maxComposedPages)
		case !finite(pl.X) || !finite(pl.Y):
			problem = fmt.Sprintf("position (%g, %g) is not finite", pl.X, pl.Y)
		case !finite(pl.Scale):
			problem = fmt.Sprintf("scale %g is not finite", pl.Scale)
		case pl.Scale < 0:
			problem = fmt.Sprintf("scale %g is negative", pl.Scale)
		case !finite(pl.Rotation):
			problem = fmt.Sprintf("rotation %g is not finite", pl.Rotation)
		}
		if problem != "" {
			return 0, fmt.Errorf("%w: placement %d: %s", ErrInvalidPlacement, i, problem)
		}
		pages = max(pages, pl.TargetPage)
	}
	return pages, nil
}

// finite reports whether v is neither NaN nor an infinity.
func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// Compose opens the pipeline on a composed document, as [Compose] writes one:
// pages width by height points, with each placement drawn on its target page.
// It must be the first stage, in place of [Pipeline.Open]: a Compose after
// another stage fails with [ErrPipelineNoSource]. The stage honours the
// options [Compose] honours. The caller keeps every placement's Source open
// until [Pipeline.WriteTo] returns.
//
// A later stage edits the composed document as it edits an opened one. The
// composed document counts as encrypted when a source is encrypted, as a
// Merge's output does (see [Pipeline]).
func (p *Pipeline) Compose(width, height float64, placements []Placement, opts ...WriteOption) *Pipeline {
	cp := append([]Placement(nil), placements...)
	return p.add(&composeStage{width: width, height: height, placements: cp, opts: opts})
}

type composeStage struct {
	width, height float64
	placements    []Placement
	opts          []WriteOption
}

func (s *composeStage) kind() stageKind { return kindOpen }
func (s *composeStage) name() string    { return stageNameCompose }

// apply writes the composed document, the pipeline's output when the stage is
// the only one, and an intermediate file the next stage reads otherwise.
func (s *composeStage) apply(ctx context.Context, ex *executor) error {
	opts := ex.restructureOptions(s.opts)
	if ex.isLastStage() {
		if err := Compose(ctx, s.width, s.height, s.placements, ex.finalDst, opts...); err != nil {
			return err
		}
		ex.recordStage(s.name(), true, 0)
		ex.recordRestructured(ctx, s.sources()...)
		ex.dstWritten = true
		return nil
	}
	f, err := ex.newTempFile()
	if err != nil {
		return err
	}
	if err := Compose(ctx, s.width, s.height, s.placements, f, opts...); err != nil {
		return err
	}
	sources := s.sources()
	ex.recordRestructured(ctx, sources...)
	// The composed document is read with the configuration of the first
	// source, as a Merge's output is read with its first source's. The
	// pipeline does not own that Reader.
	ex.cur = sources[0]
	ex.curOwned = false
	if err := ex.reopenAsCurrent(ctx, f, sources); err != nil {
		return err
	}
	var size int64
	if stat, statErr := f.Stat(); statErr == nil {
		size = stat.Size()
	}
	ex.recordStage(s.name(), true, size)
	return nil
}

// sources returns the distinct Readers the placements draw from, in the order
// they first name them. It runs after [Compose] succeeded, so none is nil.
func (s *composeStage) sources() []*Reader {
	var out []*Reader
	seen := make(map[*Reader]bool)
	for _, pl := range s.placements {
		if !seen[pl.Source] {
			seen[pl.Source] = true
			out = append(out, pl.Source)
		}
	}
	return out
}
