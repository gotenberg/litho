package remediation

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// wholeStreams returns ctx for a remediation run, under which its validation
// and its repairs read every content stream whole
// ([content.WithWholeStreams]).
//
// Validation judges a content stream as far as veraPDF reads it: to a stray
// "]", and not at all when it holds an array or a dictionary veraPDF cannot
// read. A viewer draws the operators past those points all the same, and a
// repair that rewrites such a stream drops what ended veraPDF's reading, after
// which veraPDF judges every operator it holds. Read whole, the stream's
// defects reach the repairs that answer them. What remains is judged on the
// output as validation reads it.
//
// # Corpus evidence
//
//   - pdfjs bug1130815.pdf: read as validation reads it, its damaged page
//     held no finding, and its PDF/A-1b and PDF/A-2b remediations dropped the
//     removal of 735 undefined operators, the repair of 854 malformed numbers
//     and the q/Q flattening. Read whole, they write the files they wrote
//     before, which veraPDF 1.30 opens and judges as litho does.
//   - pdfjs bug1130815.pdf, issue11678.pdf, operator-in-TJ-array.pdf and
//     poppler-90-0-fuzzed.pdf: the outputs of their PDF/A-1b and PDF/A-2b
//     remediations, default and aggressive, fail the same veraPDF 1.30 rules
//     as before, and operator-in-TJ-array.pdf no longer reports as a residual
//     the font veraPDF does not judge.
func wholeStreams(ctx context.Context) context.Context {
	return content.WithWholeStreams(ctx)
}

// RemediatePDFA validates the document against the given PDF/A profile, applies
// targeted fixes for remediable violations, and returns a report.
func RemediatePDFA(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile models.PDFAProfile, opts Options) (*Report, error) {
	ctx = wholeStreams(ctx)
	violations, err := compliance.ValidatePDFA(ctx, doc, profile)
	if err != nil {
		return nil, fmt.Errorf("validating for remediation: %w", err)
	}
	return remediate(ctx, doc, alloc, string(profile), opts, violations)
}

// RemediatePDFUA validates the document against the given PDF/UA profile, applies
// targeted fixes for remediable violations, and returns a report.
func RemediatePDFUA(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile models.PDFUAProfile, opts Options) (*Report, error) {
	ctx = wholeStreams(ctx)
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
	ctx = wholeStreams(ctx)
	violations, err := compliance.ValidateWTPDF(ctx, doc, profile)
	if err != nil {
		return nil, fmt.Errorf("validating for remediation: %w", err)
	}
	return remediate(ctx, doc, alloc, string(profile), opts, violations)
}

// reserveDefinedObjects raises alloc past the highest object number the file's
// bytes define ([document.Document.HighestObjectNumber], which runs the raw-byte
// augmentation of the cross-reference index first), before a repair allocates
// an object.
//
// A new object must not take a number the file's bytes define, even one its
// table leaves out: an object written at its number takes its place, and a
// font's /Widths then reads the XMP packet the remediation added. The
// allocator arrives from the caller, which seeds it the same way, and the
// reservation keeps a caller that seeded it from anything less from handing a
// repair such a number. A ctx done before the augmentation completed leaves
// the highest number read from the index alone, so the reservation fails with
// ctx's error rather than let a repair allocate from it.
//
// # Corpus evidence
//
//   - pdfjs issue18444.pdf: its table and its /Size stop at 389 while object
//     390 holds its cross-reference stream, and its PDF/A-2b remediation wrote
//     a new object 390.
//   - pdfjs issue16759.pdf, pdfjsbad1586.pdf, poppler-90-0-fuzzed.pdf: define
//     objects 60000, 33 and 406 past the numbers their tables and /Size cover,
//     and their remediations now allocate past them.
//   - All corpora: over the PDF/A-1b and PDF/A-2b remediations, default and
//     aggressive, of 248 files that store objects in object streams or whose
//     table leaves objects out, only those four files change, no residual
//     violation moves, and a fresh reader and one that extracted the text of
//     every page first write the same bytes.
func reserveDefinedObjects(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) error {
	alloc.Reserve(doc.HighestObjectNumber(ctx))
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("reserving the object numbers the file defines: %w", err)
	}
	return nil
}

