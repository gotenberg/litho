package pdf

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// RedactRegion names an area of one page whose content is to be removed.
//
// Rect is [llx, lly, urx, ury] in page space, the same shape and order as
// [models.Annotation.Rect] and an annotation /Rect in ISO 32000-1, Table 166.
// Page is 1-based, like every other page argument in this package.
// RedactRegion values are immutable and safe for concurrent use.
type RedactRegion struct {
	// Page is the 1-based page the region applies to.
	Page int
	// Rect is [llx, lly, urx, ury] in page space.
	Rect [4]float64
}

// RedactionHit names a place a redacted string still appears.
//
// It is reported rather than silently tolerated: a redaction that cannot
// account for every copy has not redacted the document, so these accompany
// [ErrRedactionIncomplete] to say where the survivor is.
// RedactionHit values are immutable and safe for concurrent use.
type RedactionHit struct {
	// ObjectNum is the object the copy was found in.
	ObjectNum int
	// Token is the string that survived.
	Token string
	// Where is "string" for a text string, or "stream" for decoded stream data.
	Where string
}

// RedactReport describes what a redaction removed, per vector.
//
// Removing content from a page does not remove the copies of it held elsewhere
// in the file, so the report covers both halves: what the pages lost, and what
// the document-wide passes cleared afterwards.
// Values must not be shared across goroutines without synchronization.
type RedactReport struct {
	// RemovedText holds the text of each show-text operator removed, one
	// string per operator, in the order the pages drew them.
	RemovedText []string
	// BlankedImages names the image XObjects whose covered samples were
	// overwritten.
	BlankedImages []string
	// RemovedPaths counts the painted paths removed.
	RemovedPaths int
	// RemovedAnnotations lists the object numbers of annotations dropped
	// because their rectangle sat inside a region, ascending.
	RemovedAnnotations []int
	// DroppedPageEntries names page entries dropped because they held a copy
	// of what was removed, such as /Thumb.
	DroppedPageEntries []string
	// FontsFullyRemovedOnPage maps a 1-based page number to the object
	// numbers of fonts whose every glyph on that page was removed.
	//
	// Reported rather than refused, and page-scoped on purpose: the font may
	// still be drawn elsewhere, and its embedded program carries the outlines
	// either way. litho cannot yet re-subset across the three embedded font
	// formats, so a caller that must also strip those outlines is told which
	// programs to deal with.
	FontsFullyRemovedOnPage map[int][]int
	// ScrubbedAnnotations, ScrubbedFormFields and ScrubbedOutlineItems list
	// the object numbers the document-wide passes rewrote, ascending.
	ScrubbedAnnotations  []int
	ScrubbedFormFields   []int
	ScrubbedOutlineItems []int
	// SurvivingCopies names every place a removed string was still found by
	// the verification scan. Non-empty only alongside
	// [ErrRedactionIncomplete].
	SurvivingCopies []RedactionHit
	// UnreadableObjects lists objects the verification scan could not decode.
	// A stream it could not read is one it has not proved clean, so these
	// fail the redaction as surely as a hit does.
	UnreadableObjects []int
}

