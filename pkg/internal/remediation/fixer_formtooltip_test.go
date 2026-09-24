package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// widgetAltPDF builds a single-page PDF holding one merged widget/field whose
// /StructParent keys the /ParentTree (object 7) to a structure element
// (object 5). altEntry is spliced into that element, so a test either gives it
// an /Alt or leaves it bare.
func widgetAltPDF(altEntry string) []byte {
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Form /P 4 0 R"+altEntry+" >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /T (field name) /StructParent 0 >>",
		"<< /Nums [0 5 0 R] >>",
	)
}

// TestFixFormTooltip_ContentsMatchesEnclosingAlt pins the repair of a
// regression the fixer itself caused: writing the field name into /Contents to
// satisfy ISO 14289-2 §8.10.2.3 violated §8.9.4.2 test 1, which requires
// /Contents and the enclosing structure element's /Alt to hold identical
// values.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.18.1-t03-{fail-c,pass-b}.pdf: remediation cleared
//     all six original violations and created this one.
func TestFixFormTooltip_ContentsMatchesEnclosingAlt(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, widgetAltPDF(" /Alt (spoken label)"))

	descs, err := fixFormTooltip(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2")
	if err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}
	if len(descs) != 1 {
		t.Fatalf("expected 1 description, got %v", descs)
	}

	field := pendingDict(t, alloc, 6)
	if got, want := field["Contents"], ast.PdfString("spoken label"); got != want {
		t.Errorf("/Contents: got %q, want %q (the enclosing element's /Alt)", got, want)
	}
	// /TU keeps naming the field: §8.9.4.2 constrains /Contents against /Alt
	// and says nothing about the tooltip.
	if got, want := field["TU"], ast.PdfString("field name"); got != want {
		t.Errorf("/TU: got %q, want %q", got, want)
	}
}

// TestFixFormTooltip_ContentsFallsBackToFieldName keeps the original behaviour
// wherever there is no /Alt to agree with, so the §8.10.2.3 repair still lands.
func TestFixFormTooltip_ContentsFallsBackToFieldName(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, widgetAltPDF(""))

	if _, err := fixFormTooltip(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}

	field := pendingDict(t, alloc, 6)
	if got, want := field["Contents"], ast.PdfString("field name"); got != want {
		t.Errorf("/Contents without an enclosing /Alt: got %q, want %q", got, want)
	}
}

// TestFixFormTooltip_EmptyAltIsCopied covers the shape that took two attempts:
// a widget enclosed in a /Form element whose /Alt is the EMPTY string.
// Treating empty as "no /Alt" falls back to the field name and recreates the
// §8.9.4.2 mismatch; skipping /Contents altogether instead leaves §8.10.2.3 t1
// unfixed, because that clause wants the entry present when no Lbl labels the
// widget and is not answered by /TU. Copying the empty value satisfies both
// clauses at once.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.18.1-t03-fail-c.pdf: encloses its widget in a /Form
//     element whose /Alt is the EMPTY string.
func TestFixFormTooltip_EmptyAltIsCopied(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, widgetAltPDF(" /Alt ()"))

	if _, err := fixFormTooltip(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}

	field := pendingDict(t, alloc, 6)
	got, present := field["Contents"]
	if !present {
		t.Fatal("/Contents absent: §8.10.2.3 t1 needs the entry present")
	}
	if want := ast.PdfString(""); got != want {
		t.Errorf("/Contents against an empty enclosing /Alt: got %q, want %q", got, want)
	}
	if tu, want := field["TU"], ast.PdfString("field name"); tu != want {
		t.Errorf("/TU: got %q, want %q", tu, want)
	}
}

// TestFixFormTooltip_IndirectAlt covers an enclosing /Alt written as an
// indirect reference rather than a direct string.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.18.1-t03-pass-b.pdf: its /Alt is an indirect
//     reference, the other shape the corpus carries.
func TestFixFormTooltip_IndirectAlt(t *testing.T) {
	t.Parallel()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [6 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
		"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Form /P 4 0 R /Alt 8 0 R >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /T (field name) /StructParent 0 >>",
		"<< /Nums [0 5 0 R] >>",
		"(spoken label)",
	))

	if _, err := fixFormTooltip(context.Background(), alloc, doc, Options{}, nil, "PDF/UA-2"); err != nil {
		t.Fatalf("fixFormTooltip: %v", err)
	}

	field := pendingDict(t, alloc, 6)
	if got, want := field["Contents"], ast.PdfString("spoken label"); got != want {
		t.Errorf("/Contents from an indirect /Alt: got %q, want %q", got, want)
	}
}
