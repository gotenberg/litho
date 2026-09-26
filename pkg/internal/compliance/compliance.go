package compliance

import (
	"context"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ValidatePDFA audits the document against a specific PDF/A profile,
// returning all compliance violations found as errors. Each error is a
// [Violation] value that can be type-asserted to access the structured
// [Rule] and Context fields. An empty slice means the document passes.
//
// Concurrent calls to ValidatePDFA and [ValidatePDFUA] on the same
// [document.Document] are serialized through the document's validation
// run mutex: only one validator runs at a time so that the parser-level
// format-checking flag and the accumulated [document.Document.FormatViolations]
// slice are not corrupted by an overlapping second run.
func ValidatePDFA(ctx context.Context, doc *document.Document, profile models.PDFAProfile) ([]error, error) {
	doc.Logger.Debug("compliance: starting PDF/A validation", "profile", profile)

	groups, ok := pdfaCheckGroups[profile]
	if !ok {
		doc.Logger.Error("compliance: unknown PDF/A profile", "profile", profile)
		return nil, fmt.Errorf("%w: PDF/A %s", ErrUnknownProfile, profile)
	}

	release := doc.LockValidationRun()
	defer release()

	return runValidation(ctx, doc, string(profile), groups)
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found as errors. See [ValidatePDFA]
// for the concurrency contract.
func ValidatePDFUA(ctx context.Context, doc *document.Document, profile models.PDFUAProfile) ([]error, error) {
	doc.Logger.Debug("compliance: starting PDF/UA validation", "profile", profile)

	groups, ok := pdfuaCheckGroups[profile]
	if !ok {
		doc.Logger.Error("compliance: unknown PDF/UA profile", "profile", profile)
		return nil, fmt.Errorf("%w: PDF/UA %s", ErrUnknownProfile, profile)
	}

	release := doc.LockValidationRun()
	defer release()

	return runValidation(ctx, doc, string(profile), groups)
}

// runValidation is the shared body of [ValidatePDFA] and [ValidatePDFUA].
// The caller must hold the document's validation run mutex.
func runValidation(ctx context.Context, doc *document.Document, profile string, groups []checkGroup) ([]error, error) {
	violations, err := runValidationViolations(ctx, doc, profile, groups)
	if err != nil {
		return nil, err
	}
	return violationsToErrors(violations), nil
}

// runValidationViolations runs the checks and returns the violations
// themselves. [ValidateWTPDF] needs the structured form: it subtracts
// the rules its conformance level does not state before the violations
// become errors. The caller must hold the document's validation run
// mutex.
func runValidationViolations(ctx context.Context, doc *document.Document, profile string, groups []checkGroup) ([]Violation, error) {
	// Enable parser-level format checking, resolve all objects upfront so
	// the checks run against a fully-parsed document graph, then disable
	// compliance mode again so concurrent reads after the run return
	// to the non-validation hot path. Compliance mode is set BEFORE the
	// catalog is resolved, so an object this run parses first is checked
	// as it is parsed. The objects cached before the run began, which
	// pdf.Open resolves while opening, are checked by resolveAllObjects.
	//
	// The format violations recorded by a previous run are kept. They are
	// keyed by object number, so a second run re-resolving the same
	// objects adds nothing and reports the same set. Clearing them here
	// would make every run parse every cached object a second time to
	// record the same findings again.
	//
	// Every lookup of the run reads a reference to an object the file's
	// cross-reference table leaves out as a reference to an undefined object,
	// which ISO 32000-1, 7.3.10 makes null, as veraPDF does; see
	// document.OpenedObjectsOnly. The raw-byte augmentation that finds such an
	// object for text extraction is a leniency no conformance checker grants.
	// A rebuilt index has no table to leave an object out, and its lookups
	// still run the augmentation, so it runs here, before any check group
	// reads the index, rather than at whichever lookup misses first.
	ctx = document.OpenedObjectsOnly(ctx)
	if doc.Rebuilt {
		doc.AugmentXRef(ctx)
	}
	doc.SetComplianceMode(true)
	defer doc.SetComplianceMode(false)

	catalog, err := resolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	resolveAllObjects(ctx, doc)

	violations, err := runCheckGroups(ctx, doc, catalog, profile, groups)
	if err != nil {
		doc.Logger.Debug("compliance: check groups did not complete", "profile", profile, "error", err)
		return nil, err
	}

	// Systemic zero-Rule filter: dozens of per-part rule selectors
	// legitimately return Rule{} for parts whose profile drops the rule,
	// and any unguarded emitter serializes it as a clause-less "-t00"
	// residual. Per-site guards keep regressing (page bounds, limits,
	// colorants all shipped this bug), so drop the class centrally and
	// log so a slipped guard stays visible.
	violations = slices.DeleteFunc(violations, func(v Violation) bool {
		if v.Rule.Specification != "" {
			return false
		}
		doc.Logger.Debug("compliance: dropped zero-Rule violation", "context", v.Context)
		return true
	})

	doc.Logger.Debug("compliance: validation complete",
		"profile", profile,
		"violations", len(violations),
	)
	return violations, nil
}

// Violation records a single rule violation with context about where it occurred.
// It implements the error interface so callers receive typed violations.
//
//nolint:errname // Violation is the domain name; "ViolationError" would be redundant.
type Violation struct {
	// Rule is the conformance rule that was violated, identifying the
	// governing ISO clause, test number, and description.
	Rule Rule

	// Context describes where the violation occurred (e.g., page number,
	// object reference, font name).
	Context string
}

// Error implements the error interface.
func (r Violation) Error() string {
	if r.Context != "" {
		return fmt.Sprintf("clause %s test %d: %s [%s]", r.Rule.Clause, r.Rule.TestNumber, r.Rule.Description, r.Context)
	}
	return fmt.Sprintf("clause %s test %d: %s", r.Rule.Clause, r.Rule.TestNumber, r.Rule.Description)
}

// String returns a human-readable description of the violation.
func (r Violation) String() string {
	return r.Error()
}

// Severity classifications for [SeverityOf].
const (
	SeverityFatal   = "fatal"
	SeverityWarning = "warning"
)

// warningRules is the set of rules whose firing is a soft finding rather
// than a conformance failure: the collapsed Schematron warning bucket
// and the lenient-profile Factur-X tolerances surfaced under strict
// validation. Everything else, including all PDF/A and PDF/UA rules, is
// fatal under the binary conformance model.
//
//nolint:gochecknoglobals // immutable lookup set.
var warningRules = map[Rule]bool{
	Rule_cii_sch_Warning:               true,
	Rule_invoice_AFRelationshipNonSpec: true,
	Rule_invoice_StreamSubtypeNonSpec:  true,
	Rule_invoice_VersionNonSpec:        true,
}

// SeverityOf classifies a violation as fatal or warning. The pdf package
// maps this onto its public Severity enum at the boundary.
func SeverityOf(v Violation) string {
	if warningRules[v.Rule] {
		return SeverityWarning
	}
	return SeverityFatal
}
