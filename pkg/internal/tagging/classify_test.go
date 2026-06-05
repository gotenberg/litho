package tagging

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

// headingPagePDF is a single page with one large heading line and two smaller
// body lines, each its own text object.
func headingPagePDF() []byte {
	cs := "BT /F1 24 Tf 72 700 Td (Big Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 686 Td (Body line one with several words here.) Tj ET\n" +
		"BT /F1 12 Tf 72 672 Td (Body line two with several words here.) Tj ET\n"
	return buildTaggedTestPDF(cs)
}

func TestClassify_HeadingAndParagraphs(t *testing.T) {
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(headingPagePDF()), int64(len(headingPagePDF())), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if root.Role != RoleDocument {
		t.Fatalf("root role: got %q, want Document", root.Role)
	}

	// The heading is its own H1; the two close body lines merge into one P.
	wantRoles := []Role{RoleH1, RoleP}
	if len(root.Kids) != len(wantRoles) {
		t.Fatalf("got %d elements, want %d: %+v", len(root.Kids), len(wantRoles), root.Kids)
	}
	var prevEnd int
	for i, k := range root.Kids {
		if k.Role != wantRoles[i] {
			t.Errorf("element %d role: got %q, want %q", i, k.Role, wantRoles[i])
		}
		if k.Span == nil {
			t.Fatalf("element %d has no span", i)
		}
		if k.Span.Start < prevEnd {
			t.Errorf("element %d span %+v overlaps previous (ended %d)", i, k.Span, prevEnd)
		}
		prevEnd = k.Span.End
	}
}

func TestClassify_MergesAndBreaks(t *testing.T) {
	ctx := context.Background()
	// A heading, then two paragraphs of two lines each separated by a wide gap.
	// Expect H1 + P(2 lines) + P(2 lines): adjacent lines merge, the gap breaks.
	cs := "BT /F1 24 Tf 72 720 Td (Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 700 Td (Para one line one here.) Tj ET\n" +
		"BT /F1 12 Tf 72 686 Td (Para one line two here.) Tj ET\n" +
		"BT /F1 12 Tf 72 640 Td (Para two line one here.) Tj ET\n" +
		"BT /F1 12 Tf 72 626 Td (Para two line two here.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	wantRoles := []Role{RoleH1, RoleP, RoleP}
	if len(root.Kids) != len(wantRoles) {
		t.Fatalf("got %d elements, want %d", len(root.Kids), len(wantRoles))
	}
	for i, k := range root.Kids {
		if k.Role != wantRoles[i] {
			t.Errorf("element %d: got %q, want %q", i, k.Role, wantRoles[i])
		}
	}
}

func TestAutoTag_EndToEnd(t *testing.T) {
	ctx := context.Background()
	data := headingPagePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(5)

	descs, err := AutoTag(ctx, alloc, doc)
	if err != nil {
		t.Fatalf("AutoTag: %v", err)
	}
	if len(descs) == 0 {
		t.Fatal("AutoTag produced no descriptions")
	}

	pending := alloc.Pending()
	catalog := mustDict(t, pending[1], "catalog")
	stRef := mustRef(t, catalog["StructTreeRoot"], "catalog /StructTreeRoot")
	st := mustDict(t, pending[stRef.ObjectNum], "StructTreeRoot")
	docElem := mustDict(t, pending[mustRef(t, st["K"], "/K").ObjectNum], "Document")

	// The heading is one H1; the two close body lines merge into one P.
	kids, ok := docElem["K"].(ast.PdfArray)
	if !ok || len(kids) != 2 {
		t.Fatalf("Document /K: got %v, want 2 children", docElem["K"])
	}
	wantRoles := []ast.PdfName{"H1", "P"}
	for i, kref := range kids {
		leaf := mustDict(t, pending[mustRef(t, kref, "kid").ObjectNum], "leaf")
		if leaf["S"] != wantRoles[i] {
			t.Errorf("leaf %d /S: got %v, want %v", i, leaf["S"], wantRoles[i])
		}
	}

	// The rewritten content stream carries one H1 and one P marked sequence,
	// and the merged P wraps both body lines.
	page := mustDict(t, pending[3], "page")
	cs := pending[mustRef(t, page["Contents"], "/Contents").ObjectNum].(ast.PdfStream)
	marked, _ := cs.Bytes(ctx)
	got := string(marked)
	if n := strings.Count(got, ">> BDC\n"); n != 2 {
		t.Errorf("want 2 structure BDC, got %d:\n%s", n, got)
	}
	for _, want := range []string{"/H1 <</MCID 0>>", "/P <</MCID 1>>", "(Big Heading)",
		"(Body line one with several words here.)", "(Body line two with several words here.)"} {
		if !strings.Contains(got, want) {
			t.Errorf("marked content missing %q\n%s", want, got)
		}
	}
}

