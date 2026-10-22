package mutations

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// uaClaimXMP is an XMP packet claiming PDF/UA-1 (ISO 14289-1, 5).
const uaClaimXMP = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
<rdf:Description rdf:about="" xmlns:pdfuaid="http://www.aiim.org/pdfua/ns/id/"><pdfuaid:part>1</pdfuaid:part></rdf:Description>
</rdf:RDF></x:xmpmeta>
<?xpacket end="w"?>`

// placementFormPDF builds a page with one widget whose appearance carries the
// given /BBox and /Matrix entries, written as PDF source.
func placementFormPDF(rect, form string) []byte {
	return widgetFormPDF(rect, form, false)
}

// widgetFormPDF is [placementFormPDF], with an XMP packet claiming PDF/UA-1 when
// claim is set. The document has no structure tree either way.
func widgetFormPDF(rect, form string, claim bool) []byte {
	appearance := "0 0 1 rg 0 0 10 10 re f"
	catalog := "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>"
	if claim {
		catalog = "<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> /Metadata 6 0 R >>"
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 300] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /F 4 /Rect " + rect + " /AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form %s /Length %d >>\nstream\n%s\nendstream", form, len(appearance), appearance),
	}
	if claim {
		objs = append(objs, fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(uaClaimXMP), uaClaimXMP))
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

// TestFlattenFormPlacesByAlgorithm81 pins that a flattened widget's appearance
// lands where a viewer draws it: ISO 32000-1, 12.5.5, Algorithm 8.1 transforms
// the form's /BBox by its /Matrix and maps the box it gets onto /Rect. A plain
// translation to the first corner /Rect names drew an offset box away from the
// widget, a smaller box unscaled, and a rotated box beside it. A /Rect with no
// area scales the appearance to nothing, so the widget leaves drawing nothing.
func TestFlattenFormPlacesByAlgorithm81(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, rect, form string
		// cm is the matrix the drawing is placed with, empty when nothing is
		// drawn.
		cm string
	}{
		{name: "box at the origin sized to the rectangle", rect: "[20 200 100 260]", form: "/BBox [0 0 80 60]", cm: "1 0 0 1 20 200"},
		{name: "offset box", rect: "[120 200 200 260]", form: "/BBox [200 100 280 160]", cm: "1 0 0 1 -80 100"},
		{name: "box half the rectangle", rect: "[220 200 300 260]", form: "/BBox [0 0 40 30]", cm: "2 0 0 2 220 200"},
		{name: "box turned a quarter", rect: "[20 40 80 120]", form: "/BBox [0 0 80 60] /Matrix [0 1 -1 0 0 0]", cm: "1 0 0 1 80 40"},
		{name: "reversed corners and a translating matrix", rect: "[300 120 220 40]", form: "/BBox [0 0 80 80] /Matrix [1 0 0 1 40 -30]", cm: "1 0 0 1 180 70"},
		{name: "rectangle with no area", rect: "[350 50 350 50]", form: "/BBox [0 0 20 20]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := placementFormPDF(tc.rect, tc.form)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
			if err := FlattenForm(ctx, alloc, doc); err != nil {
				t.Fatalf("FlattenForm: %v", err)
			}

			var drawn []string
			for _, obj := range alloc.Pending() {
				s, ok := obj.(ast.PdfStream)
				if !ok {
					continue
				}
				body, err := s.Bytes(ctx)
				if err != nil {
					t.Fatalf("Bytes: %v", err)
				}
				if strings.Contains(string(body), " Do Q") {
					drawn = append(drawn, string(body))
				}
			}
			page, ok := alloc.Pending()[3].(ast.PdfDict)
			if !ok {
				t.Fatalf("page 3 not rewritten: %T", alloc.Pending()[3])
			}
			if _, kept := page["Annots"]; kept {
				t.Errorf("/Annots = %v, want the widget gone", page["Annots"])
			}

			if tc.cm == "" {
				if len(drawn) != 0 {
					t.Fatalf("drew %q, want nothing", drawn)
				}
				return
			}
			if len(drawn) != 1 {
				t.Fatalf("got %d drawings, want 1: %q", len(drawn), drawn)
			}
			if want := "q " + tc.cm + " cm /FlatAP"; !strings.Contains(drawn[0], want) {
				t.Errorf("drawing = %q, want it placed with %q", drawn[0], want)
			}
		})
	}
}

// drawnContent returns the content streams pending holds that draw a baked
// appearance.
func drawnContent(ctx context.Context, t *testing.T, pending map[int]ast.PdfObject) []string {
	t.Helper()
	var drawn []string
	for _, obj := range pending {
		s, ok := obj.(ast.PdfStream)
		if !ok {
			continue
		}
		body, err := s.Bytes(ctx)
		if err != nil {
			t.Fatalf("Bytes: %v", err)
		}
		if strings.Contains(string(body), " Do Q") {
			drawn = append(drawn, string(body))
		}
	}
	return drawn
}

// requireArtifacts fails t unless each appearance drawn holds an artifact
// marking, or, with want false, unless none does.
func requireArtifacts(t *testing.T, drawn []string, want bool) {
	t.Helper()
	if len(drawn) == 0 {
		t.Fatal("no appearance drawn")
	}
	for _, d := range drawn {
		n := strings.Count(d, " Do Q")
		if want && (n != strings.Count(d, "/Artifact BMC q ") || n != strings.Count(d, " Do Q EMC")) {
			t.Errorf("drawing = %q, want each appearance marked as an artifact", d)
		}
		if !want && strings.Contains(d, "/Artifact") {
			t.Errorf("drawing = %q, want plain content", d)
		}
	}
}

// claimedFormObjs returns certifiedFormObjs with an XMP packet claiming
// PDF/UA-1 named by the catalog.
func claimedFormObjs() []string {
	objs := certifiedFormObjs()
	objs[0] = strings.Replace(objs[0], "<< /Type /Catalog", fmt.Sprintf("<< /Type /Catalog /Metadata %d 0 R", len(objs)+1), 1)
	return append(objs, fmt.Sprintf("<< /Type /Metadata /Subtype /XML /Length %d >>\nstream\n%s\nendstream", len(uaClaimXMP), uaClaimXMP))
}

// TestFlattenFormMarksAnUntaggedDrawingByTheClaim pins how a document with no
// structure tree gets each flattened appearance. ISO 14289-1, 7.1 and ISO
// 14289-2, 8.2.2 require the page content of a document claiming conformance
// to be tagged or an artifact, so with a PDF/UA claim and no structure element
// to tag it under, the drawing is an artifact. Without one it stays plain
// content, which default text extraction returns.
func TestFlattenFormMarksAnUntaggedDrawingByTheClaim(t *testing.T) {
	t.Parallel()
	for _, claim := range []bool{false, true} {
		t.Run(fmt.Sprintf("claim=%v", claim), func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := widgetFormPDF("[20 200 100 260]", "/BBox [0 0 80 60]", claim)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
			if err := FlattenForm(ctx, alloc, doc); err != nil {
				t.Fatalf("FlattenForm: %v", err)
			}
			requireArtifacts(t, drawnContent(ctx, t, alloc.Pending()), claim)
		})
	}
}

// TestBakeSignaturesForRewriteMarksAnUntaggedDrawingByTheClaim pins the same
// for the signed signatures a full rewrite of an untagged document draws.
func TestBakeSignaturesForRewriteMarksAnUntaggedDrawingByTheClaim(t *testing.T) {
	t.Parallel()
	for _, claim := range []bool{false, true} {
		t.Run(fmt.Sprintf("claim=%v", claim), func(t *testing.T) {
			t.Parallel()
			objs := certifiedFormObjs()
			if claim {
				objs = claimedFormObjs()
			}
			ctx, doc, _ := setFormDoc(t, objs)
			overrides, err := BakeSignaturesForRewrite(ctx, doc, nil)
			if err != nil {
				t.Fatalf("BakeSignaturesForRewrite: %v", err)
			}
			requireArtifacts(t, drawnContent(ctx, t, overrides), claim)
		})
	}
}

// TestBakeSignedSignaturesMarksAnUntaggedPageCopyByTheClaim pins the same for
// the signed signatures a copy of an untagged document's pages draws.
func TestBakeSignedSignaturesMarksAnUntaggedPageCopyByTheClaim(t *testing.T) {
	t.Parallel()
	for _, claim := range []bool{false, true} {
		t.Run(fmt.Sprintf("claim=%v", claim), func(t *testing.T) {
			t.Parallel()
			objs := certifiedFormObjs()
			if claim {
				objs = claimedFormObjs()
			}
			ctx, doc, _ := setFormDoc(t, objs)
			pages := readerPages(ctx, doc, mustCatalog(t, ctx, doc))
			overrides, err := bakeSignedSignatures(ctx, doc, pages[:1], pageCopyOverrides(pages[:1]), bakeTagged)
			if err != nil {
				t.Fatalf("bakeSignedSignatures: %v", err)
			}
			requireArtifacts(t, drawnContent(ctx, t, overrides), claim)
		})
	}
}
