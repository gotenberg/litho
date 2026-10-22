package remediation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/flatten"
)

// FlattenFixer names the fixer that flattens the transparency ISO 19005-1, 6.4
// forbids ([fixTransparencyFlatten]), for [Options.Disabled].
const FlattenFixer = "transparency-flatten"

// remediationJPEGQuality is the JPEG quality a remediation encodes a
// photographic composite at ([flatten.Options.JPEGQuality]): 90, at which a
// composite keeps its chroma whole (see recolor.subsampleBelowQuality). A
// composite of images and shadings is a photograph a lossless encoding holds
// in several times the bytes, and a composite mixing photographs with vector
// art is cut into tiles, its photographic ones JPEG and the others lossless.
//
// # Corpus evidence
//
//   - govdocs (34 files): the PDF/A-1b outputs weigh 177.2 MB with every
//     composite lossless and 162.9 MB with photographic ones as JPEG, 8 files
//     holding one (thread_300_300860 17556952 -> 4109245 bytes); the sources
//     weigh 45.5 MB. Most of the rest was whole pages mixing photographs with
//     vector art, lossless whole: cut into tiles, the outputs weigh 134.4 MB
//     where they weighed 163.4 MB the commit before, thread_000_000737 39.6
//     -> 20.3 MB and thread_400_400019 30.0 -> 20.5 MB. No output draws apart
//     from its source, on any page of the 13 files the tiles change, in litho
//     or in poppler 26.09.0.
//   - verapdf: 23 of 522 files hold a JPEG composite, and no output draws
//     apart from its source that did not before.
const remediationJPEGQuality = 90

// flattenedPrefix starts the report line of a run that flattened a page.
const flattenedPrefix = "flattened transparency"

// flattenedAny reports whether lines, the report of [fixTransparencyFlatten],
// says it flattened a page.
func flattenedAny(lines []string) bool {
	return slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, flattenedPrefix) })
}

// fixTransparencyFlatten removes the transparency ISO 19005-1, 6.4 forbids by
// flattening it ([flatten.Document]): each region where transparent content
// paints is composited into one opaque image, at [flatten.DefaultDPI], and
// everything outside the regions, and every glyph inside them, stays vector
// content. The page keeps its appearance, where [fixTransparency], which runs
// after it, sets each transparency property opaque and changes what the page
// shows: a 50% overlay painted at full strength hides what it tinted.
//
// It runs in both modes. Part 1 offers no conforming file that keeps live
// transparency, so the repair is not optional, and [Options.PreserveContent]
// guards what a page shows: flattening keeps it, and the opaque normalization
// it replaces did not. What flattening gives up is how the transparent regions
// are encoded, their vector art and images becoming pixels at 300 dpi; text
// is never rasterised and keeps its marked content (ISO 32000-2, 14.6), so a
// tagged PDF/A-1a page keeps its structure and its searchable text.
//
// What flattening leaves, [fixTransparency] sets opaque, as before: the
// appearances of annotations, which it does not reach, the transparency a
// Type 3 glyph's description paints with, which stays text, and every page it
// refuses or cannot read. A page past its budgets is left to that fallback
// alone, the other pages flattened.
//
// It runs first of all the fixers, ahead of any edit queued on alloc: the
// renderer composites the document as doc holds it, not as alloc edits it, so
// an earlier repair of a page would be composited from its stale original and
// its splice written over (see [flatten.Document]). Every later fixer reads the
// pages through the allocator and starts from the flattened ones.
//
// # Corpus evidence
//
//   - verapdf (522 files), litho (20) and govdocs (34), the files litho finds
//     ISO 19005-1, 6.4 transparency in: over their PDF/A-1a and PDF/A-1b
//     remediations, default and aggressive, veraPDF 1.30.2 and litho report on
//     each output the rule set the opaque normalization alone left, file by
//     file, and neither reports 6.4 on any.
//   - Same files: the outputs drawing apart from their source at the render
//     cell's bars go from 83 to 35 (verapdf), 7 to 3 (litho) and 11 to 0
//     (govdocs) in each of the four runs. Six verapdf files newly draw apart,
//     at 72 dpi only: in PDF_A-2b 6-2-10-t01-fail-c and t03-fail-c, and their
//     PDF/A-4 copies, the 300 dpi composite draws a 1-point stroke the 72 dpi
//     rendering snaps to one pixel across two, and poppler 26.09.0 draws the
//     outputs like the sources; in 6-2-10-t06-fail-c and its copy, a Darken
//     pattern of overlapping tiles, litho draws the output as the source at
//     300 dpi, and poppler draws the source apart from litho.
//   - litho flattentext_2b and govdocs thread_000_000146, thread_000_000340,
//     thread_200_200744 and thread_400_400019: the integrity probe refused
//     their flattened outputs, text at constant alpha 0 now invisible and up
//     to 90 images of a page composited, and the writer fell back to the
//     opaque normalization. Counted as the probe now counts them, all five
//     keep their flattening, veraPDF and litho report the rule set the
//     fallback left, and their pages draw like the source.
func fixTransparencyFlatten(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	if compliance.PDFAPart(profile) != 1 {
		return nil, nil
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("getting catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("getting pages: %w", err)
	}

	snap := alloc.Snapshot()
	report, err := flatten.Document(ctx, alloc, doc, pages, flatten.Options{JPEGQuality: remediationJPEGQuality})
	refused := 0
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, fmt.Errorf("flattening transparency: %w", ctxErr)
		}
		// A page flattening refuses fails the whole run and leaves the
		// pages before it queued. Undone, each page is flattened on its
		// own, and the ones refused are left to the opaque normalization.
		alloc.Restore(snap)
		doc.Logger.Debug("remediate: flattening every page at once refused, flattening page by page", slog.Any("err", err))
		report, refused, err = flattenEachPage(ctx, alloc, doc, pages)
		if err != nil {
			return nil, err
		}
	}
	if opts.flattened != nil && report != nil {
		for _, p := range report.Pages {
			opts.flattened.images += p.ReplacedImages
		}
	}
	return describeFlattening(report, refused), nil
}

