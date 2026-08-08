package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchCIDSystemInfo matches composite-font CIDSystemInfo mismatches
// reported by ISO 19005-2/3 §6.2.11.3.1 and ISO 19005-4 §6.2.10.3.1.
func matchCIDSystemInfo(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.10.3.1", "6.2.11.3.1") {
		return false
	}
	return contextContains(v, "CIDFont") || contextContains(v, "CMap")
}

// fixCIDSystemInfo forces every Type-0 font to use /Identity-H (or
// /Identity-V for vertical-writing CIDFonts). ISO 32000-1 §9.7.2.1
// exempts those two encodings from the CIDSystemInfo equivalence rule,
// so relabelling sidesteps Registry/Ordering/Supplement mismatches
// without touching the CMap or CIDFont dictionaries.
func fixCIDSystemInfo(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	count := 0
	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type != 1 || objNum == 0 {
			continue
		}
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil || obj == nil {
			continue
		}
		fontDict, ok := obj.(ast.PdfDict)
		if !ok {
			continue
		}
		if typ, _ := fontDict["Type"].(ast.PdfName); typ != "Font" {
			continue
		}
		if subtype, _ := fontDict["Subtype"].(ast.PdfName); subtype != "Type0" {
			continue
		}

		// Relabelling only sidesteps the CIDSystemInfo rule when the CMap it
		// replaces already maps two-byte codes to equal CIDs. A real CMap
		// defines its own codespace, and swapping it for /Identity-H
		// reinterprets every byte the content stream already holds:
		// issue18117.pdf embeds /Uni-Utf8-H, whose codes are one to four bytes,
		// and relabelling it produced an odd-length code tail and glyphs that
		// resolve to .notdef (ISO 19005-2, 6.2.11.8 t1 and 6.2.11.4.1 t2). An
		// unfixed CIDSystemInfo mismatch is an honest residual; rewritten text
		// is not recoverable.
		if !encodingIsIdentityCompatible(ctx, doc, fontDict) {
			continue
		}

		enc := chooseIdentityEncoding(ctx, doc, fontDict)
		current, _ := fontDict["Encoding"].(ast.PdfName)
		if current == enc {
			continue
		}
		newFont := make(ast.PdfDict, len(fontDict))
		maps.Copy(newFont, fontDict)
		newFont["Encoding"] = enc
		alloc.AllocAt(objNum, newFont)
		count++
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("normalised Type0 /Encoding to /Identity on %d font(s)", count)}, nil
}

// chooseIdentityEncoding picks Identity-V when the descendant CIDFont
// is configured for vertical writing, Identity-H otherwise.
// encodingIsIdentityCompatible reports whether a Type 0 font's /Encoding can be
// relabelled /Identity-H or /Identity-V without changing which CIDs its content
// bytes select.
//
// That holds only when the codes already ARE the CIDs: an /Identity-H or
// /Identity-V name, or no /Encoding at all. An embedded CMap stream and a
// predefined CMap both define their own codespace and mapping
// (ISO 32000-1, 9.7.5), so replacing either one rewrites the text.
func encodingIsIdentityCompatible(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return true
	}
	name, err := document.ResolveName(ctx, doc, encObj)
	if err != nil || name == "" {
		return false // A stream CMap, or unreadable: not safe to relabel.
	}
	return name == "Identity-H" || name == "Identity-V"
}

func chooseIdentityEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfName {
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return "Identity-H"
	}
	descDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || descDict == nil {
		return "Identity-H"
	}
	if w2, ok := descDict["W2"]; ok && w2 != nil {
		return "Identity-V"
	}
	if dw2, ok := descDict["DW2"]; ok && dw2 != nil {
		return "Identity-V"
	}
	return "Identity-H"
}
