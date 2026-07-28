package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ua2StructDoc builds a PDF/UA-2 document whose structure tree declares the
// PDF 2.0 (obj 5) and PDF 1.7 (obj 6) namespaces and a single Document element
// (obj 7) whose /K is docKids. Extra structure elements are appended from obj 8.
func ua2StructDoc(t *testing.T, docKids string, elems ...string) (*document.Document, ast.PdfDict) {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /Namespaces [5 0 R 6 0 R] /K 7 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf/ssn) >>",
		"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K " + docKids + " >>",
	}
	objs = append(objs, elems...)
	return objsDocPDF(t, objs...)
}

func TestUA2NamespaceStructTypes(t *testing.T) {
	ctx := context.Background()
	check := func(docKids string, elems ...string) []Violation {
		doc, cat := ua2StructDoc(t, docKids, elems...)
		return checkPDFUA2NamespaceStructTypes(ctx, doc, cat, "PDF/UA-2")
	}

	// §8.2.5.14 t1: a standard Note (PDF 1.7 namespace) is forbidden; FENote ok.
	if got := check("8 0 R", "<< /Type /StructElem /S /Note /NS 6 0 R /P 7 0 R >>"); !hasRule(got, Rule_14289_2_8_2_5_14_t01) {
		t.Errorf("standard Note: got %v, want §8.2.5.14 t1", got)
	}
	if got := check("8 0 R", "<< /Type /StructElem /S /FENote /NS 5 0 R /P 7 0 R >>"); hasRule(got, Rule_14289_2_8_2_5_14_t01) {
		t.Errorf("FENote: got %v, want none", got)
	}

	// §8.2.5.23 t1: Ruby children must be RB,RT or RB,RP,RT,RP.
	rubyRBRT := []string{
		"<< /Type /StructElem /S /Ruby /NS 5 0 R /P 7 0 R /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /RB /NS 5 0 R /P 8 0 R >>",
		"<< /Type /StructElem /S /RT /NS 5 0 R /P 8 0 R >>",
	}
	if got := check("8 0 R", rubyRBRT...); hasRule(got, Rule_14289_2_8_2_5_23_t01) {
		t.Errorf("valid Ruby RB,RT: got %v, want none", got)
	}
	rubyBad := []string{
		"<< /Type /StructElem /S /Ruby /NS 5 0 R /P 7 0 R /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /RB /NS 5 0 R /P 8 0 R >>",
		"<< /Type /StructElem /S /P /NS 5 0 R /P 8 0 R >>",
	}
	if got := check("8 0 R", rubyBad...); !hasRule(got, Rule_14289_2_8_2_5_23_t01) {
		t.Errorf("invalid Ruby RB,P: got %v, want §8.2.5.23 t1", got)
	}

	// §8.2.5.24 t1: Warichu children must be WP,WT,WP.
	warOK := []string{
		"<< /Type /StructElem /S /Warichu /NS 5 0 R /P 7 0 R /K [9 0 R 10 0 R 11 0 R] >>",
		"<< /Type /StructElem /S /WP /NS 5 0 R /P 8 0 R >>",
		"<< /Type /StructElem /S /WT /NS 5 0 R /P 8 0 R >>",
		"<< /Type /StructElem /S /WP /NS 5 0 R /P 8 0 R >>",
	}
	if got := check("8 0 R", warOK...); hasRule(got, Rule_14289_2_8_2_5_24_t01) {
		t.Errorf("valid Warichu: got %v, want none", got)
	}
	warBad := []string{
		"<< /Type /StructElem /S /Warichu /NS 5 0 R /P 7 0 R /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /WP /NS 5 0 R /P 8 0 R >>",
		"<< /Type /StructElem /S /WT /NS 5 0 R /P 8 0 R >>",
	}
	if got := check("8 0 R", warBad...); !hasRule(got, Rule_14289_2_8_2_5_24_t01) {
		t.Errorf("invalid Warichu WP,WT: got %v, want §8.2.5.24 t1", got)
	}
}

// The two /Ref directions of ISO 14289-2, 8.2.5.14 were measured against
// veraPDF rather than read out of the clause, which names neither: t2 fires
// when an element points at the FENote and the note does not point back, t3
// when the note points at an element that does not point back. A note wired
// both ways, and a note wired neither way, both conform.
func TestUA2FENoteRefs(t *testing.T) {
	ctx := context.Background()
	check := func(docKids string, elems ...string) []Violation {
		doc, cat := ua2StructDoc(t, docKids, elems...)
		return checkPDFUA2FENoteRefs(ctx, doc, cat, "PDF/UA-2")
	}
	// Object 8 is the referring paragraph, object 9 the FENote.
	para := func(ref string) string {
		return "<< /Type /StructElem /S /P /NS 5 0 R /P 7 0 R " + ref + " >>"
	}
	note := func(ref string) string {
		return "<< /Type /StructElem /S /FENote /NS 5 0 R /P 7 0 R " + ref + " >>"
	}

	t.Run("referrer points at the note alone", func(t *testing.T) {
		got := check("[8 0 R 9 0 R]", para("/Ref [9 0 R]"), note(""))
		if !hasRule(got, Rule_14289_2_8_2_5_14_t02) {
			t.Errorf("got %v, want §8.2.5.14 t2", got)
		}
		if hasRule(got, Rule_14289_2_8_2_5_14_t03) {
			t.Errorf("got %v, want no t3", got)
		}
	})

	t.Run("note points at the referrer alone", func(t *testing.T) {
		got := check("[8 0 R 9 0 R]", para(""), note("/Ref [8 0 R]"))
		if !hasRule(got, Rule_14289_2_8_2_5_14_t03) {
			t.Errorf("got %v, want §8.2.5.14 t3", got)
		}
		if hasRule(got, Rule_14289_2_8_2_5_14_t02) {
			t.Errorf("got %v, want no t2", got)
		}
	})

	t.Run("both directions wired", func(t *testing.T) {
		if got := check("[8 0 R 9 0 R]", para("/Ref [9 0 R]"), note("/Ref [8 0 R]")); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	t.Run("neither direction wired", func(t *testing.T) {
		if got := check("[8 0 R 9 0 R]", para(""), note("")); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	t.Run("a non-FENote pair is not judged", func(t *testing.T) {
		other := "<< /Type /StructElem /S /P /NS 5 0 R /P 7 0 R /Ref [8 0 R] >>"
		if got := check("[8 0 R 9 0 R]", para(""), other); len(got) != 0 {
			t.Errorf("got %v, want none", got)
		}
	})

	t.Run("the check is PDF/UA-2 only", func(t *testing.T) {
		doc, cat := ua2StructDoc(t, "[8 0 R 9 0 R]", para("/Ref [9 0 R]"), note(""))
		if got := checkPDFUA2FENoteRefs(ctx, doc, cat, "PDF/UA-1"); len(got) != 0 {
			t.Errorf("PDF/UA-1: got %v, want none", got)
		}
	})
}
