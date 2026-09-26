package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// markedLangFormsPDF builds a tagged page whose unusable marked-content /Lang
// entries sit outside the page's own content, where the compliance walk judges
// them (ISO 19005-2, 6.7.4 t1):
//
//   - object 5, a form the page draws: an inline BDC /Lang (5v).
//   - object 6, a form the page draws: a BDC naming /P1, object 9 (4w), and one
//     naming /P2, a property list written directly in the form's /Resources (3x).
//   - object 10, the normal appearance of annotation 7: an inline BDC /Lang (6u).
//   - object 8, /P0 of the page's own /Resources (2y), which the page draws.
func markedLangFormsPDF() []byte {
	form := func(resources, body string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Resources %s /Length %d >>\nstream\n%s\nendstream",
			resources, len(body), body)
	}
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R /Lang (en) /StructTreeRoot 11 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [7 0 R]"+
			" /Resources << /XObject << /Fm0 5 0 R /Fm1 6 0 R >> /Properties << /P0 8 0 R >> >> >>",
		contentObj("/Span /P0 BDC EMC /Artifact BMC /Fm0 Do /Fm1 Do EMC"),
		form("<< >>", "/Span <</Lang (5v)>> BDC EMC"),
		form("<< /Properties << /P1 9 0 R /P2 << /Lang (3x) >> >> >>", "/Span /P1 BDC EMC /Span /P2 BDC EMC"),
		"<< /Type /Annot /Subtype /Square /Rect [100 100 110 110] /F 4 /AP << /N 10 0 R >> >>",
		"<< /Lang (2y) >>",
		"<< /Lang (4w) >>",
		form("<< >>", "/Span <</Lang (6u)>> BDC EMC"),
		"<< /Type /StructTreeRoot /K 12 0 R >>",
		"<< /Type /StructElem /S /Document /P 11 0 R >>",
	)
}

// liveStreamBody returns the decoded body of the stream at objNum as the
// output will carry it.
func liveStreamBody(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document, objNum int) (ast.PdfDict, string) {
	t.Helper()

	ctx := context.Background()
	s, ok := liveStream(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
	if !ok {
		t.Fatalf("object %d is no stream", objNum)
	}
	body, err := doc.DecodeStream(ctx, s)
	if err != nil {
		t.Fatalf("decoding object %d: %v", objNum, err)
	}
	return s.Dict, string(body)
}

// assertMarkedLangsRemoved fails when any /Lang [markedLangFormsPDF] plants
// survives in the output the allocator describes, or when a stream it
// rewrote lost its marked content.
func assertMarkedLangsRemoved(t *testing.T, alloc *writer.ObjAllocator, doc *document.Document) {
	t.Helper()

	ctx := context.Background()
	for _, objNum := range []int{5, 6, 10} {
		_, body := liveStreamBody(t, alloc, doc, objNum)
		if strings.Contains(body, "/Lang") {
			t.Errorf("object %d keeps its inline /Lang: %q", objNum, body)
		}
		if !strings.Contains(body, "BDC") || !strings.Contains(body, "EMC") {
			t.Errorf("object %d lost its marked content: %q", objNum, body)
		}
	}
	for _, objNum := range []int{8, 9} {
		if list := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum}); list["Lang"] != nil {
			t.Errorf("property list %d keeps /Lang: %v", objNum, list)
		}
	}
	res := liveOwnerResources(t, alloc, doc, 6)
	p2 := liveDictValue(ctx, alloc, doc, liveDictValue(ctx, alloc, doc, res["Properties"])["P2"])
	if p2 == nil {
		t.Fatalf("/P2 is gone from the form's /Resources: %v", res)
	}
	if p2["Lang"] != nil {
		t.Errorf("the form's direct property list /P2 keeps /Lang: %v", p2)
	}
	if _, body := liveStreamBody(t, alloc, doc, 6); body != "/Span /P1 BDC EMC /Span /P2 BDC EMC" {
		t.Errorf("the form holding /P2 changed its content: %q", body)
	}
}

// TestFixMarkedContentLang_ReachesFormsAndAppearances pins that the marked-
// content /Lang repair reaches every stream the compliance walk judges.
//
// veraPDF judges the property list of every BDC operator in a page's content,
// in the forms a page or an appearance draws and in the appearance streams of
// the page's annotations, and litho's compliance walk reads the same streams
// since 8b9e9d12. The repair read the page's own content and the property
// lists of the page's /Resources alone, so a /Lang anywhere else survived
// remediation with its finding. A property list written directly in a
// /Properties dictionary was not written back either, having no object number
// of its own.
//
// # Corpus evidence
//
//   - litho mcform_2a.pdf: kept 6.7.4 t1 for the inline BDC of a form the page
//     draws and of an annotation appearance after a PDF/A-2a remediation, on
//     litho and veraPDF alike.
func TestFixMarkedContentLang_ReachesFormsAndAppearances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, markedLangFormsPDF())
	fixes, err := fixMarkedContentLang(ctx, alloc, doc, Options{}, nil, "PDF/A-2a")
	if err != nil {
		t.Fatalf("fixMarkedContentLang: %v", err)
	}
	if len(fixes) == 0 {
		t.Error("fixMarkedContentLang reported no repair")
	}
	assertMarkedLangsRemoved(t, alloc, doc)
}
