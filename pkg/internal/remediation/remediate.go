package remediation

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// RemediatePDFA validates the document against the given PDF/A profile, applies
// targeted fixes for remediable violations, and returns a report.
func RemediatePDFA(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile models.PDFAProfile, opts Options) (*Report, error) {
	violations, err := compliance.ValidatePDFA(ctx, doc, profile)
	if err != nil {
		return nil, fmt.Errorf("validating for remediation: %w", err)
	}
	return remediate(ctx, doc, alloc, string(profile), opts, violations)
}

// RemediatePDFUA validates the document against the given PDF/UA profile, applies
// targeted fixes for remediable violations, and returns a report.
func RemediatePDFUA(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile models.PDFUAProfile, opts Options) (*Report, error) {
	violations, err := compliance.ValidatePDFUA(ctx, doc, profile)
	if err != nil {
		return nil, fmt.Errorf("validating for remediation: %w", err)
	}
	return remediate(ctx, doc, alloc, string(profile), opts, violations)
}

func remediate(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile string, opts Options, violations []error) (*Report, error) {
	if len(violations) == 0 {
		return &Report{}, nil
	}

	// Classify violations by fixer.
	typed := toViolations(violations)
	plan := classify(typed)

	// Execute fixers in registry order.
	var fixed []string
	handled := make(map[int]bool) // indices of handled violations

	for _, entry := range plan {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("remediation canceled: %w", err)
		}

		descriptions, err := entry.f.apply(ctx, alloc, doc, opts, entry.violations, profile)
		if err != nil {
			return nil, fmt.Errorf("fixer %s: %w", entry.f.name, err)
		}
		fixed = append(fixed, descriptions...)
		for _, idx := range entry.indices {
			handled[idx] = true
		}
	}

	// Collect residual violations (not handled by any fixer).
	var residual []error
	for i, v := range violations {
		if !handled[i] {
			residual = append(residual, v)
		}
	}

	return &Report{Fixed: fixed, Residual: residual}, nil
}

type planEntry struct {
	f          fixer
	violations []compliance.Violation
	indices    []int
}

func classify(violations []compliance.Violation) []planEntry {
	// For each fixer in registry order, collect matching violations.
	var plan []planEntry
	for _, f := range registry {
		var matched []compliance.Violation
		var indices []int
		for i, v := range violations {
			if f.matches(v) {
				matched = append(matched, v)
				indices = append(indices, i)
			}
		}
		if len(matched) > 0 {
			plan = append(plan, planEntry{f: f, violations: matched, indices: indices})
		}
	}
	return plan
}

func toViolations(errors []error) []compliance.Violation {
	var result []compliance.Violation
	for _, err := range errors {
		if v, ok := err.(compliance.Violation); ok {
			result = append(result, v)
		}
	}
	return result
}
