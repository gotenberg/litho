package remediation

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
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

// fixCIDSystemInfo forces every Type-0 font to use /Identity-H (or
// /Identity-V for vertical-writing CIDFonts). ISO 32000-1 §9.7.2.1
// exempts those two encodings from the CIDSystemInfo equivalence rule,
// so relabelling sidesteps Registry/Ordering/Supplement mismatches
// without touching the CMap or CIDFont dictionaries.
func fixCIDSystemInfo(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	count := 0
	aligned := 0
	wmode := 0
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
			// A real CMap cannot be relabelled, but the CIDFont's declared
			// character collection can be brought into agreement with it.
			aligned += alignCIDSystemInfo(ctx, alloc, doc, fontDict, profile)
			wmode += alignCMapWMode(ctx, alloc, doc, fontDict)
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
// entry is a description of it, and 6-2-10-3-3-t02-fail-a declares WMode 1 over
// a program that says 0. The body is carried across untouched, so only the
// description changes.
func alignCMapWMode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fontDict ast.PdfDict) int {
	encRef, isRef := fontDict["Encoding"].(ast.PdfIndirectRef)
	if !isRef || encRef.ObjectNum == 0 {
		return 0
	}
	stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"])
	if err != nil {
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
	sysRef, isSysRef := descDict["CIDSystemInfo"].(ast.PdfIndirectRef)
	sysDict, err := document.ResolveDict(ctx, doc, descDict["CIDSystemInfo"])
	if err != nil || sysDict == nil {
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
