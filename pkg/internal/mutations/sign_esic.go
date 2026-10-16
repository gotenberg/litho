package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// The ESIC extension levels ETSI EN 319 142-1 V1.2.1, 5.6 has a writer
// declare, each in a developer extensions dictionary of base version 1.7.
// iText declares level 2 for ETSI.CAdES.detached likewise
// (PdfDeveloperExtension.ESIC_1_7_EXTENSIONLEVEL2); pyHanko declares level 1
// for it too, which 5.6 reserves for the clause 5.4 extensions.
const (
	// ESICLevelLTV declares the document security store and document
	// timestamps of clause 5.4.
	ESICLevelLTV = 1
	// ESICLevelCAdES declares the ETSI.CAdES.detached subfilter of clause
	// 6.3, requirement l).
	ESICLevelCAdES = 2
)

// DeclareESIC adds to catalog, the catalog an update writes, the ESIC
// developer extension of level ETSI EN 319 142-1 V1.2.1, 5.6 has a writer
// declare when it adds to a document older than PDF 2.0 what that version
// defines itself: [ESICLevelLTV] for a document security store or a document
// timestamp (ISO 32000-2, 12.8.4.3 and 12.8.5), [ESICLevelCAdES] for an
// ETSI.CAdES.detached signature (12.8.3.4). The entry is an extensions
// dictionary entry (ISO 32000-1, 7.12) of base version 1.7, as pyHanko and
// iText write it. A PDF 2.0 document gets none, as both skip it there.
//
// An ESIC entry the document has already is kept, whatever its level: 5.6
// asks for both levels when a document holds both extensions, but ISO
// 32000-1, 7.12 gives a prefix a single developer extensions dictionary (the
// array of several is the ISO 32000-2:2020 mechanism, which pyHanko never
// uses for ESIC), and replacing one would withdraw what it declared.
//
// An extensions dictionary written as an object of its own is replaced in the
// update through alloc, so the catalog keeps its reference.
func DeclareESIC(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict, level int) error {
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
		"ExtensionLevel": ast.PdfInt(level),
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