// Redact removes what the given regions cover and then clears the copies of
// that text held elsewhere in the document, refusing rather than half-redacting.
//
// This is the cure for what [Reader.ExtractRedactedText] only diagnoses. Drawing
// a black rectangle over text leaves the bytes in the file and any extractor
// recovers them; true redaction is content removal plus document-wide
// sanitization. Selection is geometric and VISIBILITY-INDEPENDENT: a glyph
// counts when the box it paints meets a region, whatever its colour, its text
// rendering mode, or the optional-content group it sits in, because invisible
// text is exactly what a failed redaction leaves behind.
//
// It runs eagerly rather than queueing, because the caller must learn whether
// the document may be written at all before writing it. It forces a full
// rewrite: verification answers for the file a full rewrite would produce, and
// an incremental update would append the edits while leaving every superseded
// object, the original content stream among them, in the file.
//
// Two ways it refuses, both leaving the [Writer] exactly as it was:
//
//   - [ErrRedactionRefused] when something cannot be removed exactly: a region
//     covering an operator only partly, a straddling, stroked or clipping path,
//     content drawn by a form XObject, or an image whose samples cannot be
//     rewritten. Removing more than was asked, or reporting success over
//     content still in the file, are both worse than refusing.
//   - [ErrRedactionIncomplete] when the passes succeed but the verification
//     scan still finds a copy, or could not read an object. The report names
//     every survivor in [RedactReport.SurvivingCopies] so the caller can act on
//     it.
//
// Regions on several pages are applied together. Passing none is a no-op.
//
// What this does NOT do: re-subset embedded fonts whose glyphs now describe
// only removed text (see [RedactReport.FontsFullyRemovedOnPage]), and it cannot
// remove content a form XObject draws or an inline image's samples, which it
// refuses instead.
func (w *Writer) Redact(ctx context.Context, regions []RedactRegion) (_ *RedactReport, err error) {
	defer recoverToError(w.logger(), "Writer.Redact", &err)

	if w.consumed {
		return nil, ErrWriterConsumed
	}
	if len(regions) == 0 {
		return &RedactReport{}, nil
	}

	doc := w.reader.doc
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	byPage, order, err := groupRedactRegions(regions, len(pages))
	if err != nil {
		return nil, err
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())

	// Apply whatever an earlier stage queued, so the passes below read the
	// document as the caller has staged it rather than the untouched source.
	// The mutations stay queued: they are idempotent, and a refusal has to be
	// able to put the Writer back as it was.
	for i, m := range w.mutations {
		if applyErr := m.apply(ctx, alloc, doc); applyErr != nil {
			return nil, fmt.Errorf("applying staged mutation %d before redaction: %w", i, applyErr)
		}
	}

	// ClearMetadata empties /Info in place on the shared document, so a
	// refusal after it has run would otherwise persist on a Writer the caller
	// was told to treat as untouched.
	preTrailer := make(ast.PdfDict, len(doc.Index.Trailer))
	maps.Copy(preTrailer, doc.Index.Trailer)
	restoreTrailer := func() { doc.Index.Trailer = preTrailer }

	report := &RedactReport{}
	for _, pageNum := range order {
		pageReport, redactErr := mutations.RedactPageRegions(
			ctx, alloc, doc, pages[pageNum-1], byPage[pageNum])
		if redactErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("%w: page %d: %w", ErrRedactionRefused, pageNum, redactErr)
		}
		mergePageRedaction(report, pageNum, pageReport)
	}

	// Nothing was selected, so there is nothing to sanitize and nothing to
	// write. Leaving the Writer untouched keeps a redaction that removed
	// nothing from forcing a rewrite of the whole file.
	if len(report.RemovedText) == 0 && len(alloc.Pending()) == 0 {
		restoreTrailer()
		return report, nil
	}

	// Metadata needs no scrubber of its own: ClearMetadata already empties
	// /Info and drops the catalog's XMP. Composing it here is what the
	// verification below depends on, since /Info is the usual home no
	// token-driven pass reaches.
	if metaErr := mutations.ClearMetadata(ctx, alloc, doc); metaErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: clearing metadata: %w", ErrRedactionRefused, metaErr)
	}

	sanitized, sanErr := mutations.SanitizeDocument(ctx, alloc, doc, report.RemovedText)
	report.ScrubbedAnnotations = sanitized.Annotations
	report.ScrubbedFormFields = sanitized.FormFields
	report.ScrubbedOutlineItems = sanitized.OutlineItems
	for _, hit := range sanitized.Scan.Hits {
		report.SurvivingCopies = append(report.SurvivingCopies,
			RedactionHit{ObjectNum: hit.ObjectNum, Token: hit.Token, Where: hit.Where})
	}
	report.UnreadableObjects = sanitized.Scan.Unreadable

	if sanErr != nil {
		// Fail closed at the boundary. SanitizeDocument leaves correct edits
		// queued so a survivor can be named, but a caller that ignored the
		// error would then write a half-sanitized document, so the Writer is
		// restored here and the report carries the diagnosis instead.
		restoreTrailer()
		return report, fmt.Errorf("%w: %w", ErrRedactionIncomplete, sanErr)
	}

	// Seed via rewriteOverrides only. Queueing a mutation that replayed the
	// pending map worked incrementally, where source and destination numbering
	// agree, but stomped the copier's destination allocations during a full
	// rewrite and produced files whose trailer pointed at the wrong object.
	w.rewriteOverrides = alloc.Pending()
	w.forceRewrite = true
	return report, nil
}

// groupRedactRegions collects the regions of each page, and returns the pages
// in ascending order so the same input always drives the same sequence of
// edits.
func groupRedactRegions(regions []RedactRegion, pageCount int) (map[int][][4]float64, []int, error) {
	byPage := make(map[int][][4]float64, len(regions))
	for _, region := range regions {
		if region.Page < 1 || region.Page > pageCount {
			return nil, nil, fmt.Errorf("%w: page %d of %d", ErrInvalidPageNumber, region.Page, pageCount)
		}
		byPage[region.Page] = append(byPage[region.Page], region.Rect)
	}

	order := make([]int, 0, len(byPage))
	for pageNum := range byPage {
		order = append(order, pageNum)
	}
	sort.Ints(order)
	return byPage, order, nil
}

// mergePageRedaction folds one page's result into the document-wide report.
func mergePageRedaction(out *RedactReport, pageNum int, in mutations.RedactReport) {
	out.RemovedText = append(out.RemovedText, in.RemovedText...)
	out.BlankedImages = append(out.BlankedImages, in.BlankedImages...)
	out.RemovedPaths += in.RemovedPaths
	out.RemovedAnnotations = append(out.RemovedAnnotations, in.RemovedAnnotations...)
	out.DroppedPageEntries = append(out.DroppedPageEntries, in.DroppedPageEntries...)
	if len(in.FontsFullyRemovedOnPage) > 0 {
		if out.FontsFullyRemovedOnPage == nil {
			out.FontsFullyRemovedOnPage = make(map[int][]int)
		}
		out.FontsFullyRemovedOnPage[pageNum] = in.FontsFullyRemovedOnPage
	}
}
