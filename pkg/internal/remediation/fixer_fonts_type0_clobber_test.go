package remediation

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// type0WidthViolation is the ISO 19005-2, 6.2.11.5 width mismatch that turns
// on the /W repair in fixFontIssues.
func type0WidthViolation() []compliance.Violation {
	return []compliance.Violation{{
		Rule: compliance.Rule{Specification: "ISO_19005_2", Clause: "6.2.11.5", TestNumber: 1},
	}}
}

// type0FontProgram returns object bodies 6 and 7 of the fixtures below: a font
// descriptor whose /FontName is fontName, carrying the extra entries, and the
// TrueType program it embeds, which advances GID 1 by 500 where every
// fixture's /W declares 999.
func type0FontProgram(fontName, extra string) []string {
	program := probeTrueType(500)
	return []string{
		"<< /Type /FontDescriptor /FontName /" + fontName + " /Flags 4 /FontFile2 7 0 R" + extra + " >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(program), program),
	}
}

// type0CIDFont is an inline CIDFontType2 dictionary named baseFont whose /W
// disagrees with the program in [type0FontProgram].
func type0CIDFont(baseFont string) string {
	return "<< /Type /Font /Subtype /CIDFontType2 /BaseFont /" + baseFont +
		" /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>" +
		" /FontDescriptor 6 0 R /W [1 [999]] >>"
}

// correctedWidth returns the width /W declares for CID 1 in a CIDFont
// dictionary, or -1 when the array does not have the fixtures' shape.
func correctedWidth(cid ast.PdfDict) float64 {
	w, ok := cid["W"].(ast.PdfArray)
	if !ok || len(w) != 2 {
		return -1
	}
	widths, ok := w[1].(ast.PdfArray)
	if !ok || len(widths) != 1 {
		return -1
	}
	return document.Float(widths[0])
}

// TestFixType0Font_KeepsAPeerFixersPendingType0Write pins that the /W repair of
// a CIDFont with no object number of its own rebuilds its container from the
// copy an earlier fixer queued.
//
// persistCIDFont writes the corrected CIDFont at the innermost enclosing object
// that exists: the /DescendantFonts array when that is indirect, the Type 0
// font otherwise. fixFontIssues read the font with document.ResolveDict and
// fixType0Font read the array the same way, so the container went back out as
// the on-disk copy. The two peers are real and run earlier: font-keys adds the
// /Type /Font a Type 0 font lacks (ISO 19005-2, 6.2.11.2), and name-utf8
// repairs a /BaseFont that is not valid UTF-8 (ISO 19005-2, 6.1.8), including
// one inside an indirect /DescendantFonts array.
func TestFixType0Font_KeepsAPeerFixersPendingType0Write(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		// font and array are the bodies of objects 4 and 5.
		font, array string
		peer        fixer
		// written is the object both fixers write, and check reports whether
		// the peer's edit is still there, along with the CIDFont it holds.
		written int
		check   func(ast.PdfObject) (ast.PdfDict, string)
	}{
		{
			name: "font-keys, CIDFont inline in an inline array",
			font: "<< /Subtype /Type0 /BaseFont /Probe /Encoding /Identity-H " +
				"/DescendantFonts [" + type0CIDFont("Probe") + "] >>",
			array:   "<< /Type /Nothing >>",
			peer:    fixer{name: "font-keys", apply: fixFontKeys},
			written: 4,
			check: func(obj ast.PdfObject) (ast.PdfDict, string) {
				font, _ := obj.(ast.PdfDict)
				arr, _ := font["DescendantFonts"].(ast.PdfArray)
				var cid ast.PdfDict
				if len(arr) > 0 {
					cid, _ = arr[0].(ast.PdfDict)
				}
				if font["Type"] != ast.PdfName("Font") {
					return cid, "/Type /Font is gone from the Type 0 font"
				}
				return cid, ""
			},
		},
		{
			name: "name-utf8, CIDFont inline in an indirect array",
			font: "<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding /Identity-H " +
				"/DescendantFonts 5 0 R >>",
			array:   "[" + type0CIDFont("Bad#80Name") + "]",
			peer:    fixer{name: "name-utf8", apply: fixNameUTF8},
			written: 5,
			check: func(obj ast.PdfObject) (ast.PdfDict, string) {
				arr, _ := obj.(ast.PdfArray)
				var cid ast.PdfDict
				if len(arr) > 0 {
					cid, _ = arr[0].(ast.PdfDict)
				}
				if cid["BaseFont"] != ast.PdfName("Bad_Name") {
					return cid, fmt.Sprintf("the CIDFont /BaseFont is %q again", cid["BaseFont"])
				}
				return cid, ""
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F0 4 0 R >> >> >>",
				tc.font,
				tc.array,
			}, type0FontProgram("Probe", "")...)
			doc, alloc := newTestDoc(t, buildStructPDF(objs...))

			written := tc.written
			if _, err := tc.peer.apply(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
				t.Fatalf("%s: %v", tc.peer.name, err)
			}
			if _, msg := tc.check(alloc.Pending()[written]); msg != "" {
				t.Fatalf("control: the peer did not queue its edit at object %d (%s), so there is "+
					"no earlier edit to lose", written, msg)
			}

			if _, err := fixFontIssues(ctx, alloc, doc, Options{}, type0WidthViolation(), "PDF/A-2b"); err != nil {
				t.Fatalf("fixFontIssues: %v", err)
			}

			cid, msg := tc.check(alloc.Pending()[written])
			if w := correctedWidth(cid); w != 500 {
				t.Fatalf("CID 1 is %v wide in object %d, want 500: the /W repair did not land "+
					"there, so the check below proves nothing", w, written)
			}
			if msg != "" {
				t.Errorf("%s: fixFontIssues rebuilt object %d from the document and discarded "+
					"the earlier fixer's write", msg, written)
			}
		})
	}
}

