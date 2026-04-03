package compliance

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ValidatePDFA audits the document against a specific PDF/A profile, returning
// all compliance violations found as errors. Each error is a [Violation] value
// that can be type-asserted to access the structured [Rule] and Context fields.
// An empty slice means the document passes.
func ValidatePDFA(ctx context.Context, doc *document.Document, profile models.PDFAProfile) ([]error, error) {
	slog.Debug("compliance: starting PDF/A validation", "profile", profile)

	groups, ok := pdfaCheckGroups[profile]
	if !ok {
		slog.Error("compliance: unknown PDF/A profile", "profile", profile)
		return nil, fmt.Errorf("compliance: unknown PDF/A profile: %s", profile)
	}

	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		slog.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	// Enable parser-level format checking and resolve all objects upfront.
	doc.ComplianceMode = true
	resolveAllObjects(ctx, doc)

	violations := runCheckGroups(ctx, doc, catalog, string(profile), groups)

	slog.Debug("compliance: PDF/A validation complete",
		"profile", profile,
		"violations", len(violations),
	)
	return violationsToErrors(violations), nil
}

// ValidatePDFUA audits the document against a specific PDF/UA profile,
// returning all compliance violations found as errors. Each error is a
// [Violation] value that can be type-asserted to access the structured [Rule]
// and Context fields. An empty slice means the document passes.
func ValidatePDFUA(ctx context.Context, doc *document.Document, profile models.PDFUAProfile) ([]error, error) {
	slog.Debug("compliance: starting PDF/UA validation", "profile", profile)

	groups, ok := pdfuaCheckGroups[profile]
	if !ok {
		slog.Error("compliance: unknown PDF/UA profile", "profile", profile)
		return nil, fmt.Errorf("compliance: unknown PDF/UA profile: %s", profile)
	}

	catalog, err := getCatalog(ctx, doc)
	if err != nil {
		slog.Error("compliance: failed to get catalog", "error", err)
		return nil, fmt.Errorf("compliance: failed to get catalog: %w", err)
	}

	doc.ComplianceMode = true
	resolveAllObjects(ctx, doc)

	violations := runCheckGroups(ctx, doc, catalog, string(profile), groups)

	slog.Debug("compliance: PDF/UA validation complete",
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
