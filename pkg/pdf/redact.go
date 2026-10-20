package pdf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
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
	// Where is "string" for a string, "stream" for a stream's data, decoded or
	// as stored, or "name" for a destination name.
	Where string
	// Pages lists the 1-based pages whose content draws the object, ascending.
	// It is set only on [RedactReport.CopiesOnOtherPages].
	Pages []int
}

// RedactOption configures [Writer.Redact].
type RedactOption func(*redactOpts)

// redactOpts holds what [Writer.Redact] and its [RedactOption] values set.
type redactOpts struct {
	strict bool
}

// WithStrictRedaction makes [Writer.Redact] refuse with
// [ErrRedactionIncomplete] when a removed string is still drawn by a page the
// regions do not cover, instead of reporting it in
// [RedactReport.CopiesOnOtherPages]. Use it when the regions select every
// occurrence of what must go, so a copy elsewhere means the selection missed
// one.
func WithStrictRedaction() RedactOption {
	return func(o *redactOpts) { o.strict = true }
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
	// CopiesOnOtherPages names the removed strings the content of pages
	// outside the regions still draws, each with the pages that draw it. These
	// are warnings: the redaction succeeded, and the output keeps them. Empty
	// under [WithStrictRedaction], which refuses them instead.
	CopiesOnOtherPages []RedactionHit
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
//     scan still finds a copy outside the content of other pages (see below),
//     or could not read an object. The report names
//     every survivor in [RedactReport.SurvivingCopies] so the caller can act on
//     it. The scan looks for each removed run holding at least three letters
//     or digits, and for each such word drawn across shorter runs, as a
//     producer that positions every glyph draws one. A run too short to
//     identify anything, a lone space, letter or digit, is not looked for: it
//     survives in nearly every document whatever was removed.
//
// A copy that only pages outside the regions draw does not fail the redaction.
// A region redaction removes what the region covers on its own page, as a
// Redact annotation applies to the page it sits on (ISO 32000-2, 12.5.6.23),
// and the same text drawn on another page is content the caller did not
// select. Such a copy, in a content stream, a form XObject, a tiling pattern,
// a Type 3 glyph or an annotation appearance that another page draws and no
// redacted page draws or names, is reported in [RedactReport.CopiesOnOtherPages]
// with the pages that draw it, and the output keeps it. A copy anywhere else,
// the redacted page's own resources and annotations, the structure tree, the
// metadata or any other object, still refuses. [WithStrictRedaction] refuses
// copies on other pages too.
//
// Regions on several pages are applied together. Passing none is a no-op.
//
// Everything outside the regions stays where it was. Text after a removed run
// on the same line keeps its position, because the run's advance is put back
// in its place. An image is blanked by rewriting it, so an image also painted
// somewhere the regions do not cover the same samples, on another page, twice
// on one page, or by an annotation or a form, is refused rather than blanked
// there too.
//
// The redaction holds whatever else the Writer does before or after it:
// another Redact, auto-tagging, and a remediation, which comes after it since a
// remediation is the last edit on a Writer (a Redact after one returns
// [ErrEditAfterRemediation]). Each builds on the others' edits,
// and [Writer.WriteTo] scans the output it is about to write for every removed
// string. A stage can still rebuild a page from the source document
// (auto-tagging marks up the original content stream), so when a removed
// string is back, WriteTo applies the regions again on top of everything else
// and scans once more. If a copy still survives, WriteTo refuses with
// [ErrRedactionIncomplete] and writes nothing.
//
// What this does NOT do: re-subset embedded fonts whose glyphs now describe
// only removed text (see [RedactReport.FontsFullyRemovedOnPage]), and it cannot
// remove content a form XObject draws or an inline image's samples, which it
// refuses instead.
func (w *Writer) Redact(ctx context.Context, regions []RedactRegion, opts ...RedactOption) (_ *RedactReport, err error) {
	defer recoverToError(w.logger(), "Writer.Redact", &err)

	if w.closed {
		return nil, ErrWriterClosed
	}
	if w.consumed {
		return nil, ErrWriterConsumed
	}
	if err := w.refuseAfterRemediation("Redact"); err != nil {
		return nil, err
	}
	if len(regions) == 0 {
		return &RedactReport{}, nil
	}
	var o redactOpts
	for _, opt := range opts {
		opt(&o)
	}

	if err := w.promotePageTree(ctx); err != nil {
		return nil, err
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

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Start from what earlier stages produced, remediation or an earlier
	// Redact, and replay whatever an earlier stage queued, so the passes below
	// read the document as the caller has staged it rather than the untouched
	// source. Starting empty replaced those stages' work: a second Redact
	// dropped the first, and a Redact after remediation undid its fixes. The
	// mutations the overrides already hold are not applied again, and the rest
	// stay queued, marked folded once the overrides hold them (see
	// [Writer.folded]): a refusal has to be able to put the Writer back as it
	// was.
	seedAllocator(alloc, w.rewriteOverrides)
	for i, m := range w.unfoldedMutations() {
		if applyErr := m.apply(ctx, alloc, doc); applyErr != nil {
			return nil, fmt.Errorf("applying staged mutation %d before redaction: %w", w.folded+i, applyErr)
		}
	}
	// What the passes below queue is told apart from what the stages above
	// staged, which is pending too.
	staged := alloc.Writes()

	// ClearMetadata empties /Info in place on the document the Writer edits,
	// so a refusal after it has run would otherwise persist on a Writer the
	// caller was told to treat as untouched.
	preTrailer := make(ast.PdfDict, len(doc.Index.Trailer))
	maps.Copy(preTrailer, doc.Index.Trailer)
	restoreTrailer := func() { doc.Index.Trailer = preTrailer }

	report := &RedactReport{}
	var blanked []int
	for _, pageNum := range order {
		pageReport, redactErr := mutations.RedactPageRegions(
			ctx, alloc, doc, pages[pageNum-1], byPage[pageNum])
		if redactErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("%w: page %d: %w", ErrRedactionRefused, pageNum, redactErr)
		}
		mergePageRedaction(report, pageNum, pageReport)
		blanked = appendNewObjects(blanked, pageReport.BlankedImageObjects)
	}

	// Blanking rewrites an image in place, so it is exact only when every
	// place that paints the image had the same samples covered. That needs
	// every page's regions at once, which is why it runs here rather than per
	// page.
	if useErr := mutations.CheckBlankedImageUses(ctx, alloc, doc, pages, byPage, blanked); useErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
	}
	// An annotation is one object however many pages list it, so removing it
	// from one page is exact only when no other page still shows it. That
	// also needs every page's regions applied first.
	if useErr := mutations.CheckRemovedAnnotationUses(ctx, alloc, doc, pages, report.RemovedAnnotations); useErr != nil {
		restoreTrailer()
		return nil, fmt.Errorf("%w: %w", ErrRedactionRefused, useErr)
	}

	// Nothing was selected, so there is nothing to sanitize and nothing to
	// write. Leaving the Writer untouched keeps a redaction that removed
	// nothing from forcing a rewrite of the whole file.
	//
	// The passes queued nothing when the allocator took no write after the
	// staged edits. Testing for an empty pending map instead read every staged
	// edit as a removal: a redaction that selected nothing after a remediation
	// went on to clear the metadata, and the output lost the XMP packet ISO
	// 19005-2, 6.6.2.1 requires.
	if len(report.RemovedText) == 0 && alloc.Writes() == staged {
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

	// The scrubs and the scan look for the removed runs that can identify
	// what was removed, not for every run: a lone space or digit survives in
	// nearly every document, and refusing over it refuses over nothing (see
	// mutations.RedactionNeedles).
	needles := mutations.RedactionNeedles(report.RemovedText)
	sanitized, sanErr := mutations.SanitizeDocument(ctx, alloc, doc, needles)
	report.ScrubbedAnnotations = sanitized.Annotations
	report.ScrubbedFormFields = sanitized.FormFields
	report.ScrubbedOutlineItems = sanitized.OutlineItems
	for _, hit := range sanitized.Scan.Hits {
		report.SurvivingCopies = append(report.SurvivingCopies,
			RedactionHit{ObjectNum: hit.ObjectNum, Token: hit.Token, Where: hit.Where})
	}
	report.UnreadableObjects = sanitized.Scan.Unreadable

	if sanErr != nil && !o.strict && errors.Is(sanErr, mutations.ErrSanitizationIncomplete) {
		copies, onlyOtherPages, splitErr := w.otherPageCopies(ctx, alloc, pages, byPage, sanitized.Scan)
		if splitErr != nil {
			restoreTrailer()
			return nil, fmt.Errorf("telling copies on other pages apart: %w", splitErr)
		}
		if onlyOtherPages {
			report.SurvivingCopies = nil
			report.CopiesOnOtherPages = copies
			sanErr = nil
		}
	}

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
	w.folded = len(w.mutations)
	w.forceRewrite = true
	w.recordRedaction(regions, needles, o.strict)
	return report, nil
}

// otherPageCopies reports whether every hit of scan is a copy only pages
// outside byPage draw, and names those copies when it is. An object the scan
// could not read is not proved to be one, so any makes it false.
func (w *Writer) otherPageCopies(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pages []*document.Page,
	byPage map[int][][4]float64,
	scan mutations.ScanResult,
) ([]RedactionHit, bool, error) {
	if len(scan.Unreadable) > 0 || len(scan.Hits) == 0 {
		return nil, false, nil
	}
	redacted := make(map[int]bool, len(byPage))
	for pageNum := range byPage {
		redacted[pageNum] = true
	}
	copies, rest, err := mutations.SplitOtherPageCopies(ctx, alloc, w.reader.doc, pages, redacted, scan.Hits)
	if err != nil {
		return nil, false, err
	}
	if len(rest) > 0 {
		return nil, false, nil
	}
	out := make([]RedactionHit, 0, len(copies))
	for _, c := range copies {
		out = append(out, RedactionHit{ObjectNum: c.Hit.ObjectNum, Token: c.Hit.Token, Where: c.Hit.Where, Pages: c.Pages})
	}
	return out, true, nil
}

// seedAllocator queues every override at its own object number, so a stage
// allocating on top of them builds on the work an earlier stage produced.
// Object numbers are visited in order so the same Writer state always drives
// the same sequence of edits.
func seedAllocator(alloc *writer.ObjAllocator, overrides map[int]ast.PdfObject) {
	for _, objNum := range slices.Sorted(maps.Keys(overrides)) {
		alloc.AllocAt(objNum, overrides[objNum])
	}
}

// recordRedaction remembers a successful redaction so [Writer.WriteTo] can
// check the composed output against it. needles are the removed runs the
// output is verified against (see mutations.RedactionNeedles).
//
// A redaction that tolerated copies on other pages also records the pages it
// covered and its needles, so WriteTo tolerates the same copies and no others.
func (w *Writer) recordRedaction(regions []RedactRegion, needles []string, strict bool) {
	w.redactions = append(w.redactions, slices.Clone(regions))
	w.redactedTokens = appendNewTokens(w.redactedTokens, needles)
	if strict {
		return
	}
	tolerance := redactionTolerance{pages: make(map[int]bool, len(regions)), tokens: make(map[string]bool, len(needles))}
	for _, region := range regions {
		tolerance.pages[region.Page] = true
	}
	for _, tok := range needles {
		tolerance.tokens[tok] = true
	}
	w.redactionTolerances = append(w.redactionTolerances, tolerance)
}

// redactionTolerance is what one non-strict [Writer.Redact] call lets survive:
// a copy of one of its tokens that only pages outside its pages draw.
type redactionTolerance struct {
	pages  map[int]bool
	tokens map[string]bool
}

// untolerated drops from scan the hits some recorded redaction tolerates (see
// [redactionTolerance]), and returns what is left.
func (w *Writer) untolerated(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	pages []*document.Page,
	scan mutations.ScanResult,
) (mutations.ScanResult, error) {
	rest := scan.Hits
	for _, tolerance := range w.redactionTolerances {
		var mine, others []mutations.TokenHit
		for _, hit := range rest {
			if tolerance.tokens[hit.Token] {
				mine = append(mine, hit)
			} else {
				others = append(others, hit)
			}
		}
		if len(mine) == 0 {
			continue
		}
		_, kept, err := mutations.SplitOtherPageCopies(ctx, alloc, w.reader.doc, pages, tolerance.pages, mine)
		if err != nil {
			return mutations.ScanResult{}, err
		}
		others = append(others, kept...)
		rest = others
	}
	return mutations.ScanResult{Hits: rest, Unreadable: scan.Unreadable}, nil
}

// appendNewObjects appends each object number of add that objs does not hold
// yet.
func appendNewObjects(objs, add []int) []int {
	for _, objNum := range add {
		if !slices.Contains(objs, objNum) {
			objs = append(objs, objNum)
		}
	}
	return objs
}

// appendNewTokens appends each token of add that tokens does not hold yet.
func appendNewTokens(tokens, add []string) []string {
	for _, tok := range add {
		if !slices.Contains(tokens, tok) {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

// enforceRedactions checks the output [Writer.WriteTo] is about to write
// against every recorded redaction, and returns the overrides to write.
//
// A redaction lives in the rewrite overrides, but a stage can still rebuild a
// page from the source document around it: auto-tagging marks up the original
// content stream, whether queued before or after Redact or run as a
// remediation fixer, and its /Contents then carries the text Redact removed.
// So the composed output is scanned for every removed token. When one is
// back, the recorded regions are applied again on top of the composed state
// and the output is sanitized and scanned once more. A copy that survives that
// is refused with [ErrRedactionIncomplete], and nothing is written: reporting
// success over content still in the file is the failure redaction exists to
// prevent.
func (w *Writer) enforceRedactions(ctx context.Context, overrides map[int]ast.PdfObject) (map[int]ast.PdfObject, error) {
	if len(w.redactedTokens) == 0 {
		return overrides, nil
	}
	doc := w.reader.doc
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	seedAllocator(alloc, overrides)

	scan, err := mutations.ScanOutputForTokens(ctx, alloc, doc, w.redactedTokens)
	if err != nil {
		return nil, fmt.Errorf("%w: scanning the composed output: %w", ErrRedactionIncomplete, err)
	}
	if scan.Clean() {
		return overrides, nil
	}
	pages, err := w.reader.resolvePages(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}
	if scan, err = w.untolerated(ctx, alloc, pages, scan); err != nil {
		return nil, fmt.Errorf("%w: telling copies on other pages apart: %w", ErrRedactionIncomplete, err)
	}
	if scan.Clean() {
		return overrides, nil
	}
	doc.Logger.Debug("writer: a later stage restored redacted content, re-applying the redactions",
		slog.Int("hits", len(scan.Hits)), slog.Int("unreadable", len(scan.Unreadable)))

	tokens := slices.Clone(w.redactedTokens)
	for _, regions := range w.redactions {
		byPage, order, err := groupRedactRegions(regions, len(pages))
		if err != nil {
			return nil, err
		}
		for _, pageNum := range order {
			pageReport, redactErr := mutations.RedactPageRegions(ctx, alloc, doc, pages[pageNum-1], byPage[pageNum])
			if redactErr != nil {
				return nil, fmt.Errorf("%w: re-applying page %d at write time: %w", ErrRedactionRefused, pageNum, redactErr)
			}
			tokens = appendNewTokens(tokens, mutations.RedactionNeedles(pageReport.RemovedText))
		}
	}
	sanitized, sanErr := mutations.SanitizeDocument(ctx, alloc, doc, tokens)
	if sanErr != nil && errors.Is(sanErr, mutations.ErrSanitizationIncomplete) {
		// The copies Redact reported on other pages are still there, and still
		// tolerated: only what remains past them refuses.
		left, splitErr := w.untolerated(ctx, alloc, pages, sanitized.Scan)
		if splitErr != nil {
			return nil, fmt.Errorf("%w: telling copies on other pages apart: %w", ErrRedactionIncomplete, splitErr)
		}
		if left.Clean() {
			sanErr = nil
		}
	}
	if sanErr != nil {
		return nil, fmt.Errorf("%w: %w", ErrRedactionIncomplete, sanErr)
	}
	return alloc.Pending(), nil
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