// flattenEachPage flattens the pages one at a time, so a page flattening
// refuses leaves the others flattened. It returns what it did and how many
// pages it refused. Each run composites with images of its own, where one run
// over the document shares their colour space: the cost of the fallback only.
func flattenEachPage(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) (*flatten.Report, int, error) {
	merged := &flatten.Report{}
	refused := 0
	for i, pg := range pages {
		if err := ctx.Err(); err != nil {
			return nil, 0, fmt.Errorf("flattening transparency: %w", err)
		}
		snap := alloc.Snapshot()
		report, err := flatten.Document(ctx, alloc, doc, []*document.Page{pg}, flatten.Options{JPEGQuality: remediationJPEGQuality})
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, fmt.Errorf("flattening transparency: %w", ctxErr)
			}
			alloc.Restore(snap)
			refused++
			doc.Logger.Debug("remediate: flattening refused a page, left to the opaque normalization",
				slog.Int("page", i+1), slog.Bool("too_complex", errors.Is(err, flatten.ErrTooComplex)), slog.Any("err", err))
			continue
		}
		for _, p := range report.Pages {
			p.Page = i
			merged.Pages = append(merged.Pages, p)
		}
		if len(report.Skipped) > 0 {
			merged.Skipped = append(merged.Skipped, i)
		}
		merged.TransparentAnnotations += report.TransparentAnnotations
	}
	return merged, refused, nil
}

// describeFlattening returns the report lines of a flattening: what it
// composited, the text whose look it approximates, and the pages it refused,
// which the opaque normalization takes.
func describeFlattening(report *flatten.Report, refused int) []string {
	if report == nil || len(report.Pages) == 0 {
		return nil
	}
	var regions, kept, approximated, jpeg int
	dpi := 0.0
	for _, p := range report.Pages {
		regions += p.Regions
		kept += p.KeptText
		approximated += p.TextUnderTransparency + p.TransparentText
		jpeg += p.JPEG
		if p.Regions > 0 && (dpi == 0 || p.DPI < dpi) {
			dpi = p.DPI
		}
	}
	line := fmt.Sprintf(flattenedPrefix+" on %d page(s)", len(report.Pages))
	if regions > 0 {
		line += fmt.Sprintf(": %d region(s) composited into images at %.0f dpi, %d text object(s) kept as text", regions, dpi, kept)
		if jpeg > 0 {
			line += fmt.Sprintf(", %d photographic image(s) as JPEG", jpeg)
		}
	}
	if refused > 0 {
		line += fmt.Sprintf("; %d page(s) refused, their transparency set opaque", refused)
	}
	lines := []string{line}
	if approximated > 0 {
		lines = append(lines, fmt.Sprintf("flattening drew %d text object(s) opaque that painted with or under transparency", approximated))
	}
	return lines
}
