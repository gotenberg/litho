package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixExtGState_FullyInlineChainIsWritten covers the shape that defeated the
// first Type 3 transparency repair.
//
// ISO 32000-1, 7.3.7 lets /Resources, /ExtGState and the graphics state
// parameter dictionary all be direct objects, and the repair wrote nothing for
// such a chain because it could only rewrite an indirect /GS entry or an
// indirect /ExtGState. The enclosing Type 3 font object is the nearest
// indirect object, so the repair has to be written there.
//
// The assertion is on the OWNING OBJECT's contents, not on the count the fixer
// returns: the count was always correct, which is precisely why four green
// tests coexisted with a repair that never wrote.
//
// # Corpus evidence
//
//   - verapdf: 14 of the 16 §6.4 carriers write the whole chain direct, and
//     the repair wrote nothing for every one of them.
func TestFixExtGState_FullyInlineChainIsWritten(t *testing.T) {
	for _, tc := range []struct {
		name string
		gs   string
		key  ast.PdfName
		want ast.PdfObject
	}{
		{"ca inline (6.4 t6)", "/ca 0.5", "ca", ast.PdfReal(1.0)},
		{"CA inline (6.4 t5)", "/CA 0.5", "CA", ast.PdfReal(1.0)},
		{"BM inline (6.4 t4)", "/BM /Darken", "BM", ast.PdfName("Normal")},
		{"SMask inline (6.4 t1)", "/SMask /Alpha", "SMask", ast.PdfName("None")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, alloc := newTestDoc(t, inlineType3PDF(tc.gs))
			pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 10}}}

			if n := fixType3FontTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0); n == 0 {
				t.Fatal("a fully inline ExtGState chain must still be repaired")
			}

			// Object 10 is the Type 3 font: the nearest indirect ancestor.
			font := pendingDict(t, alloc, 10)
			if font == nil {
				t.Fatal("the owning font object was never written")
			}
			if _, ok := font["CharProcs"]; !ok {
				t.Error("the rewrite dropped /CharProcs: sibling keys must survive")
			}
			res, ok := font["Resources"].(ast.PdfDict)
			if !ok {
				t.Fatalf("font /Resources is %T, want an inline dict", font["Resources"])
			}
			gsDict, ok := res["ExtGState"].(ast.PdfDict)
			if !ok {
				t.Fatalf("/ExtGState is %T, want an inline dict", res["ExtGState"])
			}
			gs, ok := gsDict["GS0"].(ast.PdfDict)
			if !ok {
				t.Fatalf("/GS0 is %T, want an inline dict", gsDict["GS0"])
			}
			if got := gs[tc.key]; got != tc.want {
				t.Errorf("/%s = %v, want %v", tc.key, got, tc.want)
			}
		})
	}
}

// TestFixExtGState_InlineChainWithoutOwnerIsNotCounted pins the honest count.
//
// With no indirect ancestor to write through there is no way to persist the
// repair, and the fixer must report 0 rather than claim a repair it could not
// make. The previous implementation returned a non-zero count here.
func TestFixExtGState_InlineChainWithoutOwnerIsNotCounted(t *testing.T) {
	doc, alloc := newTestDoc(t, minimalCatalogPDF(""))
	resources := ast.PdfDict{
		"ExtGState": ast.PdfDict{"GS0": ast.PdfDict{"ca": ast.PdfReal(0.5)}},
	}

	n, pending, dirty := fixExtGStateDict(context.Background(), doc, alloc, resources, nil)
	if n != 0 {
		t.Errorf("an unwritable repair must count 0, got %d", n)
	}
	if pending != 0 {
		t.Errorf("nothing can be pending on an owner write that cannot happen, got %d", pending)
	}
	if dirty {
		t.Error("no owner was supplied, so none can be dirty")
	}
	if len(alloc.Pending()) != 0 {
		t.Errorf("nothing may be queued, got %d objects", len(alloc.Pending()))
	}
}

// TestFixExtGState_EveryInlineEntryIsKept pins that several modified inline
// entries survive together. Rebuilding the parent once per entry would keep
// only the last one, and with map iteration order that failure is
// intermittent rather than reproducible.
func TestFixExtGState_EveryInlineEntryIsKept(t *testing.T) {
	doc, alloc := newTestDoc(t, inlineType3PDFMulti())
	pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 10}}}

	if n := fixType3FontTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0); n < 2 {
		t.Fatalf("both inline entries must be repaired, counted %d", n)
	}

	font := pendingDict(t, alloc, 10)
	res, _ := font["Resources"].(ast.PdfDict)
	gsDict, ok := res["ExtGState"].(ast.PdfDict)
	if !ok {
		t.Fatalf("/ExtGState is %T, want an inline dict", res["ExtGState"])
	}
	for _, name := range []ast.PdfName{"GS0", "GS1"} {
		gs, ok := gsDict[name].(ast.PdfDict)
		if !ok {
			t.Fatalf("/%s is %T, want an inline dict", name, gsDict[name])
		}
		if got := gs["ca"]; got != ast.PdfReal(1.0) {
			t.Errorf("/%s /ca = %v, want 1", name, got)
		}
	}
}

