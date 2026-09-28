package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// DeclareESIC adds to catalog, the catalog an update writes, the ESIC
// developer extension ETSI EN 319 142-1, 5.6 has a writer declare when it
// adds a document security store or a document timestamp to a document older
// than PDF 2.0, the version that defines both (ISO 32000-2, 12.8.4.3 and
// 12.8.5): an extensions dictionary entry (ISO 32000-1, 7.12) of base
// version 1.7 and extension level 1. An ESIC entry the document has already
// is kept, and a PDF 2.0 document gets none.
//
// An extensions dictionary written as an object of its own is replaced in the
// update through alloc, so the catalog keeps its reference.
func DeclareESIC(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) error {
	if doc.Version >= "2.0" {
		return nil
	}
	extObj, has := catalog["Extensions"]
	ext := ast.PdfDict{}
	if has {
		current, err := resolveDictAllocFirst(ctx, alloc, doc, extObj)
		if err != nil || current == nil {
			// An extensions entry that is not a dictionary declares nothing,
			// and ISO 32000-1, Table 28 types it a dictionary, so it is
			// written anew.
			has = false
		} else {
			if _, ok := current["ESIC"]; ok {
				return nil
			}
			ext = maps.Clone(current)
		}
	}
	ext["ESIC"] = ast.PdfDict{
		"Type":           ast.PdfName("DeveloperExtensions"),
		"BaseVersion":    ast.PdfName("1.7"),
		"ExtensionLevel": ast.PdfInt(1),
	}
	if ref, ok := extObj.(ast.PdfIndirectRef); has && ok {
		if ref.ObjectNum <= 0 {
			return fmt.Errorf("the catalog's /Extensions names object %d", ref.ObjectNum)
		}
		alloc.AllocAt(ref.ObjectNum, ext)
		return nil
	}
	catalog["Extensions"] = ext
	return nil
}
