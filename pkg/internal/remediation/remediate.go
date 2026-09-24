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

// RemediateWTPDF validates the document against the given WTPDF conformance
// level, applies targeted fixes for remediable violations, and returns a
// report.
//
// The violations come from [compliance.ValidateWTPDF], not the PDF/UA-2
// validator: the levels drop rules PDF/UA-2 states, and judging the document
// against PDF/UA-2 here would report those as residuals the level never
// required.
func RemediateWTPDF(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile models.WTPDFProfile, opts Options) (*Report, error) {
	violations, err := compliance.ValidateWTPDF(ctx, doc, profile)
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
	// The version each profile requires of its output. Note the reach: this
	// value travels through Report.RequiredVersion into the writer's
	// full-rewrite config, and the INCREMENTAL path ignores it, because an
	// incremental update preserves the original bytes and cannot re-emit the
	// header. A profile whose remediation does not force a rewrite therefore
	// keeps the source's declared version whatever this says. Measured on a
	// %PDF-1.4 source: WTPDF-1.0-Reuse rewrites and comes out 2.0, while
	// PDF/UA-1, PDF/UA-2 and WTPDF-1.0-Accessibility stay incremental and
	// come out 1.4. Raising those would mean writing a catalog /Version
	// (ISO 32000-1, 7.5.2), which the values below cannot express: some are
	// CAPS (PDF/A-1, PDF/UA-1 pin a later source DOWN) and some are FLOORS.
	var requiredVersion string
	switch {
	case strings.HasPrefix(profile, "PDF/A-4"):
		requiredVersion = "2.0"
	case strings.HasPrefix(profile, "PDF/A-1"):
		requiredVersion = "1.4"
	case strings.HasPrefix(profile, "PDF/A-2"), strings.HasPrefix(profile, "PDF/A-3"):
		requiredVersion = "1.7"
	case strings.HasPrefix(profile, "PDF/UA-1"):
		// ISO 14289-1 §6.1 (PDFUA-1.xml 6.1 t1): the header shall be
		// %PDF-1.n with n in 0..7, so a PDF 2.0 source is pinned down
		// to 1.7, the highest conforming version.
		requiredVersion = "1.7"
	case strings.HasPrefix(profile, "PDF/UA-2"):
		// ISO 14289-2 builds on PDF 2.0, and litho says so itself:
		// compliance.Rule_14289_2_6_2_t01 is checked with checkVersionIs2,
		// which accepts only "2.n". Remediation writes PDF 2.0 structure
		// namespaces (/Namespaces, /NS) for this profile, so leaving the
		// source's version would declare a file that its own contents
		// contradict.
		requiredVersion = "2.0"
	case strings.HasPrefix(profile, "WTPDF"):
		// WTPDF 1.0 adopts the ISO 14289-2 requirements wholesale, differing
		// only by the rules it does not state and the Declaration it adds
		// (see compliance.ValidateWTPDF), so it carries PDF/UA-2's version
		// requirement too.
		//
		// A separate case rather than a PDF/UA-2 prefix: validation
		// normalises a WTPDF run to the PDF/UA-2 identifier, but
		// RemediateWTPDF passes the WTPDF level through verbatim, so a
		// PDF/UA-2 test would never match here.
		requiredVersion = "2.0"
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

	// requiredVersion above only reaches the output through the writer's
	// full-rewrite config. An incremental update preserves the original bytes
	// and cannot re-emit the header, so a profile needing a later version
	// than the source declares has one place left to say so: the catalog
	// /Version (ISO 32000-1, 7.5.2), which litho's own reader honors (see
	// document.initCatalogVersion).
	if !needsRewrite && catalogCarriesVersion(profile) && requiredVersion != "" {
		if raised := writer.AtLeastVersion(doc.Version, requiredVersion); raised != doc.Version {
			if declareCatalogVersion(ctx, alloc, doc, raised) {
				fixed = append(fixed, fmt.Sprintf("declared PDF version %s in the document catalog", raised))
			}
		}
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
