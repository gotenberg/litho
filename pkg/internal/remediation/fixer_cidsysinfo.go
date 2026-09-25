package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// matchCIDSystemInfo matches composite-font CIDSystemInfo mismatches
// reported by ISO 19005-1 §6.3.3.1, ISO 19005-2/3 §6.2.11.3.1 and
// ISO 19005-4 §6.2.10.3.1.
func matchCIDSystemInfo(v compliance.Violation) bool {
	// An embedded CMap whose dictionary contradicts its own program is the same
	// kind of disagreement, settled the same way (ISO 19005-1, 6.3.3.3 t2 /
	// ISO 19005-4, 6.2.10.3.3 t2 / ISO 19005-2, 6.2.11.3.3 t2 /
	// ISO 14289-1, 7.21.3.3 t2 / ISO 14289-2, 8.4.5.4 t2).
	if clausePrefix(v, "6.3.3.3", "6.2.10.3.3", "6.2.11.3.3", "7.21.3.3", "8.4.5.4") {
		return contextContains(v, "WMode")
	}
	// PDF/UA restates the CIDSystemInfo relationship word for word
	// (ISO 14289-1, 7.21.3.1 t1 / ISO 14289-2, 8.4.5.3.1 t1), and the repair is
	// the same one. Claiming only the PDF/A clauses left the alignment fixer
	// idle on every PDF/UA document that needed it.
	//
	// ISO 19005-1 states both requirements at 6.3.3.1 t1 and 6.3.3.3 t2, and
	// they were omitted here while the PDF/UA restatements were added. The
	// checker reports them (fonts_cmap.go:34 passes the part-1 rule), so the
	// fixer sat idle on every PDF/A-1 document that needed it.
	if !clausePrefix(v, "6.3.3.1", "6.2.10.3.1", "6.2.11.3.1", "7.21.3.1", "8.4.5.3.1") {
		return false
	}
	return contextContains(v, "CIDFont") || contextContains(v, "CMap")
}

