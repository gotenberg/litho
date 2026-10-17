package remediation

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"regexp"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchWTPDFDeclaration matches the missing PDF Declaration violation that
// WTPDF states of its own (WTPDF 1.0, 6.1.2 for reuse and 6.1.3 for
// accessibility). Both carry the WTPDF_1_0 specification, which no other
// rule in the catalog uses, so the specification alone identifies them:
// the clause numbers 6.1.2 and 6.1.3 also exist under ISO 19005 and mean
// something else entirely there.
func matchWTPDFDeclaration(v compliance.Violation) bool {
	return v.Rule.Specification == "WTPDF_1_0"
}

// fixWTPDFDeclaration writes the PDF Declaration the profile requires into
// the catalog XMP packet.
//
// The shape is load-bearing and was measured against veraPDF rather than
// inferred: the declaration counts only as
//
//	<pdfd:declarations><rdf:Bag><rdf:li rdf:parseType="Resource">
//	  <pdfd:conformsTo>URI</pdfd:conformsTo>
//	</rdf:li></rdf:Bag></pdfd:declarations>
//
// A bare <pdfd:conformsTo> child of rdf:Description is not credited, and
// omitting rdf:parseType="Resource" additionally breaks the unrelated
// metadata rule ISO 14289-2 8.11.1, so a malformed declaration makes the
// file worse. pdfd:claimData, which real-world producers emit alongside
// conformsTo, is not required.
func fixWTPDFDeclaration(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	uri := compliance.WTPDFDeclarationURI(profile)
	if uri == "" {
		return nil, nil
	}

	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	metaObj, ok := catalogDict["Metadata"]
	if !ok {
		// No XMP packet at all. The XMP fixer owns creating one; writing a
		// declaration into a packet that does not exist would race it.
		return nil, nil
	}
	var metaRef ast.PdfIndirectRef
	if ref, ok := metaObj.(ast.PdfIndirectRef); ok {
		metaRef = ref
	}

	// Read the packet an earlier fixer may already have queued, the way
	// fixXMPSchema does: falling back to the source document here would
	// discard the pdfaid and pdfuaid edits fixXMP just made.
	stream, ok := resolveStreamFromAllocOrDoc(ctx, alloc, doc, metaRef, metaObj)
	if !ok {
		return nil, nil
	}
	xmp, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(xmp) == 0 {
		doc.Logger.Debug("remediation: wtpdf: cannot decode catalog XMP", "error", err)
		return nil, nil
	}

	// Byte-level splicing assumes a parseable packet. A malformed one is
	// its own violation; editing it shifts how a lenient reader parses the
	// rest and can invent new failures, so leave it for the validator to
	// report.
	if !compliance.XMPWellFormed(xmp) {
		return nil, nil
	}

	if compliance.WTPDFDeclares(xmp, uri) {
		return nil, nil
	}

	patched, err := ensureNamespaceDecl(xmp, "pdfd", compliance.WTPDFDeclarationNamespace)
	if err != nil {
		doc.Logger.Debug("remediation: wtpdf: cannot bind the pdfd namespace", "error", err)
		return nil, nil
	}

	patched, err = insertWTPDFDeclaration(patched, uri)
	if err != nil {
		doc.Logger.Debug("remediation: wtpdf: cannot splice the declaration", "error", err)
		return nil, nil
	}
	// Claim the fix only when the validator reads the declaration back: a
	// declaration spliced anywhere but a document property states nothing.
	if bytes.Equal(patched, xmp) || !compliance.WTPDFDeclares(patched, uri) {
		doc.Logger.Debug("remediation: wtpdf: spliced declaration does not read back", "uri", uri)
		return nil, nil
	}

	// The packet goes back decoded, so the filter chain describing the old
	// body no longer applies.
	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	delete(newDict, "Filter")
	delete(newDict, "DecodeParms")
	newStream := ast.NewStream(newDict, patched)
	if metaRef.ObjectNum > 0 {
		alloc.AllocAt(metaRef.ObjectNum, newStream)
	} else {
		ref := alloc.Alloc(newStream)
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Metadata"] = ref
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{fmt.Sprintf("declared WTPDF conformance %s in XMP", uri)}, nil
}

// declarationsCloseRe matches the closing tag pair of a pdfd:declarations
// container across the whitespace real producers write between them.
var declarationsCloseRe = regexp.MustCompile(`</rdf:Bag>\s*</pdfd:declarations>`)

// insertWTPDFDeclaration splices the declaration block into the packet. An
// existing pdfd:declarations container is extended with one more rdf:li so a
// file claiming one level keeps that claim when the other is added;
// otherwise a fresh container is inserted.
func insertWTPDFDeclaration(xmp []byte, uri string) ([]byte, error) {
	item := fmt.Sprintf(
		`<rdf:li rdf:parseType="Resource"><pdfd:conformsTo>%s</pdfd:conformsTo></rdf:li>`, uri)

	// Real producers close the container across several lines
	// ("</rdf:Bag>\r\n      </pdfd:declarations>"), so match the pair
	// tolerantly. A byte-exact search missed every real packet and fell
	// through to inserting a SECOND pdfd:declarations container beside the
	// first, which is not a shape any producer writes.
	//
	// The extended container must be a document property for the new item to
	// count. A container that sits anywhere else, such as inside an item of
	// pdfaExtension:schemas, states nothing (see compliance.WTPDFDeclares), so
	// when the item does not read back a fresh container goes on the document
	// instead.
	if loc := declarationsCloseRe.FindIndex(xmp); loc != nil {
		out := make([]byte, 0, len(xmp)+len(item))
		out = append(out, xmp[:loc[0]]...)
		out = append(out, item...)
		out = append(out, xmp[loc[0]:]...)
		if compliance.WTPDFDeclares(out, uri) {
			return out, nil
		}
	}

	block := "<pdfd:declarations><rdf:Bag>" + item + "</rdf:Bag></pdfd:declarations>"
	return insertChildElement(xmp, block)
}
