package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// matchCIDToGIDMap matches the Type 2 CIDFont CIDToGIDMap violation. The rule
// requires every CIDFontType2 to carry a /CIDToGIDMap entry (a stream or the
// name Identity); it is shared by PDF/A-2/3/4 and PDF/UA-1/2 (PDF/A-1 does not
// emit it). The matcher keys on the rule description, the one substring common
// to every profile's wording, rather than the clause, which differs per part.
func matchCIDToGIDMap(v compliance.Violation) bool {
	return descriptionContains(v, "CIDToGIDMap")
}

// fixCIDToGIDMap writes /CIDToGIDMap /Identity onto every CIDFontType2 whose
// entry is missing or non-conformant. Identity is the entry's default value
// (ISO 32000-1 Table 117; ISO 32000-2 Table 115), so stating it explicitly is
// render-preserving: a reader already treats an absent entry as Identity, and a
// malformed name or wrong type produces undefined behaviour that readers
// likewise resolve to the default. A present /Identity name or a CIDToGIDMap
// stream already conforms and is left untouched, so a real CID-to-glyph mapping
// is never clobbered. The repair mirrors [compliance] checkCIDToGIDMap: it keys
// on /Subtype CIDFontType2 alone, the same condition the validator tests. Only
// indirect CIDFonts are rewritten; an inline descendant CIDFont (a direct dict
// in a Type0 font's /DescendantFonts) has no object number to patch in place and
// is left to the validator to report.
//
// Fonts held in an object stream (ISO 32000-1, 7.5.7) are repaired too. Their
// cross-reference entry is type 2 rather than a byte offset (Table 18), and
// real producers store CIDFonts that way, so skipping them left the violation
// standing on most files that carry it. Rewriting one is sound: the update
// defines the object normally in a later cross-reference section, which
// supersedes the copy inside the object stream (7.5.8). Objects in an object
// stream always have generation 0 (7.5.7), which is what the entry reports.
func fixCIDToGIDMap(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int
	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type == xref.XRefTypeFree || objNum == 0 {
			continue
		}

		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if fontDict == nil {
			continue
		}
		if subtype, _ := fontDict["Subtype"].(ast.PdfName); subtype != "CIDFontType2" {
			continue
		}

		if cidObj, ok := fontDict["CIDToGIDMap"]; ok {
			// The font dict is read alloc-aware, so cidObj reflects any
			// pending edit. The value itself is resolved against the source
			// document: no fixer rewrites a /CIDToGIDMap target, so a pending
			// edit to that object cannot exist here.
			resolved, err := document.ResolveObject(ctx, doc, cidObj)
			if err != nil {
				// An unreadable entry is left alone rather than guessed at.
				doc.Logger.Debug("remediation: cid-gidmap: cannot resolve /CIDToGIDMap",
					"obj", objNum, "error", err)
				continue
			}
			switch v := resolved.(type) {
			case ast.PdfName:
				if v == "Identity" {
					continue
				}
				// A non-Identity name is invalid; normalise to Identity.
			case ast.PdfStream:
				// A custom CID-to-glyph mapping already conforms.
				continue
			default:
				// Any other type is invalid; normalise to Identity.
			}
		}

		newFont := make(ast.PdfDict, len(fontDict)+1)
		maps.Copy(newFont, fontDict)
		newFont["CIDToGIDMap"] = ast.PdfName("Identity")
		alloc.AllocAt(objNum, newFont)
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("set /CIDToGIDMap /Identity on %d CIDFontType2 font(s)", count)}, nil
}