// fixCIDSystemInfo labels every Type 0 font whose codes are already its CIDs
// with /Identity-H or /Identity-V, the writing mode [chooseIdentityEncoding]
// keeps. ISO 32000-1 §9.7.2.1 exempts those two encodings from the
// CIDSystemInfo equivalence rule, so relabelling sidesteps
// Registry/Ordering/Supplement mismatches without touching the CMap or CIDFont
// dictionaries.
//
// Fonts held in an object stream (ISO 32000-1, 7.5.7) are repaired too, as
// [fixCIDToGIDMap] repairs them: the update defines the object normally in a
// later cross-reference section, which supersedes the copy inside the object
// stream (7.5.8). Every dictionary written back is read through the allocator
// first, so an earlier fixer's edit, or the alignment of another font sharing
// the object, is kept.
//
// # Corpus evidence
//
//   - pdfjs issue18117.pdf: embeds /Uni-Utf8-H, whose codes are one to four
//     bytes, and relabelling it produced an odd-length code tail and glyphs
//     that resolve to .notdef (ISO 19005-2, 6.2.11.8 t1 and 6.2.11.4.1 t2).
//   - All corpora: no font or CMap this fixer rewrites is queued by an earlier
//     fixer first, at PDF/A-1b, 2b, 4 or PDF/UA-1, so reading through the
//     allocator changes no output.
//   - All corpora: 133 files hold a Type 0 font in an object stream, and none
//     carries a CIDSystemInfo mismatch this fixer can settle, so reaching them
//     changes no residual violation at PDF/A-1b or 2b, with or without the
//     aggressive fixes.
func fixCIDSystemInfo(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	// Sorted, since Index.Entries is a map: two fonts sharing a CIDFont or a
	// CMap each read the other's pending edit, so the order decides the
	// output.
	nums := make([]int, 0, len(doc.Index.Entries))
	for objNum := range doc.Index.Entries {
		nums = append(nums, objNum)
	}
	slices.Sort(nums)

	count := 0
	aligned := 0
	wmode := 0
	for _, objNum := range nums {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		entry := doc.Index.Entries[objNum]
		if entry.Type == xref.XRefTypeFree || objNum == 0 {
			continue
		}
		// Alloc-first: the relabelled font is written back whole, and earlier
		// fixers (name-utf8, font-keys, fonts) rewrite Type 0 font
		// dictionaries too.
		obj, pending := alloc.Pending()[objNum]
		if !pending {
			resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
			if err != nil || resolved == nil {
				continue
			}
			obj = resolved
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
		// reinterprets every byte the content stream already holds, which can
		// leave an odd-length code tail and glyphs that resolve to .notdef
		// (ISO 19005-2, 6.2.11.8 t1 and 6.2.11.4.1 t2). An unfixed
		// CIDSystemInfo mismatch is an honest residual; rewritten text is not
		// recoverable.
		if !encodingIsIdentityCompatible(ctx, doc, fontDict) {
			// A real CMap cannot be relabelled, but the CIDFont's declared
			// character collection can be brought into agreement with it.
			aligned += alignCIDSystemInfo(ctx, alloc, doc, fontDict, profile)
			wmode += alignCMapWMode(ctx, alloc, doc, fontDict)
			continue
		}

		enc := chooseIdentityEncoding(ctx, doc, fontDict)
		// Resolved rather than asserted direct: an /Encoding held as an
		// indirect name already names this CMap, and the exemption holds
		// however the name is written.
		if current, err := document.ResolveName(ctx, doc, fontDict["Encoding"]); err == nil && current == enc {
			continue
		}
		newFont := make(ast.PdfDict, len(fontDict))
		maps.Copy(newFont, fontDict)
		newFont["Encoding"] = enc
		alloc.AllocAt(objNum, newFont)
		count++
	}
	var fixes []string
	if count > 0 {
		fixes = append(fixes, fmt.Sprintf("normalised Type0 /Encoding to /Identity on %d font(s)", count))
	}
	if aligned > 0 {
		fixes = append(fixes, fmt.Sprintf("aligned /CIDSystemInfo with the CMap on %d CIDFont(s)", aligned))
	}
	if wmode > 0 {
		fixes = append(fixes, fmt.Sprintf("set /WMode from the CMap program on %d CMap(s)", wmode))
	}
	return fixes, nil
}

// alignCMapWMode sets an embedded CMap dictionary's /WMode to the value the
// CMap program itself declares, returning 1 when it rewrote the stream.
//
// ISO 19005-4, 6.2.10.3.3 test 2 requires the two to be identical. The program
// is what a viewer executes, so it is the value that survives; the dictionary
// entry is only a description of it. The body is carried across untouched, so
// only the description changes.
//
// # Corpus evidence
//
//   - verapdf 6-2-10-3-3-t02-fail-a: declares WMode 1 over a program that says
//     0.
func alignCMapWMode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fontDict ast.PdfDict) int {
	encRef, isRef := fontDict["Encoding"].(ast.PdfIndirectRef)
	if !isRef || encRef.ObjectNum == 0 {
		return 0
	}
	// Alloc-first, since the whole stream is written back: name-utf8 may
	// already have queued this CMap with a repaired /CMapName.
	stream, ok := liveStream(ctx, alloc, doc, encRef)
	if !ok {
		return 0
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return 0
	}
	programWMode := max(compliance.ExtractCMapStreamWMode(data),
		// The program declares none, so the default applies and there is
		// nothing to disagree with.
		0)
	dictWMode, err := document.ResolveInt(ctx, doc, stream.Dict["WMode"])
	if err != nil || int(dictWMode) == programWMode {
		return 0
	}

	newDict := make(ast.PdfDict, len(stream.Dict))
	maps.Copy(newDict, stream.Dict)
	newDict["WMode"] = ast.PdfInt(programWMode)
	raw, err := stream.Bytes(ctx)
	if err != nil {
		doc.Logger.Debug("remediation: cannot carry a CMap body across for /WMode",
			"obj", encRef.ObjectNum, "error", err)
		return 0
	}
	alloc.AllocAt(encRef.ObjectNum, ast.NewStream(newDict, raw))
	return 1
}

// alignCIDSystemInfo brings a CIDFont's /CIDSystemInfo into the agreement with
// its CMap that ISO 19005-2/3, 6.2.11.3.1 and ISO 19005-4, 6.2.10.3.1 require,
// returning 1 when it rewrote the CIDFont.
//
// Two disagreements are safe to settle, because neither changes which glyph a
// content byte selects:
//
//   - /Registry or /Ordering differing only in letter case. The clause wants
//     the values identical, and "adobe" against "Adobe" is a producer typo
//     naming the same collection, not a different one.
//   - a /Supplement above the CMap's. A supplement is cumulative, and only the
//     CIDs the CMap reaches can be selected, so the CMap's value is the honest
//     upper bound.
//
// A genuine difference (Japan1 against GB1) is left alone: the font and the
// CMap really do disagree, and overwriting the declaration would assert a
// collection the glyphs do not come from.
func alignCIDSystemInfo(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fontDict ast.PdfDict, profile string) int {
	cmapReg, cmapOrd, cmapSup, ok := cmapCIDSystemInfo(ctx, doc, fontDict, profile)
	if !ok {
		return 0
	}

	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return 0
	}
	descRef, isRef := descArr[0].(ast.PdfIndirectRef)
	if !isRef || descRef.ObjectNum == 0 {
		return 0
	}
	descDict := resolveFromAllocOrDoc(ctx, alloc, doc, descRef)
	if descDict == nil {
		return 0
	}
	// Alloc-first, since the patched dict is written back whole: a second
	// Type 0 font sharing this CIDSystemInfo object may already have queued
	// it.
	sysRef, isSysRef := descDict["CIDSystemInfo"].(ast.PdfIndirectRef)
	sysDict := liveDictValue(ctx, alloc, doc, descDict["CIDSystemInfo"])
	if sysDict == nil {
		return 0
	}

	patched := make(ast.PdfDict, len(sysDict))
	maps.Copy(patched, sysDict)
	changed := false

	for key, want := range map[ast.PdfName]string{"Registry": cmapReg, "Ordering": cmapOrd} {
		have, err := document.ResolveString(ctx, doc, sysDict[key])
		if err != nil {
			continue
		}
		got := document.DecodePdfString(have)
		if got != want && strings.EqualFold(got, want) {
			patched[key] = ast.PdfString(want)
			changed = true
		}
	}
	if have, err := document.ResolveInt(ctx, doc, sysDict["Supplement"]); err == nil && int(have) > cmapSup {
		patched["Supplement"] = ast.PdfInt(cmapSup)
		changed = true
	}
	if !changed {
		return 0
	}

	// The dict may be direct in the CIDFont or an object of its own.
	if isSysRef && sysRef.ObjectNum > 0 {
		alloc.AllocAt(sysRef.ObjectNum, patched)
		return 1
	}
	newDesc := make(ast.PdfDict, len(descDict))
	maps.Copy(newDesc, descDict)
	newDesc["CIDSystemInfo"] = patched
	alloc.AllocAt(descRef.ObjectNum, newDesc)
	return 1
}

