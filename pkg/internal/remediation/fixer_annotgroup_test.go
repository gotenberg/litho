package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixAnnotation_AppearanceStreamsOwnGroupIsRemoved covers the location
// where real files hold ISO 19005-1 §6.4 test 3: the appearance stream itself.
//
// An appearance stream is a Form XObject (ISO 32000-1, 12.5.5), so a /Group
// with /S /Transparency on it is forbidden exactly as on any other form. The
// previous walk repaired the graphics states inside the appearance's
// /Resources but never the group on the stream itself, which left the rule
// standing on every file that hides its group there.
//
// # Corpus evidence
//
//   - All corpora: the carriers of ISO 19005-1 §6.4 test 3 hold the group on
//     the appearance stream itself.
func TestFixAnnotation_AppearanceStreamsOwnGroupIsRemoved(t *testing.T) {
	doc, alloc := newTestDoc(t, annotGroupPDF(`/AP << /N 20 0 R >>`))

	annots := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 15}}
	n := fixAnnotationTransparency(context.Background(), doc, alloc, annots, make(map[int]bool))
	if n == 0 {
		t.Fatal("a transparency group on an appearance stream must be removed")
	}

	ap, ok := pendingStream(t, alloc, 20)
	if !ok {
		t.Fatalf("obj %v was never written", 20)
	}
	if _, still := ap.Dict["Group"]; still {
		t.Error("/Group survived on the appearance stream")
	}
	if _, ok := ap.Dict["BBox"]; !ok {
		t.Error("the rewrite dropped /BBox: sibling keys must survive")
	}
}

// TestFixAnnotation_StateSubdictionaryIsWalked pins the shape the walk skipped
// wholesale.
//
// ISO 32000-1, 12.5.5: /N may hold a subdictionary of named appearance states
// rather than a stream. Resolving it as a stream yields nothing and the whole
// appearance was silently ignored, groups and graphics states alike.
func TestFixAnnotation_StateSubdictionaryIsWalked(t *testing.T) {
	doc, alloc := newTestDoc(t, annotGroupPDF(`/AP << /N << /Off 20 0 R /On 21 0 R >> >>`))

	annots := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 15}}
	if n := fixAnnotationTransparency(context.Background(), doc, alloc, annots, make(map[int]bool)); n < 2 {
		t.Fatalf("both appearance states must be repaired, counted %d", n)
	}
	for _, objNum := range []int{20, 21} {
		ap, ok := pendingStream(t, alloc, objNum)
		if !ok {
			t.Fatalf("obj %v was never written", objNum)
		}
		if _, still := ap.Dict["Group"]; still {
			t.Errorf("/Group survived on appearance state obj %d", objNum)
		}
	}
}

// TestFixAnnotation_DownAndRolloverAreWalked covers /D and /R.
//
// This test is the ONLY evidence for that branch. Recorded as such rather than
// left to a gate that cannot reach it.
//
// # Corpus evidence
//
//   - All corpora: no corpus fixture uses /D or /R.
func TestFixAnnotation_DownAndRolloverAreWalked(t *testing.T) {
	for _, which := range []string{"D", "R"} {
		t.Run("/"+which, func(t *testing.T) {
			doc, alloc := newTestDoc(t, annotGroupPDF(`/AP << /`+which+` 20 0 R >>`))

			annots := ast.PdfArray{ast.PdfIndirectRef{ObjectNum: 15}}
			if n := fixAnnotationTransparency(context.Background(), doc, alloc, annots, make(map[int]bool)); n == 0 {
				t.Fatalf("/%s appearance must be repaired", which)
			}
			ap, ok := pendingStream(t, alloc, 20)
			if !ok {
				t.Fatalf("obj %v was never written", 20)
			}
			if _, still := ap.Dict["Group"]; still {
				t.Errorf("/Group survived on the /%s appearance", which)
			}
		})
	}
}

