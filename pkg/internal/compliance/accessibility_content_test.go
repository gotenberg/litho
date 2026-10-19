package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// contentMarkingDoc builds a minimal tagged single-page PDF whose page content
// stream is the given operator string. The catalog declares /MarkInfo and a
// StructTreeRoot whose ParentTree maps the page's MCID 0 to the Document
// element, which reaches the root, so an /MCID 0 sequence tags its content.
func contentMarkingDoc(t *testing.T, content string) (*document.Document, ast.PdfDict) {
	t.Helper()

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /StructParents 0 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /P 5 0 R /K [0] /Pg 3 0 R >>",
		"<< /Nums [0 [6 0 R]] >>",
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)

	data := buf.Bytes()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	cat, err := document.ResolveCatalog(context.Background(), doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	return doc, cat.Dict
}

// TestContentMarking_UntaggedScope covers ISO 14289-1 §7.1 t3: real content is
// tagged only when an enclosing marked-content sequence carries an /MCID. A BDC
// with a structure tag but no /MCID does not tag its content.
func TestContentMarking_UntaggedScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	firesT3 := func(t *testing.T, content string) bool {
		t.Helper()
		doc, cat := contentMarkingDoc(t, content)
		for _, v := range checkContentMarking(ctx, doc, cat, "PDF/UA-1") {
			if v.Rule == Rule_14289_1_7_1_t03 {
				return true
			}
		}
		return false
	}

	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{"tag without MCID wrapping painting is untagged", "/P <<>> BDC\n0 0 10 10 re\nf\nEMC", true},
		{"bare BMC wrapping painting is untagged", "/Foo BMC\n0 0 10 10 re\nf\nEMC", true},
		{"painting with no marked content is untagged", "0 0 10 10 re\nf", true},
		{"tag with MCID is tagged", "/P <</MCID 0>> BDC\n0 0 10 10 re\nf\nEMC", false},
		{"Artifact scope is exempt", "/Artifact <<>> BDC\n0 0 10 10 re\nf\nEMC", false},
		{"Do referencing an absent XObject paints nothing", "q\n/X0 Do\nQ", false},
		{"non-MCID BDC nested in MCID BDC stays tagged", "/P <</MCID 0>> BDC\n/Span <<>> BDC\n0 0 10 10 re\nf\nEMC\nEMC", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesT3(t, tc.content); got != tc.want {
				t.Errorf("checkContentMarking t3 = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestContentMarking_UnclosedSequence covers content after a BMC or BDC that
// no EMC in the stream closes. ISO 32000-1, 14.6 delimits a marked-content
// sequence by BMC or BDC and the matching EMC, so an operator in a sequence
// the stream never closes is in none, and veraPDF groups every operator from
// the last point the stream left all sequences closed into one unmarked group
// (GFPDSemanticContentStream.getContentItem). An EMC lexed inside a literal
// or hex string that never terminates closes nothing.
func TestContentMarking_UnclosedSequence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	untagged := func(t *testing.T, content, profile string, rule Rule) int {
		t.Helper()
		doc, cat := contentMarkingDoc(t, content)
		n := 0
		for _, v := range checkContentMarking(ctx, doc, cat, profile) {
			if v.Rule == rule {
				n++
			}
		}
		return n
	}

	tests := []struct {
		name    string
		content string
		want    int
	}{
		{"closed artifact", "/Artifact BMC\n0 0 m 10 10 l S\nEMC", 0},
		{"artifact never closed", "/Artifact BMC\n0 0 m 10 10 l S", 1},
		{"EMC swallowed by an unterminated string", "/Artifact BMC\n0 0 m 10 10 l S\n(dangling ( string\nEMC", 1},
		{"EMC swallowed by an unterminated hex string", "/Artifact BMC\n0 0 m 10 10 l S\n<0A1B\nEMC", 1},
		{"second artifact never closed", "/Artifact BMC\n0 0 m 10 10 l S\nEMC\n/Artifact BMC\n0 0 m 5 5 l S\n0 0 m 6 6 l S", 2},
		{"outer artifact never closed around a closed one", "/Artifact BMC\n/Artifact BMC\n0 0 m 10 10 l S\nEMC\n0 0 m 5 5 l S", 2},
		{"surplus EMC closes nothing", "/Artifact BMC\n0 0 m 10 10 l S\nEMC\nEMC", 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := untagged(t, tc.content, "PDF/UA-1", Rule_14289_1_7_1_t03); got != tc.want {
				t.Errorf("PDF/UA-1 7.1 t3 items = %d, want %d", got, tc.want)
			}
			if got := untagged(t, tc.content, "PDF/UA-2", Rule_14289_2_8_2_2_t01); got != tc.want {
				t.Errorf("PDF/UA-2 8.2.2 t1 items = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestContentMarking_DoResourceScope covers ISO 14289-1 §7.1 t3 for a `Do`
// whose XObject resolution depends on resource inheritance: a page with no
// /Resources inherits the Pages-node resources (so the Do paints and fires),
// while a page with its own (empty) /Resources does not inherit (so the Do is a
// no-op and is exempt).
func TestContentMarking_DoResourceScope(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	content := "q\n/X0 Do\nQ"
	contentObj := fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
	form := "<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 14 >>\nstream\n0 0 10 10 re f\nendstream"

	// Page inherits /X0 (an untagged Form painting a square) from the Pages
	// node: the Do paints real, untagged content -> §7.1 t3 fires.
	inheritDoc, inheritCat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /XObject << /X0 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		contentObj,
		"<< /Type /StructTreeRoot /K [] >>",
		form,
	)
	if !hasRule(checkContentMarking(ctx, inheritDoc, inheritCat, "PDF/UA-1"), Rule_14289_1_7_1_t03) {
		t.Errorf("inherited-XObject Do: want §7.1 t3, got none")
	}

	// Page declares its own empty /Resources, so it does not inherit; /X0 is
	// absent, the Do is a no-op, and §7.1 t3 must not fire.
	absentDoc, absentCat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /XObject << /X0 6 0 R >> >> >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> >>",
		contentObj,
		"<< /Type /StructTreeRoot /K [] >>",
		form,
	)
	if hasRule(checkContentMarking(ctx, absentDoc, absentCat, "PDF/UA-1"), Rule_14289_1_7_1_t03) {
		t.Errorf("absent-XObject Do (own empty /Resources): want none, got §7.1 t3")
	}
}

// TestContentMarking_TaggedArtifactOverlap covers ISO 14289-1 §7.1 t2 for a
// content item painted while marked both as tagged (an enclosing MCID) and as an
// Artifact: veraPDF reports it as tagged-content-inside-an-artifact even when the
// Artifact sequence is nested inside the tagged one.
func TestContentMarking_TaggedArtifactOverlap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	firesT2 := func(content string) bool {
		doc, cat := contentMarkingDoc(t, content)
		for _, v := range checkContentMarking(ctx, doc, cat, "PDF/UA-1") {
			if v.Rule == Rule_14289_1_7_1_t02 {
				return true
			}
		}
		return false
	}
	if !firesT2("/Span <</MCID 0>> BDC\n/Artifact <<>> BDC\n0 0 10 10 re\nf\nEMC\nEMC") {
		t.Error("tagged > artifact > painting: want §7.1 t2")
	}
	if firesT2("/Artifact <<>> BDC\n0 0 10 10 re\nf\nEMC") {
		t.Error("artifact-only painting: want no §7.1 t2")
	}
	if firesT2("/Span <</MCID 0>> BDC\n0 0 10 10 re\nf\nEMC") {
		t.Error("tagged-only painting: want no §7.1 t2")
	}
}
