package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
		doc.Logger.Debug("remediate: no violations",
			slog.String("profile", profile),
		)
		return &Report{}, nil
	}

	// Classify violations by fixer.
	typed := toViolations(violations)
	plan := classify(typed)

	doc.Logger.Debug("remediate started",
		slog.String("profile", profile),
		slog.Int("violations", len(violations)),
		slog.Int("plan_entries", len(plan)),
	)

	// Snapshot the original trailer size to detect mutations that
	// don't hit the allocator (e.g. a /ID insertion). The incremental
	// writer's fast-path skips writes when nothing is pending, so
	// without this check trailer-only fixes would silently be lost.
	origTrailerSize := len(doc.Index.Trailer)

	// Execute fixers in registry order.
	var fixed []string
	var needsRewrite bool
	handled := make(map[int]bool) // indices of handled violations

	// Snapshot of the allocator taken right before the first content-tier
	// fixer runs. It lets the writer roll back only the content tier when
	// the post-remediation integrity probe detects a regression.
	var contentSnapshot *writer.Snapshot
	var contentFixed []string
	contentHandled := make(map[int]bool)

	for _, entry := range plan {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("remediation canceled: %w", err)
		}

		if entry.f.safety == safetyContent && contentSnapshot == nil {
			snap := alloc.Snapshot()
			contentSnapshot = &snap
		}

		doc.Logger.Debug("fixer starting",
			slog.String("fixer", entry.f.name),
			slog.String("safety", entry.f.safety.String()),
			slog.Int("violations", len(entry.violations)),
		)
		descriptions, err := entry.f.apply(ctx, alloc, doc, opts, entry.violations, profile)
		if err != nil {
			doc.Logger.Debug("fixer failed",
				slog.String("fixer", entry.f.name),
				slog.Any("err", err),
			)
			return nil, fmt.Errorf("fixer %s: %w", entry.f.name, err)
		}
		doc.Logger.Debug("fixer completed",
			slog.String("fixer", entry.f.name),
			slog.Int("descriptions", len(descriptions)),
		)
		fixed = append(fixed, descriptions...)
		if entry.f.safety == safetyContent {
			contentFixed = append(contentFixed, descriptions...)
		}
		for _, idx := range entry.indices {
			handled[idx] = true
			if entry.f.safety == safetyContent {
				contentHandled[idx] = true
			}
		}
		if entry.f.name == "file-structure" && len(descriptions) > 0 {
			needsRewrite = true
		}
	}

	var contentResidual []error
	for i, v := range violations {
		if contentHandled[i] {
			contentResidual = append(contentResidual, v)
		}
	}

	// Collect unmatched violations. The accurate residual list is computed
	// by Writer.remediate after serializing the output and re-validating.
	// This preliminary list is used as a fallback.
	var residual []error
	for i, v := range violations {
		if !handled[i] {
			residual = append(residual, v)
		}
	}

	// Pin the output PDF version to the value required by each PDF/A
	// family: part 1 → 1.4, parts 2 and 3 → 1.7, part 4 → 2.0. The
	// writer honours this in both full-rewrite and incremental output.
	var requiredVersion string
	switch {
	case strings.HasPrefix(profile, "PDF/A-4"):
		requiredVersion = "2.0"
	case strings.HasPrefix(profile, "PDF/A-1"):
		requiredVersion = "1.4"
	case strings.HasPrefix(profile, "PDF/A-2"), strings.HasPrefix(profile, "PDF/A-3"):
		requiredVersion = "1.7"
	}

	// Trailer mutations (e.g. /ID insertion) don't allocate objects but
	// still have to be serialized. Incremental writes that ADD keys are
	// covered by an extra xref section. A SHRINK (e.g. removing /Info
	// for PDF/A-4 §6.1.3) can only stick through a full rewrite because
	// earlier trailers inherit absent keys via /Prev, so escalate in
	// that case.
	trailerDirty := len(doc.Index.Trailer) != origTrailerSize
	if len(doc.Index.Trailer) < origTrailerSize {
		needsRewrite = true
	}

	doc.Logger.Debug("remediate completed",
		slog.String("profile", profile),
		slog.Int("fixed", len(fixed)),
		slog.Int("residual", len(residual)),
		slog.Int("content_fixed", len(contentFixed)),
		slog.Bool("content_snapshot", contentSnapshot != nil),
		slog.Bool("needs_rewrite", needsRewrite),
		slog.Bool("trailer_dirty", trailerDirty),
		slog.String("required_version", requiredVersion),
	)
	return &Report{
		Fixed:            fixed,
		Residual:         residual,
		NeedsFullRewrite: needsRewrite,
		TrailerDirty:     trailerDirty,
		RequiredVersion:  requiredVersion,
		ContentSnapshot:  contentSnapshot,
		ContentFixed:     contentFixed,
		ContentResidual:  contentResidual,
	}, nil
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