func remediate(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, profile string, opts Options, violations []error) (*Report, error) {
	fontsJudged := opts.Fonts != nil && !slices.Contains(opts.Disabled, "font-embed")
	if len(violations) == 0 {
		doc.Logger.Debug("remediate: no violations",
			slog.String("profile", profile),
		)
		return &Report{FontsJudged: fontsJudged}, nil
	}
	if err := reserveDefinedObjects(ctx, doc, alloc); err != nil {
		return nil, err
	}
	fonts := &fontLedger{}
	opts.fonts = fonts
	flat := &flattenLedger{}
	opts.flattened = flat

	// Classify violations by fixer. A disabled fixer claims nothing, so the
	// violations it would have answered stay in the residual.
	typed := toViolations(violations)
	plan := slices.DeleteFunc(classify(typed), func(e planEntry) bool {
		return slices.Contains(opts.Disabled, e.f.name)
	})

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

	// Execute fixers in registry order, the content tier after the others.
	// The writer rolls a regression back to the snapshot taken between the
	// two, so a rollback discards the content tier's repairs and keeps every
	// other one. Run interleaved, a structural or resource fixer registered
	// after a content fixer lost its repair to the rollback while the report
	// still listed it as fixed.
	var fixed, flattened []string
	var needsRewrite bool
	handled := make(map[int]bool) // indices of handled violations

	others, content := splitContentTier(plan)
	for _, entry := range others {
		descriptions, err := applyPlanEntry(ctx, alloc, doc, opts, profile, entry)
		if err != nil {
			return nil, err
		}
		fixed = append(fixed, descriptions...)
		for _, idx := range entry.indices {
			handled[idx] = true
		}
		if entry.f.name == "file-structure" && len(descriptions) > 0 {
			needsRewrite = true
		}
		if entry.f.name == FlattenFixer && flattenedAny(descriptions) {
			flattened = descriptions
		}
	}

	// A trailer SHRINK (e.g. removing /Info for PDF/A-4 §6.1.3) can only
	// stick through a full rewrite, because earlier trailers inherit absent
	// keys via /Prev. Only fixers outside the content tier edit the trailer,
	// so its state is final here, where the version declaration below needs
	// to know whether the output is rewritten.
	if len(doc.Index.Trailer) < origTrailerSize {
		needsRewrite = true
	}

	requiredVersion := requiredVersionFor(profile)

	// A full rewrite writes requiredVersion in the header, and an incremental
	// update, which cannot touch the header, declares it in the catalog
	// /Version (ISO 32000-1, 7.7.2, Table 28). Declared before the content
	// tier, so a rollback keeps it with the other repairs.
	if fix := declareOutputVersion(ctx, alloc, doc, requiredVersion, needsRewrite); fix != "" {
		fixed = append(fixed, fix)
	}

	// Snapshot of the allocator taken right before the content tier runs. It
	// lets the writer roll back exactly the content tier when the
	// post-remediation integrity probe detects a regression.
	var contentSnapshot *writer.Snapshot
	var contentFixed, contentFixers []string
	if len(content) > 0 {
		snap := alloc.Snapshot()
		contentSnapshot = &snap
	}
	for _, entry := range content {
		descriptions, err := applyPlanEntry(ctx, alloc, doc, opts, profile, entry)
		if err != nil {
			return nil, err
		}
		fixed = append(fixed, descriptions...)
		contentFixed = append(contentFixed, descriptions...)
		contentFixers = append(contentFixers, entry.f.name)
		for _, idx := range entry.indices {
			handled[idx] = true
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

	// Trailer mutations (e.g. /ID insertion) don't allocate objects but
	// still have to be serialized. Incremental writes that ADD keys are
	// covered by an extra xref section.
	trailerDirty := len(doc.Index.Trailer) != origTrailerSize

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
		ContentFixers:    contentFixers,
		FontRefusals:     fonts.refused,
		FontsEmbedded:    fonts.embedded,
		FontsJudged:      fontsJudged,
		Flattened:        flattened,
		FlattenedImages:  flat.images,
	}, nil
}

// splitContentTier returns the entries of plan outside the content tier and
// the content-tier ones, each in plan order.
func splitContentTier(plan []planEntry) (others, content []planEntry) {
	for _, entry := range plan {
		if entry.f.safety == safetyContent {
			content = append(content, entry)
		} else {
			others = append(others, entry)
		}
	}
	return others, content
}

// applyPlanEntry runs the fixer of one plan entry against the violations it
// matched and returns the descriptions of what it repaired.
func applyPlanEntry(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, profile string, entry planEntry) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("remediation canceled: %w", err)
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
	return descriptions, nil
}

// requiredVersionFor returns the PDF version the output of a remediation for
// profile declares, or "" when the profile requires none: the version of the
// PDF standard the profile builds on. ISO 19005-1 builds on PDF 1.4, ISO
// 19005-2, ISO 19005-3 and ISO 14289-1 on ISO 32000-1 (PDF 1.7), and ISO
// 19005-4 and ISO 14289-2 on ISO 32000-2 (PDF 2.0).
//
// It reaches the output two ways. Report.RequiredVersion carries it into the
// writer's full-rewrite config, which writes it in the header, and
// [declareOutputVersion] writes it in the catalog /Version of an output the
// writer extends incrementally, whose header stays the source's. The catalog
// entry raises the header's version and cannot lower it (ISO 32000-1, 7.7.2,
// Table 28), so an incremental PDF/A-1 output of a later source keeps the
// later version, which ISO 19005-1, 6.1.2 does not constrain.
func requiredVersionFor(profile string) string {
	switch {
	case strings.HasPrefix(profile, "PDF/A-4"):
		return "2.0"
	case strings.HasPrefix(profile, "PDF/A-1"):
		return "1.4"
	case strings.HasPrefix(profile, "PDF/A-2"), strings.HasPrefix(profile, "PDF/A-3"):
		return "1.7"
	case strings.HasPrefix(profile, "PDF/UA-1"):
		// ISO 14289-1 §6.1 (PDFUA-1.xml 6.1 t1): the header shall be
		// %PDF-1.n with n in 0..7, so a PDF 2.0 source is pinned down
		// to 1.7, the highest conforming version.
		return "1.7"
	case strings.HasPrefix(profile, "PDF/UA-2"):
		// ISO 14289-2 builds on PDF 2.0. Remediation writes PDF 2.0
		// structure namespaces (/Namespaces, /NS) for this profile, so
		// leaving the source's version would declare a file that its own
		// contents contradict.
		return "2.0"
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
		return "2.0"
	}
	return ""
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
