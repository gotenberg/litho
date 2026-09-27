package remediation

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/tagging"
)

// matchAutoTag claims the structure and content-marking violations of an
// untagged accessible document (PDF/UA-1/2 and PDF/A-1a/2a/3a): a missing
// /StructTreeRoot, a missing /MarkInfo, and content not marked or artifacted
// (ISO 14289-1 §7.1, clause 8.2.2). It is the union of the matchers of the flat
// struct-tree, mark-info, and content-tag fixers, which the auto-tag fixer
// supersedes by synthesizing a real structure tree.
func matchAutoTag(v compliance.Violation) bool {
	return matchStructTree(v) || matchMarkInfo(v) || matchContentTag(v)
}

// fixAutoTag synthesizes a real structure tree for an untagged document via
// [tagging.AutoTag]: a StructTreeRoot with per-element structure elements and
// per-element marked content, replacing the crude whole-page wrapper of the
// content-tag fixer. Registered ahead of the flat mark-info, struct-tree,
// rolemap, and content-tag fixers so they see a tagged document downstream and
// no-op; those remain the fallback for a document AutoTag cannot tag.
//
// The call is atomic: AutoTag allocates incrementally, so a mid-synthesis
// failure on a malformed page would leave partial objects queued. fixAutoTag
// snapshots the allocator and restores it on error, discarding the partial tree
// and deferring to the flat fixers rather than aborting the whole remediation.
func fixAutoTag(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	snap := alloc.Snapshot()
	res, err := tagging.AutoTag(ctx, alloc, doc)
	if err != nil {
		// Auto-tagging is best-effort: on failure, roll back any partial
		// mutations and let the flat tagging fixers handle the document. The
		// error is logged at Debug, not propagated, so one untaggable page
		// does not fail the entire remediation.
		alloc.Restore(snap)
		doc.Logger.Debug("remediation: auto-tagging failed; deferring to flat tagging fixers",
			slog.Any("err", err))
		return nil, nil
	}
	// A skip is the flat fixers' cue, as a failure is, and has no fix to
	// report.
	if res.Skipped != nil {
		doc.Logger.Debug("remediation: auto-tagging skipped; deferring to flat tagging fixers",
			slog.Any("reason", res.Skipped))
	}
	return res.Descriptions, nil
}
