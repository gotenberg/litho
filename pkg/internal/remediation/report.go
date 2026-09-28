package remediation

import "github.com/gotenberg/litho/pkg/internal/core/writer"

// Report describes what the remediation engine fixed and what it could not fix.
type Report struct {
	// Fixed lists human-readable descriptions of applied fixes.
	Fixed []string
	// Residual contains violations that could not be fixed automatically.
	// Each element is a compliance.Violation.
	Residual []error
	// NeedsFullRewrite is set when file structure violations require a full
	// rewrite to fix (clean object serialization, xref tables, headers).
	NeedsFullRewrite bool
	// TrailerDirty signals that a fixer mutated the document trailer
	// without allocating any objects. The writer's incremental fast path
	// skips writes when nothing is pending, so the caller must force an
	// incremental emit to persist the updated trailer.
	TrailerDirty bool
	// RequiredVersion overrides the PDF version in the output header when
	// the target profile requires a specific version (e.g., "2.0" for PDF/A-4).
	RequiredVersion string
	// ContentSnapshot captures allocator state after every fixer outside the
	// content tier ran and before the first content-modifying fixer did.
	// When nil, no content-tier fixer was applied and rollback is a no-op.
	// The writer restores it to revert exactly the content tier when a
	// post-remediation integrity probe regresses: every other repair stays
	// in the output, and in Fixed.
	ContentSnapshot *writer.Snapshot
	// ContentFixed lists descriptions produced by content-tier fixers. These
	// entries are the subset of Fixed that rollback discards.
	ContentFixed []string
	// ContentFixers names, in the order they ran, the content-tier fixers a
	// rollback to ContentSnapshot discards the repairs of. A writer that rolls
	// them back keeps them off the later rounds of the same remediation
	// ([Options.Disabled]).
	ContentFixers []string
	// FontRefusals lists the fonts left unembedded although [Options.Fonts]
	// was set, each with the reason, in object-number order.
	FontRefusals []FontRefusal
}
