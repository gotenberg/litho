package mutations

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Structure namespaces (ISO 32000-2, 14.8.6): the PDF 1.7 namespace, the
// default for an element with no /NS, and the PDF 2.0 namespace.
const (
	pdf17StructNamespace = "http://iso.org/pdf/ssn"
	pdf20StructNamespace = "http://iso.org/pdf2/ssn"
)

// rootDocument returns the root's only kid of standard type Document (see
// [isDocumentElement]), zero when it holds none, several, or one written
// directly into /K, which no element can name as its parent.
//
// ISO 14289-2, 8.2.5.2 asks for the root's only kid to be a Document element,
// and ISO/TS 32005, Table 5 for the root to hold exactly one. A source whose
// root holds one describes its content as that document's, which is where the
// output puts what the source carries (see [structOutput.add]).
func rootDocument(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, root ast.PdfDict) ast.PdfIndirectRef {
	var found ast.PdfIndirectRef
	for _, kid := range structKidList(ctx, alloc, doc, root["K"]) {
		elem, err := resolveDictAllocFirst(ctx, alloc, doc, kid)
		if err != nil || elem == nil || !isDocumentElement(ctx, alloc, doc, root, elem) {
			continue
		}
		ref, isRef := kid.(ast.PdfIndirectRef)
		if !isRef || found.ObjectNum != 0 {
			return ast.PdfIndirectRef{}
		}
		found = ref
	}
	return found
}

// maxRoleMapSteps caps how many role mappings [isDocumentElement] follows.
// Anti-DoS guard, not spec-mandated: a chain longer than a handful of steps
// maps nothing a real document means, and a circular one must end.
const maxRoleMapSteps = 32

// isDocumentElement reports whether elem, an element of the tree whose root
// is root, has the standard structure type Document (ISO 32000-1, 14.8.4.2;
// ISO 32000-2, 14.8.4.3), directly or through role mapping: the root's
// /RoleMap for an element in the default namespace (ISO 32000-1, 14.7.3), and
// the /RoleMapNS of its namespace for one that names one (ISO 32000-2,
// 14.7.4.2, Table 356).
func isDocumentElement(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, root, elem ast.PdfDict) bool {
	s, isName := elem["S"].(ast.PdfName)
	if !isName {
		return false
	}
	ns, _ := resolveDictAllocFirst(ctx, alloc, doc, elem["NS"])
	roleMap, _ := resolveDictAllocFirst(ctx, alloc, doc, root["RoleMap"])
	for range maxRoleMapSteps {
		uri := namespaceURI(ctx, alloc, doc, ns)
		if s == "Document" && (uri == "" || uri == pdf17StructNamespace || uri == pdf20StructNamespace) {
			return true
		}
		var mapped ast.PdfObject
		if ns == nil {
			mapped = roleMap[s]
		} else {
			roleMapNS, _ := resolveDictAllocFirst(ctx, alloc, doc, ns["RoleMapNS"])
			mapped = roleMapNS[s]
		}
		resolved, err := resolveAllocFirst(ctx, alloc, doc, mapped)
		if err != nil || resolved == nil {
			return false
		}
		switch v := resolved.(type) {
		case ast.PdfName:
			// A plain name maps into the default namespace.
			s, ns = v, nil
		case ast.PdfArray:
			if len(v) < 2 {
				return false
			}
			name, isName := v[0].(ast.PdfName)
			if !isName {
				return false
			}
			target, err := resolveDictAllocFirst(ctx, alloc, doc, v[1])
			if err != nil {
				return false
			}
			s, ns = name, target
		default:
			return false
		}
	}
	return false
}

// namespaceURI returns the /NS string of a namespace dictionary, "" for none.
func namespaceURI(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, ns ast.PdfDict) string {
	if ns == nil {
		return ""
	}
	obj, err := resolveAllocFirst(ctx, alloc, doc, ns["NS"])
	if err != nil {
		return ""
	}
	s, _ := obj.(ast.PdfString)
	return document.DecodePdfString(s)
}
