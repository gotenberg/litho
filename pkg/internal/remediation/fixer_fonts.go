package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

func matchFontFixes(v compliance.Violation) bool {
	return clausePrefix(v,
		"6.2.10.5", "6.2.11.5", "6.3.6", // glyph width mismatch
		"6.2.10.7",                 // ToUnicode maps to U+0000/FEFF/FFFE
		"6.2.10.3.1", "6.2.11.3.1", // CIDSystemInfo mismatch
		"6.2.11.4.2", // CIDSet incomplete
		"6.3.5",      // CharSet incomplete
	)
}

func fixFontIssues(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.GetCatalog(ctx, doc)
	if err != nil {
		return nil, nil
	}
	pages, err := document.GetPages(ctx, doc, catalog)
	if err != nil {
		return nil, nil
	}

	var fixes []string
	processed := make(map[int]bool)

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if page.Resources == nil {
			continue
		}
		fontDictObj, ok := page.Resources["Font"]
		if !ok {
			continue
		}
		fontDict, err := document.ResolveDict(ctx, doc, fontDictObj)
		if err != nil || fontDict == nil {
			continue
		}

		for _, fontEntry := range fontDict {
			var fontRef ast.PdfIndirectRef
			if ref, ok := fontEntry.(ast.PdfIndirectRef); ok {
				fontRef = ref
			}
			if fontRef.ObjectNum > 0 && processed[fontRef.ObjectNum] {
				continue
			}
			if fontRef.ObjectNum > 0 {
				processed[fontRef.ObjectNum] = true
			}

			fd, err := document.ResolveDict(ctx, doc, fontEntry)
			if err != nil || fd == nil {
				continue
			}

			subtype, _ := fd["Subtype"].(ast.PdfName)
			switch string(subtype) {
			case "Type0":
				if f := fixType0Font(ctx, doc, alloc, fd); len(f) > 0 {
					fixes = append(fixes, f...)
				}
			case "Type1", "TrueType", "MMType1":
				if f := fixSimpleFont(ctx, doc, alloc, fd, fontRef); len(f) > 0 {
					fixes = append(fixes, f...)
				}
			}
		}
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

func fixSimpleFont(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef) []string {
	fdObj, ok := fontDict["FontDescriptor"]
	if !ok {
		return nil
	}
	var fdRef ast.PdfIndirectRef
	if ref, ok := fdObj.(ast.PdfIndirectRef); ok {
		fdRef = ref
	}
	fdDict, err := document.ResolveDict(ctx, doc, fdObj)
	if err != nil || fdDict == nil {
		return nil
	}

	fontData := extractFontProgram(ctx, doc, fdDict)
	if fontData == nil {
		return nil
	}

	binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
	if len(binaryWidths) == 0 {
		return nil
	}
	upm := typography.ReadUnitsPerEm(fontData)
	if upm == 0 {
		upm = 1000
	}

	widthsObj, ok := fontDict["Widths"]
	if !ok {
		return nil
	}
	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil || widths == nil {
		return nil
	}

	firstChar := 0
	if fc, ok := fontDict["FirstChar"].(ast.PdfInt); ok {
		firstChar = int(fc)
	}

	charToGID := typography.ExtractCharCodeToGID(ctx, fontData)

	modified := false
	newWidths := make(ast.PdfArray, len(widths))
	copy(newWidths, widths)

	for i, wObj := range widths {
		pdfWidth := document.GetFloat(wObj)
		charCode := uint16(firstChar + i) //nolint:gosec // charCode range is bounded by PDF spec
		gid := charCode
		if charToGID != nil {
			if mapped, ok := charToGID[charCode]; ok {
				gid = mapped
			}
		}
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expected := float64(bw) * 1000.0 / float64(upm)
		if math.Abs(pdfWidth-expected) > 1.0 {
			newWidths[i] = ast.PdfReal(math.Round(expected*100) / 100)
			modified = true
		}
	}

	var fixes []string
	baseName := fontBaseName(fontDict)

	if modified {
		newFont := make(ast.PdfDict, len(fontDict))
		maps.Copy(newFont, fontDict)
		newFont["Widths"] = newWidths
		if fontRef.ObjectNum > 0 {
			alloc.AllocAt(fontRef.ObjectNum, newFont)
		}
		fixes = append(fixes, fmt.Sprintf("corrected /Widths for font %s", baseName))
	}

	// Fix CharSet if incomplete.
	if _, ok := fdDict["CharSet"]; ok {
		if fixed := fixCharSet(ctx, alloc, fdDict, fdRef, fontData); fixed {
			fixes = append(fixes, fmt.Sprintf("regenerated /CharSet for font %s", baseName))
		}
	}

	return fixes
}

func fixType0Font(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict) []string {
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}

	var cidRef ast.PdfIndirectRef
	if ref, ok := descArr[0].(ast.PdfIndirectRef); ok {
		cidRef = ref
	}
	cidFont, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFont == nil {
		return nil
	}

	fdObj, ok := cidFont["FontDescriptor"]
	if !ok {
		return nil
	}
	var fdRef ast.PdfIndirectRef
	if ref, ok := fdObj.(ast.PdfIndirectRef); ok {
		fdRef = ref
	}
	fdDict, err := document.ResolveDict(ctx, doc, fdObj)
	if err != nil || fdDict == nil {
		return nil
	}

	fontData := extractFontProgram(ctx, doc, fdDict)
	if fontData == nil {
		return nil
	}

	binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
	if len(binaryWidths) == 0 {
		return nil
	}
	upm := typography.ReadUnitsPerEm(fontData)
	if upm == 0 {
		upm = 1000
	}

	var fixes []string
	baseName := fontBaseName(fontDict)

	// Fix /W array.
	if wObj, ok := cidFont["W"]; ok {
		wArr, err := document.ResolveArray(ctx, doc, wObj)
		if err == nil && wArr != nil {
			newW, corrected := correctCIDWidths(ctx, doc, wArr, binaryWidths, upm)
			if corrected {
				newCID := make(ast.PdfDict, len(cidFont))
				maps.Copy(newCID, cidFont)
				newCID["W"] = newW
				if cidRef.ObjectNum > 0 {
					alloc.AllocAt(cidRef.ObjectNum, newCID)
				}
				fixes = append(fixes, fmt.Sprintf("corrected /W widths for CIDFont %s", baseName))
			}
		}
	}

	// Fix CIDSet if incomplete.
	if _, ok := fdDict["CIDSet"]; ok {
		if fixed := fixCIDSet(ctx, alloc, fdDict, fdRef, fontData); fixed {
			fixes = append(fixes, fmt.Sprintf("regenerated /CIDSet for font %s", baseName))
		}
	}

	return fixes
}