// TestFixAnnotation_SharedAppearanceIsWrittenOnce pins the visited guard.
//
// Two annotations may reference one appearance object, and without the shared
// visited set the walk repairs it twice.
//
// MEASURED LIMIT, recorded because the first version of this test overclaimed.
// Re-walking this shape is IDEMPOTENT: the second pass rebuilds its working
// copy from the same original dictionary and applies the same repairs, so the
// emitted file is correct either way. Deleting the guard was mutation-tested
// and the assertions on /Group and /ca still passed. What doubles is the
// COUNT, and that is what this test detects. Real content loss needs two
// DIFFERENT repairs racing on one object; no fixture in this package
// exercises that, so the guard's stronger justification is unproven here.
func TestFixAnnotation_SharedAppearanceIsWrittenOnce(t *testing.T) {
	doc, alloc := newTestDoc(t, sharedAppearancePDF())

	annots := ast.PdfArray{
		ast.PdfIndirectRef{ObjectNum: 15},
		ast.PdfIndirectRef{ObjectNum: 16},
	}
	// One /Group removed plus one inline /ca normalised, counted once: 2.
	// Without the guard the second annotation repeats both and reports 4.
	if n := fixAnnotationTransparency(context.Background(), doc, alloc, annots, make(map[int]bool)); n != 2 {
		t.Errorf("shared appearance counted %d repairs, want 2: it was walked twice", n)
	}

	ap, ok := pendingStream(t, alloc, 20)
	if !ok {
		t.Fatalf("obj %v was never written", 20)
	}
	if _, still := ap.Dict["Group"]; still {
		t.Error("/Group survived: the second walk overwrote the first repair")
	}
	gs, _ := ap.Dict["Resources"].(ast.PdfDict)
	gsDict, _ := gs["ExtGState"].(ast.PdfDict)
	if g, ok := gsDict["GS0"].(ast.PdfDict); ok && g["ca"] != ast.PdfReal(1.0) {
		t.Errorf("/ca = %v, want 1: the inline repair was discarded", g["ca"])
	}
}

// TestFixXObject_NestedFormGroupIsRemoved pins recursion one level deeper.
//
// A group may sit on a form INSIDE a form's resources. The walk previously
// repaired the outer form's graphics states and stopped, so the nested group
// survived.
//
// # Corpus evidence
//
//   - verapdf 6-2-4-3-t04-pass-f: holds its group on a form INSIDE a form's
//     resources.
func TestFixXObject_NestedFormGroupIsRemoved(t *testing.T) {
	doc, alloc := newTestDoc(t, nestedFormGroupPDF())
	pageRes := ast.PdfDict{"XObject": ast.PdfDict{"Fm0": ast.PdfIndirectRef{ObjectNum: 20}}}

	if n := fixXObjectTransparency(context.Background(), doc, alloc, pageRes, make(map[int]bool), 0); n == 0 {
		t.Fatal("a group on a nested form must be removed")
	}

	inner, ok := pendingStream(t, alloc, 21)
	if !ok {
		t.Fatalf("obj %v was never written", 21)
	}
	if _, still := inner.Dict["Group"]; still {
		t.Error("/Group survived on the nested form")
	}
}

// annotGroupPDF builds a one-page PDF whose single annotation (obj 15) carries
// the given /AP, with appearance streams 20 and 21 that each declare a
// transparency group and an inline graphics state.
func annotGroupPDF(ap string) []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [15 0 R] >>\nendobj\n" +
			"15 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] " + ap + " >>\nendobj\n" +
			"20 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Group << /S /Transparency >> " +
			"/Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"21 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Group << /S /Transparency >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"trailer\n<< /Size 22 /Root 1 0 R >>\n%%EOF\n")
}

// sharedAppearancePDF points two annotations at one appearance object.
func sharedAppearancePDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [15 0 R 16 0 R] >>\nendobj\n" +
			"15 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AP << /N 20 0 R >> >>\nendobj\n" +
			"16 0 obj\n<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /AP << /N 20 0 R >> >>\nendobj\n" +
			"20 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Group << /S /Transparency >> " +
			"/Resources << /ExtGState << /GS0 << /ca 0.5 >> >> >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"trailer\n<< /Size 21 /Root 1 0 R >>\n%%EOF\n")
}

// nestedFormGroupPDF puts the transparency group on a form declared inside another
// form's resources.
//
// # Corpus evidence
//
//   - verapdf 6-2-4-3-t04-pass-f: the fixture mirrors its shape.
func nestedFormGroupPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /XObject << /Fm0 20 0 R >> >> >>\nendobj\n" +
			"20 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Resources << /XObject << /Fm1 21 0 R >> >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"21 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] " +
			"/Group << /S /Transparency >> /Length 0 >>\nstream\n\nendstream\nendobj\n" +
			"trailer\n<< /Size 22 /Root 1 0 R >>\n%%EOF\n")
}