func TestAutoTag_ArtifactsNonTextContent(t *testing.T) {
	ctx := context.Background()
	// A stroked rule line sits between two text objects. The text objects
	// become structure; the stroke must be wrapped as an /Artifact so no
	// painting operator is left unmarked (ISO 14289-1 §7.1).
	cs := "BT /F1 24 Tf 72 700 Td (Heading) Tj ET\n" +
		"72 695 m 540 695 l S\n" +
		"BT /F1 12 Tf 72 670 Td (Body text here now.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(5)
	if _, err := AutoTag(ctx, alloc, doc); err != nil {
		t.Fatalf("AutoTag: %v", err)
	}

	pending := alloc.Pending()
	page := mustDict(t, pending[3], "page")
	cstream := pending[mustRef(t, page["Contents"], "/Contents").ObjectNum].(ast.PdfStream)
	marked, _ := cstream.Bytes(ctx)
	got := string(marked)

	// Two structure marks (H1 heading + P body).
	if n := strings.Count(got, ">> BDC\n"); n != 2 {
		t.Errorf("structure BDC count: got %d, want 2\n%s", n, got)
	}
	// The stroke is wrapped in an artifact.
	ai := strings.Index(got, "/Artifact BMC")
	if ai < 0 {
		t.Fatalf("stroke not artifacted; no /Artifact BMC found\n%s", got)
	}
	ei := strings.Index(got[ai:], "EMC")
	if ei < 0 || !strings.Contains(got[ai:ai+ei], " l S") {
		t.Errorf("stroke operators not inside the artifact region\n%s", got)
	}
}

func TestAutoTag_StrokeBlocksMergeAndStaysArtifact(t *testing.T) {
	ctx := context.Background()
	// Two body lines that are vertically adjacent (would merge) but with a
	// stroked rule between them. The merge must be refused so the stroke is
	// not swallowed into a paragraph, and the stroke must be artifacted.
	cs := "BT /F1 12 Tf 72 700 Td (Body line one here now.) Tj ET\n" +
		"72 692 m 540 692 l S\n" +
		"BT /F1 12 Tf 72 686 Td (Body line two here now.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	// Classification keeps the two paragraphs separate.
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(root.Kids) != 2 {
		t.Fatalf("got %d elements, want 2 (merge must be blocked by the stroke)", len(root.Kids))
	}

	alloc := writer.NewObjAllocator(5)
	if _, err := AutoTag(ctx, alloc, doc); err != nil {
		t.Fatalf("AutoTag: %v", err)
	}
	page := mustDict(t, alloc.Pending()[3], "page")
	cstream := alloc.Pending()[mustRef(t, page["Contents"], "/Contents").ObjectNum].(ast.PdfStream)
	marked, _ := cstream.Bytes(ctx)
	got := string(marked)
	if n := strings.Count(got, ">> BDC\n"); n != 2 {
		t.Errorf("structure BDC: got %d, want 2 (two unmerged P)\n%s", n, got)
	}
	ai := strings.Index(got, "/Artifact BMC")
	if ai < 0 || !strings.Contains(got[ai:ai+strings.Index(got[ai:], "EMC")], " l S") {
		t.Errorf("stroke not artifacted between the two paragraphs\n%s", got)
	}
}

func TestAutoTag_SkipsAlreadyTagged(t *testing.T) {
	ctx := context.Background()
	cs := "BT /F1 12 Tf 72 700 Td (already tagged) Tj ET\n"
	data := assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs),
	})
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(4)
	descs, err := AutoTag(ctx, alloc, doc)
	if err != nil {
		t.Fatalf("AutoTag: %v", err)
	}
	if len(descs) != 0 || len(alloc.Pending()) != 0 {
		t.Errorf("AutoTag mutated an already-tagged doc: descs=%v, pending=%d", descs, len(alloc.Pending()))
	}
}

func TestClassify_AllBodyNoHeading(t *testing.T) {
	ctx := context.Background()
	// Uniform 12pt text: no block exceeds body * 1.15, so everything is P.
	cs := "BT /F1 12 Tf 72 700 Td (Line one here.) Tj ET\n" +
		"BT /F1 12 Tf 72 680 Td (Line two here.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	for i, k := range root.Kids {
		if k.Role != RoleP {
			t.Errorf("element %d: got %q, want P (uniform text)", i, k.Role)
		}
	}
}