// TestFixCIDSet_KeepsTheNameFixersPendingDescriptorWrite pins that the /CIDSet
// regeneration rewrites the font descriptor name-utf8 queued, not the
// document's copy.
//
// fixCIDSet and fixCharSet write the whole descriptor back at its own object
// number. fixType0Font and fixSimpleFont read that descriptor with
// document.ResolveDict, so name-utf8's repair of a /FontName that is not valid
// UTF-8 (ISO 19005-2, 6.1.8) was discarded. Font descriptors are among the
// objects name-utf8 rewrites most, since /FontName repeats the /BaseFont.
func TestFixCIDSet_KeepsTheNameFixersPendingDescriptorWrite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	objs := append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F0 4 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type0 /BaseFont /Probe /Encoding /Identity-H /DescendantFonts [5 0 R] >>",
		type0CIDFont("Probe"),
	}, type0FontProgram("Bad#80Name", " /CIDSet 8 0 R")...)
	// Object 8: a /CIDSet that misses CID 1, which the program defines.
	objs = append(objs, "<< /Length 1 >>\nstream\n\x00\nendstream")
	doc, alloc := newTestDoc(t, buildStructPDF(objs...))

	const fdObj = 6

	if _, err := fixNameUTF8(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNameUTF8: %v", err)
	}
	if got := pendingDict(t, alloc, fdObj)["FontName"]; got != ast.PdfName("Bad_Name") {
		t.Fatalf("control: name-utf8 queued /FontName %v at object %d, want /Bad_Name, so there "+
			"is no earlier edit to lose", got, fdObj)
	}

	violations := []compliance.Violation{{
		Rule: compliance.Rule{Specification: "ISO_19005_2", Clause: "6.2.11.4.2", TestNumber: 2},
	}}
	if _, err := fixFontIssues(ctx, alloc, doc, Options{}, violations, "PDF/A-2b"); err != nil {
		t.Fatalf("fixFontIssues: %v", err)
	}

	got := pendingDict(t, alloc, fdObj)
	if ref, ok := got["CIDSet"].(ast.PdfIndirectRef); !ok || ref.ObjectNum == 8 {
		t.Fatalf("/CIDSet is %v: the regeneration did not rewrite the descriptor, so the check "+
			"below proves nothing", got["CIDSet"])
	}
	if name := got["FontName"]; name != ast.PdfName("Bad_Name") {
		t.Errorf("/FontName is %q, want /Bad_Name: fixCIDSet rewrote the document's descriptor "+
			"and discarded name-utf8's write", name)
	}
}
