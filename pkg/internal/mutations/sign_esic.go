package mutations

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/intconv"
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
// A document holding both extensions declares the higher level alone. 5.6
// asks for an entry of each level then, but ISO 32000-1, 7.12.3, Table 48
// gives a prefix a single developer extensions dictionary, and the array of
// several is the ISO 32000-2:2020 form, which a document older than PDF 2.0,
// the only kind given an ESIC entry, cannot use. Table 50 has a developer's
// extension levels increase over time for a base version, and the extension
// convention it comes from makes each level include the ones below it (Adobe
// Supplement to ISO 32000, BaseVersion 1.7, ExtensionLevel 3, "Extension
// levels"). iText (PdfCatalog.addDeveloperExtension) and pyHanko (the ESIC
// extension compares by level) read ESIC so: an entry of a lower level gives
// way to the higher one, and one of the same or a higher level is kept, as
// is one of a higher base version. An entry that is not a dictionary, such as
// the ISO 32000-2:2020 array, is kept as it stands.
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
			if declared, ok := current["ESIC"]; ok && esicCovers(ctx, alloc, doc, declared, level) {
				return nil
			}
			ext = maps.Clone(current)
		}
	}
	ext["ESIC"] = ast.PdfDict{
		"Type":           ast.PdfName("DeveloperExtensions"),
		"BaseVersion":    ast.PdfName(esicBaseVersion),
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

// esicBaseVersion is the base version ETSI EN 319 142-1 V1.2.1, 5.6 gives
// the ESIC developer extensions.
const esicBaseVersion = "1.7"

// esicCovers reports whether declared, the ESIC entry of an extensions
// dictionary, declares level already (see [DeclareESIC]): a developer
// extensions dictionary of a higher base version, or of base version 1.7 and
// a level at least as high (ISO 32000-1, 7.12.3, Table 50), or an entry that
// is no dictionary, which litho leaves as it stands.
func esicCovers(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, declared ast.PdfObject, level int) bool {
	ext, err := resolveDictAllocFirst(ctx, alloc, doc, declared)
	if err != nil || ext == nil {
		return true
	}
	// A missing or unreadable base version or level compares as the lowest,
	// so a declaration that states neither gives way to one that does.
	base, err := document.ResolveName(ctx, doc, ext["BaseVersion"])
	if err != nil {
		base = ""
	}
	if base != esicBaseVersion {
		return base > esicBaseVersion
	}
	declaredLevel, err := document.ResolveInt(ctx, doc, ext["ExtensionLevel"])
	if err != nil {
		return false
	}
	return intconv.Clamp(declaredLevel) >= level
}