// cmapCIDSystemInfo returns the character collection a Type 0 font's CMap
// declares, from the embedded stream's dict or from the tabulated resource of a
// predefined name. It asks [cmap.PredefinedCMapCIDSystemInfo] exactly as the
// detector does, so the fixer cannot align towards a different answer.
func cmapCIDSystemInfo(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, profile string) (registry, ordering string, supplement int, ok bool) {
	encObj, present := fontDict["Encoding"]
	if !present {
		return "", "", 0, false
	}
	if stream, err := document.ResolveStream(ctx, doc, encObj); err == nil {
		sysDict, err := document.ResolveDict(ctx, doc, stream.Dict["CIDSystemInfo"])
		if err != nil || sysDict == nil {
			return "", "", 0, false
		}
		reg, rErr := document.ResolveString(ctx, doc, sysDict["Registry"])
		ord, oErr := document.ResolveString(ctx, doc, sysDict["Ordering"])
		sup, sErr := document.ResolveInt(ctx, doc, sysDict["Supplement"])
		if rErr != nil || oErr != nil || sErr != nil {
			return "", "", 0, false
		}
		return document.DecodePdfString(reg), document.DecodePdfString(ord), int(sup), true
	}
	encName, err := document.ResolveName(ctx, doc, encObj)
	if err != nil || encName == "" {
		return "", "", 0, false
	}
	// ISO 32000-2 widened several predefined CMaps to newer collections, so the
	// tabulated answer depends on the generation the profile targets.
	pdf20 := compliance.PDFAPart(profile) == 4 ||
		(compliance.IsPDFUA(profile) && compliance.PDFUAPart(profile) == 2)
	return cmap.PredefinedCMapCIDSystemInfo(string(encName), pdf20)
}

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

// chooseIdentityEncoding returns the Identity CMap a Type 0 font is labelled
// with.
//
// A font already on /Identity-H or /Identity-V keeps it. The CMap sets the
// writing mode: /WMode is an entry of the CMap (ISO 32000-1, 9.7.5.3, Table
// 120), and the CIDFont's /W2 and /DW2 only give the vertical metrics a
// vertical CMap uses (9.7.4.3). Both Identity CMaps are exempt from the
// CIDSystemInfo rule, so swapping one for the other repairs nothing and turns
// vertical text horizontal, or the reverse. A vertical CIDFont with no /W2 or
// /DW2 is a legal one that takes the default vertical metrics.
//
// Only a font with no /Encoding, which ISO 32000-1, Table 121 requires, has
// no writing mode to keep. There the descendant's vertical metrics are the one
// sign of the producer's intent: /Identity-V when it declares /W2 or /DW2,
// /Identity-H otherwise.
//
// # Corpus evidence
//
//   - pdfjs vertical.pdf, verapdf 6-2-11-3-1-t01-pass-c, 6-2-10-3-1-t01-pass-c:
//     hold an /Identity-V font with no /W2 or /DW2, the shape the metrics
//     alone relabelled /Identity-H. None reports a CIDSystemInfo mismatch that
//     runs the repair, so no corpus output changes.
//   - pdfjs issue11922_reduced.pdf, issue13823.pdf, issue19319.pdf,
//     issue19360.pdf: hold an /Identity-H font whose CIDFont declares /W2 or
//     /DW2, the reverse case, and none runs the repair either.
func chooseIdentityEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfName {
	if current, err := document.ResolveName(ctx, doc, fontDict["Encoding"]); err == nil &&
		(current == "Identity-H" || current == "Identity-V") {
		return current
	}
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
