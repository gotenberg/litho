package mutations

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// SanitizeReport says what the document-wide passes cleared and what the
// verification found afterwards.
//
// The object numbers are what each pass rewrote, ascending. Scan is the state
// of the document the writer would produce, and is the half that decides
// whether the result may be written at all.
type SanitizeReport struct {
	// Annotations were rewritten by [ScrubAnnotationText].
	Annotations []int
	// FormFields were rewritten by [ScrubFormFieldValues].
	FormFields []int
	// OutlineItems were rewritten by [ScrubOutlineTitles].
	OutlineItems []int
	// Scan is the verification pass. It is filled in even when sanitization
	// fails closed, because naming where a token survived is the only way a
	// caller can act on it.
	Scan ScanResult
}

// SanitizeDocument clears the document-wide copies of text a redaction removed
// and then verifies that none of it survives, failing closed when it does.
//
// Clearing a page is not clearing a document. [RedactPageRegions] removes what a
// region covers and reports the runs it took; the same words go on living in
// annotation values, form fields and outline titles, each rooted at the catalog
// rather than at the page. This runs those passes over the whole file and then
// asks whether anything is left, which is what makes the result a claim rather
// than a hope. Pass [RedactReport.RemovedText] as tokens.
//
// Verification uses [ScanOutputForTokens], so it answers for the document a
// FULL REWRITE would write: every reference resolved through alloc, and
// unreferenced objects left out. That is the writer redaction requires. A caller
// writing an incremental update instead keeps every superseded object in the
// file and must ask [ScanForTokens] as well, which this deliberately does not
// do on its behalf.
//
// The two ways it fails are not the same, and they are treated differently:
//
//   - A pass REFUSES, because something could not be cleared exactly. Everything
//     queued is rolled back and the error is returned with an empty report. A
//     document scrubbed halfway is worse than one not scrubbed at all, since a
//     caller that handled the error would otherwise still write it.
//   - The passes succeed but VERIFICATION still finds the token. The edits are
//     correct and are left queued; the report names where the copy survives and
//     the error is [ErrSanitizationIncomplete]. Rolling back here would throw
//     away good work and hide the finding. The usual cause is a home no
//     token-driven pass reaches, such as /Info, which [ClearMetadata] owns.
//     A caller must not write the document until the scan comes back clean.
//
// No tokens is not a failure. A redaction that removed nothing leaves nothing to
// clear and nothing to verify, so the report comes back empty.
func SanitizeDocument(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	tokens []string,
) (SanitizeReport, error) {
	if len(tokens) == 0 {
		return SanitizeReport{}, nil
	}

	// One snapshot around all three passes. Each pass already restores its own
	// edits when it refuses, but that is not enough here: annotations can
	// succeed and outlines then refuse, leaving the annotation rewrites queued
	// behind an error the caller was told to treat as a failure.
	snap := alloc.Snapshot()

	var report SanitizeReport
	var err error

	if report.Annotations, err = ScrubAnnotationText(ctx, alloc, doc, tokens); err != nil {
		alloc.Restore(snap)
		return SanitizeReport{}, fmt.Errorf("scrubbing annotation text: %w", err)
	}
	if report.FormFields, err = ScrubFormFieldValues(ctx, alloc, doc, tokens); err != nil {
		alloc.Restore(snap)
		return SanitizeReport{}, fmt.Errorf("scrubbing form field values: %w", err)
	}
	if report.OutlineItems, err = ScrubOutlineTitles(ctx, alloc, doc, tokens); err != nil {
		alloc.Restore(snap)
		return SanitizeReport{}, fmt.Errorf("scrubbing outline titles: %w", err)
	}

	scan, err := ScanOutputForTokens(ctx, alloc, doc, tokens)
	if err != nil {
		alloc.Restore(snap)
		return SanitizeReport{}, fmt.Errorf("verifying the sanitized document: %w", err)
	}
	report.Scan = scan

	// Unreadable counts against it as much as a hit does. A stream the scan
	// could not decode is one it has not proved clean, and reporting the empty
	// hit list as success would be the same false negative the scan exists to
	// prevent.
	if !scan.Clean() {
		return report, fmt.Errorf("%w: %d surviving %s, %d object(s) unread",
			ErrSanitizationIncomplete, len(scan.Hits), hitWord(len(scan.Hits)), len(scan.Unreadable))
	}
	return report, nil
}

// hitWord keeps the refusal readable for the one-hit case.
func hitWord(n int) string {
	if n == 1 {
		return "copy"
	}
	return "copies"
}
