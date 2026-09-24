package remediation

import (
	"context"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// catalogCarriesVersion reports whether the catalog /Version entry is an
// acceptable carrier for this profile's version requirement.
//
// It is deliberately NOT the same set as the profiles whose required version
// is a minimum. PDF/A-4 also requires PDF 2.0, and is excluded: ISO 19005-4
// §6.1.2 governs the file HEADER, and its §6.1.12 check reads the catalog
// entry with fallbackToHeader false, so an ABSENT entry conforms (see
// compliance.checkVersionPart4). Remediation already re-emits the header for
// that profile, so the output conforms with no catalog /Version. Writing one
// there would add a key litho leaves absent by design.
//
// ISO 14289-2 is the opposite case. Its version rule is checked with
// fallbackToHeader true, so the catalog entry and the header are equally
// acceptable carriers, and remediation for these profiles frequently stays
// incremental, where the header cannot be touched at all. WTPDF adopts the
// ISO 14289-2 requirements wholesale and is named separately because
// [RemediateWTPDF] passes the WTPDF level through verbatim rather than
// normalising it to the PDF/UA-2 identifier the way validation does.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4__veraPDF_test_suite_6-1-2-t01-fail-a.pdf: its header is
//     %PDF-1.7, and the remediated output is %PDF-2.0 with NO catalog
//     /Version and zero version-clause findings.
func catalogCarriesVersion(profile string) bool {
	return strings.HasPrefix(profile, "PDF/UA-2") || strings.HasPrefix(profile, "WTPDF")
}

// declareCatalogVersion records version in the catalog /Version entry and
// reports whether it wrote anything.
//
// The catalog is replaced rather than mutated: [writer.ObjAllocator.Snapshot]
// captures pending entries by reference, so editing the dictionary in place
// would also alter a snapshot taken before now.
func declareCatalogVersion(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, version string) bool {
	ref, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return false
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	if catalog == nil {
		return false
	}

	next := make(ast.PdfDict, len(catalog)+1)
	maps.Copy(next, catalog)
	next["Version"] = ast.PdfName(version)
	return alloc.AllocAt(ref.ObjectNum, next)
}
