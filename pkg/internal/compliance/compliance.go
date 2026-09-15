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
// Concurrent calls to ValidatePDFA and ValidatePDFUA on the same
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
	// Enable parser-level format checking, resolve all objects upfront so
	// the checks run against a fully-parsed document graph, then disable
	// compliance mode again so concurrent reads after the run return
	// to the non-validation hot path. Compliance mode is set BEFORE the
	// catalog is resolved: an object first parsed while resolving the
	// catalog or page tree would otherwise be cached without its header
	// being format-checked, so a later §6.1.8/§6.1.9 defect on that object
	// would be reported only when a run happened to parse it after the flag
	// flipped, an order-dependent (nondeterministic) result.
	//
	// The format violations recorded by a previous run are kept. They are
	// keyed by object number, so a second run re-resolving the same
	// objects adds nothing and reports the same set. Clearing them here
	// would instead hand the second run only what it re-parsed: the
	// objects cached by the first run are never parsed again, so their
	// violations would disappear.
	doc.SetComplianceMode(true)
	defer doc.SetComplianceMode(false)

	catalog, err := resolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	resolveAllObjects(ctx, doc)

	violations := runCheckGroups(ctx, doc, catalog, profile, groups)

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
	return violationsToErrors(violations), nil
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
