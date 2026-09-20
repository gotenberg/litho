package remediation

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchFileStructure(v compliance.Violation) bool {
	// PDF/UA-1 header rule (ISO 14289-1 §6.1 t1): a %PDF-2.0 header is
	// repaired by the full rewrite, which re-emits the header at the
	// pinned RequiredVersion (1.7 for UA-1 targets).
	if v.Rule == compliance.Rule_14289_1_6_1_t01 {
		return true
	}

	// Two rules sit inside the clause ranges below that the full rewrite
	// cannot reach, and no other fixer claims either. [fixFileStructure]
	// repairs nothing itself, so claiming one asserts a repair that does not
	// happen and drops the violation out of the residual. Matched on the whole
	// rule rather than the clause: that is the identity, and it cannot drift
	// when a neighbouring prefix widens.
	switch v.Rule {
	case compliance.Rule_19005_4_6_1_9_t01:
		// Parts 2 and 3 spell the object and generation spacing rule 6.1.9,
		// which re-serialisation does repair. ISO 19005-4 gives that number to
		// the Inline Image /F key instead, and a content stream's body is
		// carried across as bytes, so a BI...ID...EI sequence is beyond the
		// rewrite. [fixHexString] documents that limit from measurement.
		return false
	case compliance.Rule_19005_4_6_1_6_1_t03:
		// A 3D stream /Subtype shall be U3D or PRC. The rewrite copies
		// dictionary values verbatim, so a wrong /Subtype survives it, and
		// litho ships no repair for a 3D stream.
		return false
	}

	clause := v.Rule.Clause
	return strings.HasPrefix(clause, "6.1.2") || // header format
		strings.HasPrefix(clause, "6.1.4") || // xref table spacing
		strings.HasPrefix(clause, "6.1.5") || // hex string length (part 4)
		strings.HasPrefix(clause, "6.1.6") || // hex string length / stream length
		strings.HasPrefix(clause, "6.1.7.1") || // stream length
		// ISO 19005-1 spells the stream-length (t1) and stream-keyword-EOL
		// (t2) rules 6.1.7, where parts 2 and 3 spell them 6.1.7.1 and part 4
		// 6.1.6.1. The prefix above is LONGER than the part-1 clause, so it
		// can never match it and those rules went unclaimed for PDF/A-1
		// targets while the full rewrite repaired them everywhere else.
		//
		// Scoped to the specification because ISO 19005-4 clause 6.1.7 is
		// CosUnicodeName, an unrelated requirement, and scoped to t1 and t2
		// because t3 forbids the F, FFilter and FDecodeParms keys, which
		// re-serialisation cannot delete: stream-file-keys repairs that one.
		(v.Rule.Specification == "ISO_19005_1" && clause == "6.1.7" &&
			(v.Rule.TestNumber == 1 || v.Rule.TestNumber == 2)) ||
		strings.HasPrefix(clause, "6.1.8") || // object/endobj spacing
		strings.HasPrefix(clause, "6.1.9") || // object/endobj EOL markers
		clause == "6.1.12" || // integer out of range (part 1/2/4)
		clause == "6.1.13" // integer out of range (part 2)
}

// FullRewriteNote is what [fixFileStructure] reports. It repairs nothing
// itself: it only records that the violations it claimed are handled by the
// full rewrite the orchestrator performs.
//
// Callers driving a fix-to-fixpoint loop must not read it as progress. Every
// round re-emits it while the underlying formatting violation persists, so a
// file with an unfixable one ran the loop to its bound, re-serialising itself
// twice for nothing and reporting "Fixed: 3" with the same sentence listed
// three times.
const FullRewriteNote = "full rewrite re-serializes all objects with clean formatting"

// fixFileStructure is a no-op fixer. The actual fix is that the remediation
// orchestrator detects these violations and the Writer forces a full rewrite,
// which re-serializes all objects with correct formatting.
// This fixer exists to classify these violations as "handled" so they don't
// appear in the residual list.
func fixFileStructure(_ context.Context, _ *writer.ObjAllocator, _ *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	// Only trigger a full rewrite for formatting violations (header, spacing,
	// stream length). Implementation-limit violations (6.1.12, 6.1.13)
	// cannot be fixed by re-serialization: the out-of-range values persist
	// and a full rewrite risks corrupting other objects.
	hasFormatViolation := false
	for _, v := range violations {
		c := v.Rule.Clause
		if c != "6.1.12" && c != "6.1.13" {
			hasFormatViolation = true
			break
		}
	}
	if !hasFormatViolation {
		return nil, nil
	}
	return []string{FullRewriteNote}, nil
}
