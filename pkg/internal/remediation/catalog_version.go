package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// declareOutputVersion makes the remediation output declare required, the
// version [requiredVersionFor] gives its profile, as far as the write path
// allows, and leaves no catalog /Version contradicting what it declares. It
// returns a description of the catalog edit it made, or "" when the catalog
// already agrees.
//
// The output declares the later of its header and required. A full rewrite
// writes required in the header (see mutations.FullRewrite), so the two agree.
// An incremental update keeps the source's header, byte zero of the bytes it
// preserves, and ISO 32000-1, 7.7.2 (Table 28) gives the catalog /Version as
// the version the document conforms to when it is later than the header's
// (7.5.2): an earlier header is raised there. A later header stays, since
// Table 28 lets the entry raise the header's version and never lower it. That
// case reaches PDF/A-1 alone: a later header fails ISO 19005-2 and 19005-3,
// 6.1.2 (%PDF-1.n, n up to 7), ISO 19005-4, 6.1.2 (%PDF-2.n) and ISO 14289-1,
// 6.1 (%PDF-1.n, n up to 7), whose violations the file-structure fixer
// answers with a rewrite, while ISO 19005-1, 6.1.2 constrains the header's
// form and not its number.
//
// When the header declares the version, an entry naming another is removed
// rather than rewritten. litho reads the entry in both directions, as pdf.js
// does (see document.initCatalogVersion), and ISO 19005-4, 6.1.12 wants it 2.n
// when present, so a full rewrite that copied an entry of /1.7 under the
// %PDF-2.0 header it wrote failed that clause, and litho reported the output
// as 1.7. An absent entry defers to whatever header the writer emits, which a
// caller forcing a rewrite of an output planned as incremental also changes.
// For the same reason an absent entry stays absent: ISO 19005-4, 6.1.12
// accepts one, and adding it would write a key the source never had.
//
// The catalog is replaced rather than mutated: [writer.ObjAllocator.Snapshot]
// captures pending entries by reference, so editing the dictionary in place
// would also alter a snapshot taken before now.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4__veraPDF_test_suite_6-1-2-t01-fail-a.pdf: its header is
//     %PDF-1.7, and the remediated output is %PDF-2.0 with no catalog
//     /Version and no version-clause finding.
func declareOutputVersion(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, required string, rewrite bool) string {
	if required == "" {
		return ""
	}
	header := doc.HeaderVersion
	if rewrite {
		header = required
	}
	// A header that is no version number yields required.
	declared := writer.AtLeastVersion(header, required)

	ref, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return ""
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	if catalog == nil {
		return ""
	}
	current, present := catalog["Version"]
	if name, ok := current.(ast.PdfName); ok && string(name) == declared {
		return ""
	}

	next := make(ast.PdfDict, len(catalog)+1)
	maps.Copy(next, catalog)
	var fix string
	switch {
	case declared != header:
		next["Version"] = ast.PdfName(declared)
		fix = fmt.Sprintf("declared PDF version %s in the document catalog", declared)
	case present:
		delete(next, "Version")
		fix = fmt.Sprintf("removed the document catalog /Version, which contradicted PDF version %s", declared)
	default:
		return ""
	}
	if !alloc.AllocAt(ref.ObjectNum, next) {
		return ""
	}
	return fix
}
