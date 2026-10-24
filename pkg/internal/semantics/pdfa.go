package semantics

import (
	"context"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// pdfaIDNamespace is the namespace URI of the PDF/A identification schema,
// whose pdfaid:part names the part of ISO 19005 a document claims (ISO
// 19005-1, 6.7.11; ISO 19005-2 to ISO 19005-4, 6.6.4).
const pdfaIDNamespace = "http://www.aiim.org/pdfa/ns/id/"

// DeclaredPDFAPart returns the part of ISO 19005 the document's catalog XMP
// packet claims through pdfaid:part, 1 to 4, or 0 when it claims none.
//
// This is introspection, not a verdict: it reads the identifier and says what
// it found, as [DeclaredPDFX] does, and only the catalog's packet is read,
// the one that claims conformance for the document (ISO 19005-2, 6.6.4). The
// first value that is a part number wins.
func DeclaredPDFAPart(ctx context.Context, doc *document.Document) int {
	for _, value := range xmpSimpleValues(ctx, catalogPacket(ctx, doc, "the PDF/A identifier"), pdfaIDNamespace, "part") {
		if part, err := strconv.Atoi(value); err == nil && part >= 1 && part <= 4 {
			return part
		}
	}
	return 0
}
