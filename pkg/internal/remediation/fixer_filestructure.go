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
	clause := v.Rule.Clause
	return strings.HasPrefix(clause, "6.1.2") || // header format
		strings.HasPrefix(clause, "6.1.4") || // xref table spacing
		strings.HasPrefix(clause, "6.1.5") || // hex string length (part 4)
		strings.HasPrefix(clause, "6.1.6") || // hex string length / stream length
		strings.HasPrefix(clause, "6.1.7.1") || // stream length
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
