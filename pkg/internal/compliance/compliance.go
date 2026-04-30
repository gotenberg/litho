package compliance

import (
	"context"
	"fmt"
	"log/slog"

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
		slog.Error("compliance: unknown PDF/A profile", "profile", profile)
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
		slog.Error("compliance: unknown PDF/UA profile", "profile", profile)
		return nil, fmt.Errorf("%w: PDF/UA %s", ErrUnknownProfile, profile)
	}

	release := doc.LockValidationRun()
	defer release()

	return runValidation(ctx, doc, string(profile), groups)
}

// runValidation is the shared body of [ValidatePDFA] and [ValidatePDFUA].
// The caller must hold the document's validation run mutex.
func runValidation(ctx context.Context, doc *document.Document, profile string, groups []checkGroup) ([]error, error) {
	catalog, err := resolveCatalog(ctx, doc)
	if err != nil {
		slog.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	// Clear any residual format violations from a prior run, enable
	// parser-level format checking, resolve all objects upfront so the
	// checks run against a fully-parsed document graph, then disable
	// compliance mode again so concurrent reads after the run return
	// to the non-validation hot path.
	doc.ResetFormatViolations()
	doc.SetComplianceMode(true)
	defer doc.SetComplianceMode(false)

	resolveAllObjects(ctx, doc)

	violations := runCheckGroups(ctx, doc, catalog, profile, groups)

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
	// Rule is the VeraPDF validation rule that was violated.
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
