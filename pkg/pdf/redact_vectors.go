package pdf

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/mutations"
	"github.com/gotenberg/litho/pkg/internal/remediation"
)

// clearOpaqueCopies removes the document-wide homes of a copy that the
// verification scan cannot prove clean, and stages the signature removal the
// rewrite makes, so the scan answers for the file [Writer.WriteTo] writes. It
// records what it removed in report.
//
//   - Scripts go (see [remediation.StripJavaScript]): a script can build a
//     removed word from pieces at run time, which no comparison of bytes finds.
//   - The private data of the catalog's /PieceInfo goes (see
//     [mutations.ScrubCatalogPrivateData]): an application's own format, an
//     embedded search index among them, that the scan cannot read into.
//   - Signatures are drawn into their pages and leave the form, /Perms and
//     /DSS (see [mutations.BakeSignaturesForRewrite]). Redaction forces a full
//     rewrite and the rewrite removes them anyway, since no signature verifies
//     over bytes it no longer covers (ISO 32000-1, 12.8.1). Scanning the
//     document before that step refused a redaction over a signer's /Reason
//     or a certificate that the output never carries.
func clearOpaqueCopies(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, report *RedactReport) error {
	scripts, err := remediation.StripJavaScript(ctx, alloc, doc)
	if err != nil {
		return fmt.Errorf("removing scripts: %w", err)
	}
	report.RemovedScripts = scripts

	private, err := mutations.ScrubCatalogPrivateData(ctx, alloc, doc)
	if err != nil {
		return fmt.Errorf("clearing private data: %w", err)
	}
	report.ScrubbedPrivateData = private

	baked, err := mutations.BakeSignaturesForRewrite(ctx, doc, maps.Clone(alloc.Pending()))
	if err != nil {
		return fmt.Errorf("drawing the signatures into their pages: %w", err)
	}
	// The result holds every staged edit too, so queueing it whole changes
	// only what the step itself wrote.
	for _, objNum := range slices.Sorted(maps.Keys(baked)) {
		alloc.AllocAt(objNum, baked[objNum])
	}
	return nil
}