func correctCIDWidths(ctx context.Context, doc *document.Document, wArr ast.PdfArray, binaryWidths map[uint16]uint16, upm uint16) (ast.PdfArray, bool) {
	modified := false
	newW := make(ast.PdfArray, 0, len(wArr))

	i := 0
	for i < len(wArr) {
		if i+1 >= len(wArr) {
			newW = append(newW, wArr[i:]...)
			break
		}

		startCID := int(document.GetFloat(wArr[i]))
		next := wArr[i+1]

		if nextArr, err := document.ResolveArray(ctx, doc, next); err == nil && nextArr != nil {
			corrected := make(ast.PdfArray, len(nextArr))
			for j, wObj := range nextArr {
				pdfWidth := document.GetFloat(wObj)
				cid := uint16(startCID + j) //nolint:gosec // CID range bounded
				if bw, ok := binaryWidths[cid]; ok {
					expected := float64(bw) * 1000.0 / float64(upm)
					if math.Abs(pdfWidth-expected) > 1.0 {
						corrected[j] = ast.PdfReal(math.Round(expected*100) / 100)
						modified = true
						continue
					}
				}
				corrected[j] = wObj
			}
			newW = append(newW, wArr[i], corrected)
			i += 2
		} else if i+2 < len(wArr) {
			newW = append(newW, wArr[i], wArr[i+1], wArr[i+2])
			i += 3
		} else {
			newW = append(newW, wArr[i:]...)
			break
		}
	}

	return newW, modified
}

func fixCharSet(ctx context.Context, alloc *writer.ObjAllocator, fdDict ast.PdfDict, fdRef ast.PdfIndirectRef, fontData []byte) bool {
	glyphNames := typography.ExtractGlyphNames(ctx, fontData)
	if len(glyphNames) == 0 {
		return false
	}

	names := make([]string, 0, len(glyphNames))
	for _, name := range glyphNames {
		if name != "" && name != ".notdef" {
			names = append(names, "/"+name)
		}
	}
	if len(names) == 0 {
		return false
	}

	newFD := make(ast.PdfDict, len(fdDict))
	maps.Copy(newFD, fdDict)
	newFD["CharSet"] = ast.PdfString(strings.Join(names, ""))
	if fdRef.ObjectNum > 0 {
		alloc.AllocAt(fdRef.ObjectNum, newFD)
	}
	return true
}

func fixCIDSet(ctx context.Context, alloc *writer.ObjAllocator, fdDict ast.PdfDict, fdRef ast.PdfIndirectRef, fontData []byte) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	validCIDs := typography.ExtractValidCIDs(fontData)
	if len(validCIDs) == 0 {
		return false
	}

	maxCID := uint16(0)
	for cid := range validCIDs {
		if cid > maxCID {
			maxCID = cid
		}
	}

	byteCount := int(maxCID)/8 + 1
	cidSet := make([]byte, byteCount)
	for cid := range validCIDs {
		byteIdx := int(cid) / 8
		bitIdx := 7 - (int(cid) % 8)
		if byteIdx < len(cidSet) {
			cidSet[byteIdx] |= 1 << uint(bitIdx)
		}
	}

	newStream := ast.NewStream(ast.PdfDict{}, cidSet)
	cidSetRef := alloc.Alloc(newStream)

	newFD := make(ast.PdfDict, len(fdDict))
	maps.Copy(newFD, fdDict)
	newFD["CIDSet"] = cidSetRef
	if fdRef.ObjectNum > 0 {
		alloc.AllocAt(fdRef.ObjectNum, newFD)
	}
	return true
}

func extractFontProgram(ctx context.Context, doc *document.Document, fdDict ast.PdfDict) []byte {
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3", "FontFile"} {
		obj, ok := fdDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}
		return data
	}
	return nil
}

func fontBaseName(fontDict ast.PdfDict) string {
	if bn, ok := fontDict["BaseFont"].(ast.PdfName); ok {
		return string(bn)
	}
	return "unknown"
}
