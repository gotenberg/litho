package models

// RemediationReport describes what the remediation engine fixed and what it
// could not fix automatically.
// Concurrent reads of a value are safe; a write needs synchronization.
type RemediationReport struct {
	// Fixed lists human-readable descriptions of applied fixes that were
	// kept in the output.
	Fixed []string
	// Residual contains violations that could not be fixed automatically,
	// in the flat error form the Validate methods return.
	// [RemediationReport.ResidualViolations] returns the same findings as
	// structured [Violation] values. If content-modifying fixers were rolled
	// back, the violations they had claimed to handle move back here.
	//
	// A remediation given [github.com/gotenberg/litho/pkg/pdf.WithPDFUA] lists
	// the violations of both its profiles here, each [Violation] naming its
	// standard in Specification.
	//
	// A Factur-X remediation or repair lists the failures a validation of its
	// output reports, [FacturXReport.Fatals]: a warning does not fail
	// conformance and is not listed, so an empty Residual means the output
	// validates.
	//
	// A non-empty Residual does not stop the output from ASSERTING the
	// profile. The identification schemas are themselves requirements
	// (ISO 19005-1, 6.7.11 and ISO 14289-1, clause 5), so remediation writes
	// pdfaid or pdfuaid to satisfy them, and a document still failing other
	// clauses carries that claim regardless: a PDF/A-2b run leaving seven
	// font-embedding violations emits pdfaid:part 2 and pdfaid:conformance B
	// all the same. Read this field rather than trusting what the document
	// says about itself.
	Residual []error
	// RolledBack lists human-readable descriptions of content-modifying fixes
	// that were applied, failed the post-remediation integrity probe, and were
	// subsequently discarded. When non-empty, the output preserves rendering
	// at the cost of leaving the listed violations unfixed. The flattening of
	// a PDF/A-1 remediation is the exception: discarded, it gives way to
	// setting the transparency opaque, which fixes the same violations and
	// changes how the page draws (see
	// [github.com/gotenberg/litho/pkg/pdf.Writer.RemediatePDFA]).
	RolledBack []string
	// FontRefusals lists, in object-number order, the fonts the output shows
	// unembedded although
	// [github.com/gotenberg/litho/pkg/pdf.WithFontProvider] or
	// [github.com/gotenberg/litho/pkg/pdf.WithFontFiles] was given, each with
	// the reason. It is nil without a provider. It is the verdict of the last
	// pass that judged the fonts: a font one round refused and a later round
	// embedded is not listed, and one every round refused is listed once, with
	// the last round's reason.
	FontRefusals []FontRefusal
}

// ResidualViolations returns the residual findings as structured
// [Violation] values with clause, test number, context, and severity.
// Residual keeps the raw error form; this accessor is the structured view
// for callers that route, filter, or report findings programmatically.
func (r *RemediationReport) ResidualViolations() []Violation {
	return toViolations(r.Residual)
}

// AutoTagReport says what a
// [github.com/gotenberg/litho/pkg/pdf.Writer.AutoTag] did to the document.
//
// AutoTag is queued like every other edit, so the Writer fills the report when
// it applies the edit: during
// [github.com/gotenberg/litho/pkg/pdf.Writer.WriteTo], or during a remediation
// or a [github.com/gotenberg/litho/pkg/pdf.Writer.Redact] queued after
// AutoTag, since those apply the edits queued before them. Until then Applied
// is false and the other fields are zero.
//
// Values must not be shared across goroutines without synchronization: the
// Writer fills one on the goroutine that calls the method applying the edit, so
// read it after that call returns.
type AutoTagReport struct {
	// Applied reports whether the Writer applied the edit. It stays false when
	// the call that would apply it failed before reaching it, and when the
	// Writer refused AutoTag after a remediation (see
	// [github.com/gotenberg/litho/pkg/pdf.ErrEditAfterRemediation]). It says
	// the edit ran, not that the output was written: the applying call's error
	// says that.
	Applied bool
	// Skipped is nil when AutoTag built a structure tree. Otherwise it says
	// why AutoTag left the document as it was, and wraps one of
	// [github.com/gotenberg/litho/pkg/pdf.ErrAutoTagAlreadyTagged],
	// [github.com/gotenberg/litho/pkg/pdf.ErrAutoTagMarkedContent],
	// [github.com/gotenberg/litho/pkg/pdf.ErrAutoTagUnreadableContent] and
	// [github.com/gotenberg/litho/pkg/pdf.ErrAutoTagNothingToTag]: test it
	// with [errors.Is]. A skip fails nothing, and the output is written
	// without a new tree.
	Skipped error
	// Page is the 1-based page Skipped concerns, counted among the pages the
	// Writer holds when AutoTag is queued, 0 when it concerns the whole
	// document or AutoTag skipped nothing.
	Page int
}