// TestFixType3CharProcs_GlyphStreamResourcesAreRepaired covers the shape that
// survived two gates.
//
// A Type 3 font may carry non-conforming /CA values both in its own
// /Resources and in its /CharProcs glyph streams, which are Form XObjects with
// their own resource dictionaries. Repairing only the font leaves the glyph
// streams behind. litho's detector shares the blind spot, so litho reported
// such a file clean while veraPDF still failed it.
//
// The font here declares BOTH its own /Resources and glyph resources: an
// earlier `continue` on the font's /Resources meant /CharProcs was never
// reached when the font had one.
//
// # Corpus evidence
//
//   - verapdf 6-2-10-t04-fail-b: holds FOUR non-conforming /CA values, one in
//     the Type 3 font's own /Resources (obj 10) and one in each of its three
//     /CharProcs glyph streams (obj 16, 17, 18). Repairing only the font left
//     three behind, and litho reported the file clean while veraPDF still
//     failed it, which drove the corpus disagreement metric up rather than
//     down.
func TestFixType3CharProcs_GlyphStreamResourcesAreRepaired(t *testing.T) {
	doc, alloc := newTestDoc(t, type3CharProcPDF())
	pageRes := ast.PdfDict{"Font": ast.PdfDict{"F1": ast.PdfIndirectRef{ObjectNum: 10}}}

	if n := fixType3FontTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0); n < 2 {
		t.Fatalf("font and glyph resources must both be repaired, counted %d", n)
	}

	font := pendingDict(t, alloc, 10)
	if font == nil {
		t.Fatal("the font object carrying its own /CA was never written")
	}
	fres, _ := font["Resources"].(ast.PdfDict)
	fgs, _ := fres["ExtGState"].(ast.PdfDict)
	if gs, ok := fgs["GS0"].(ast.PdfDict); !ok || gs["CA"] != ast.PdfReal(1.0) {
		t.Errorf("font /Resources /GS0 /CA = %v, want 1", fgs["GS0"])
	}

	// Each glyph stream is its own indirect object and must be rewritten.
	for _, objNum := range []int{16, 17} {
		pending, ok := alloc.Pending()[objNum]
		if !ok {
			t.Errorf("glyph stream obj %d was never written", objNum)
			continue
		}
		stream, ok := pending.(ast.PdfStream)
		if !ok {
			t.Errorf("obj %d is %T, want a stream: writing a bare dict would drop the glyph body", objNum, pending)
			continue
		}
		res, _ := stream.Dict["Resources"].(ast.PdfDict)
		gsDict, _ := res["ExtGState"].(ast.PdfDict)
		gs, ok := gsDict["GS0"].(ast.PdfDict)
		if !ok {
			t.Errorf("obj %d /GS0 is %T, want an inline dict", objNum, gsDict["GS0"])
			continue
		}
		if gs["CA"] != ast.PdfReal(1.0) {
			t.Errorf("obj %d /GS0 /CA = %v, want 1", objNum, gs["CA"])
		}
		if _, hasBBox := stream.Dict["BBox"]; !hasBBox {
			t.Errorf("obj %d lost /BBox: sibling keys must survive the rewrite", objNum)
		}
	}
}

// type3CharProcPDF builds a Type 3 font with its own /Resources AND /CharProcs
// glyph streams that carry their own, every level stored inline.
//
// # Corpus evidence
//
//   - verapdf 6-2-10-t04-fail-b: the fixture mirrors its shape.
func type3CharProcPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /Font /Subtype /Type3 /CharProcs 13 0 R " +
			"/Resources << /ExtGState << /GS0 << /CA 0.5 >> >> >> >>\nendobj\n" +
			"13 0 obj\n<< /square 16 0 R /triangle 17 0 R >>\nendobj\n" +
			"16 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Resources << /ExtGState << /GS0 << /CA 0.5 >> >> >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"17 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Resources << /ExtGState << /GS0 << /CA 0.5 >> >> >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"trailer\n<< /Size 18 /Root 1 0 R >>\n%%EOF\n")
}

// inlineType3PDF builds a Type 3 font (obj 10) that declares /Resources
// inline, whose dict declares /ExtGState inline, and whose /GS0 is an inline
// dictionary too, so nothing below the font object is indirect.
//
// # Corpus evidence
//
//   - All corpora: this is the shape the corpus carries.
func inlineType3PDF(gs string) []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /Font /Subtype /Type3 /CharProcs << /a 11 0 R >> " +
			"/Resources << /ExtGState << /GS0 << " + gs + " >> >> >> >>\nendobj\n" +
			"11 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"trailer\n<< /Size 12 /Root 1 0 R >>\n%%EOF\n")
}

// inlineType3PDFMulti is the same shape with two non-conforming inline entries.
func inlineType3PDFMulti() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 10 0 R >> >> >>\nendobj\n" +
			"10 0 obj\n<< /Type /Font /Subtype /Type3 /CharProcs << /a 11 0 R >> " +
			"/Resources << /ExtGState << /GS0 << /ca 0.5 >> /GS1 << /ca 0.25 >> >> >> >>\nendobj\n" +
			"11 0 obj\n<< /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"trailer\n<< /Size 12 /Root 1 0 R >>\n%%EOF\n")
}
