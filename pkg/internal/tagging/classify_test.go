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
	for _, want := range []string{
		"/H1 <</MCID 0>>", "/P <</MCID 1>>", "(Big Heading)",
		"(Body line one with several words here.)", "(Body line two with several words here.)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("marked content missing %q\n%s", want, got)
		}
	}
}

func TestAutoTag_ArtifactsNonTextContent(t *testing.T) {
	ctx := context.Background()
	// A stroked rule line sits between two text objects. The text objects
	// become structure; the stroke must be wrapped as an /Artifact so no
	// painting operator is left unmarked (ISO 14289-1, 7.1).
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

// imagePagePDF is a single page with a paragraph, an image XObject paint
// between two text objects, and an inline image, plus a form XObject
// that must NOT become a Figure.
func imagePagePDF() []byte {
	cs := "BT /F1 12 Tf 72 700 Td (Text before the image goes here.) Tj ET\n" +
		"q 100 0 0 80 72 600 cm /Im1 Do Q\n" +
		"BT /F1 12 Tf 72 500 Td (Text after the image goes here.) Tj ET\n" +
		"q /Fm1 Do Q\n" +
		"BI /W 1 /H 1 /CS /G /BPC 8 ID \x00 EI\n"
	fm := "0 0 m 10 10 l S"
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> /XObject << /Im1 6 0 R /Fm1 7 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs),
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length %d >>\nstream\n%s\nendstream", len(fm), fm),
	})
}

// TestClassify_Figures pins the Figure pass: image XObject paints and
// inline images become Figure leaves with synthesized /Alt, interleaved
// in content order; form XObjects stay untagged.
func TestClassify_Figures(t *testing.T) {
	ctx := context.Background()
	data := imagePagePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	var roles []Role
	figures := 0
	for _, k := range root.Kids {
		roles = append(roles, k.Role)
		if k.Role == RoleFigure {
			figures++
			if k.Alt == "" {
				t.Error("Figure without synthesized /Alt")
			}
			if k.Span == nil {
				t.Error("Figure without content span")
			}
		}
	}
	if figures != 2 {
		t.Fatalf("got %d Figure elements (roles %v), want 2 (image XObject + inline; the form XObject is excluded)", figures, roles)
	}
	// Content order: P, Figure(Im1), P, Figure(inline).
	want := []Role{RoleP, RoleFigure, RoleP, RoleFigure}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	for i := range want {
		if roles[i] != want[i] {
			t.Fatalf("roles = %v, want %v", roles, want)
		}
	}
}

// TestAutoTag_FigureEndToEnd runs the full pipeline on an image page and
// asserts the emitted content stream wraps the image paint in a Figure
// marked-content sequence.
func TestAutoTag_FigureEndToEnd(t *testing.T) {
	ctx := context.Background()
	data := imagePagePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(7)
	if _, err := AutoTag(ctx, alloc, doc); err != nil {
		t.Fatalf("AutoTag: %v", err)
	}

	foundFigureMark := false
	foundFigureElem := false
	for _, obj := range alloc.Pending() {
		switch v := obj.(type) {
		case ast.PdfStream:
			body, err := v.Bytes(ctx)
			if err == nil && bytes.Contains(body, []byte("/Figure <</MCID")) {
				foundFigureMark = true
			}
		case ast.PdfDict:
			if s, _ := v["S"].(ast.PdfName); s == "Figure" {
				foundFigureElem = true
				if alt, _ := v["Alt"].(ast.PdfString); alt == "" {
					t.Error("emitted Figure element lacks /Alt")
				}
			}
		}
	}
	if !foundFigureElem {
		t.Error("no Figure structure element emitted")
	}
	if !foundFigureMark {
		t.Error("no /Figure BDC mark in rewritten content")
	}
}

// TestClassify_BulletList pins the list pass: consecutive marker-starting
// lines become one L with LI > LBody per item, and the marker stays
// inside the LBody span (no Lbl split).
func TestClassify_BulletList(t *testing.T) {
	ctx := context.Background()
	cs := "BT /F1 12 Tf 72 700 Td (Intro paragraph before the list.) Tj ET\n" +
		"BT /F1 12 Tf 72 686 Td (- first item in the list) Tj ET\n" +
		"BT /F1 12 Tf 72 672 Td (- second item in the list) Tj ET\n" +
		"BT /F1 12 Tf 72 658 Td (- third item in the list) Tj ET\n" +
		"BT /F1 12 Tf 72 630 Td (Closing paragraph after the list.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}

	var roles []Role
	for _, k := range root.Kids {
		roles = append(roles, k.Role)
	}
	want := []Role{RoleP, RoleL, RoleP}
	if len(roles) != len(want) {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	list := root.Kids[1]
	if len(list.Kids) != 3 {
		t.Fatalf("list has %d items, want 3", len(list.Kids))
	}
	for i, li := range list.Kids {
		if li.Role != RoleLI || len(li.Kids) != 1 || li.Kids[0].Role != RoleLBody {
			t.Errorf("item %d: want LI > LBody, got %q > %v", i, li.Role, li.Kids)
		}
		if li.Kids[0].Span == nil {
			t.Errorf("item %d LBody has no span", i)
		}
	}
}

// TestClassify_NumberedListAndSingleDash pins the enumeration markers and
// the single-marker guard: "1." / "2)" lines form a list, while a lone
// dash-prefixed line stays a paragraph.
func TestClassify_NumberedListAndSingleDash(t *testing.T) {
	ctx := context.Background()
	cs := "BT /F1 12 Tf 72 700 Td (1. first numbered item here) Tj ET\n" +
		"BT /F1 12 Tf 72 686 Td (2\\) second numbered item here) Tj ET\n" +
		"BT /F1 12 Tf 72 640 Td (- a lone dash line is prose) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	var roles []Role
	for _, k := range root.Kids {
		roles = append(roles, k.Role)
	}
	want := []Role{RoleL, RoleP}
	if len(roles) != len(want) || roles[0] != want[0] || roles[1] != want[1] {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	if len(root.Kids[0].Kids) != 2 {
		t.Fatalf("numbered list has %d items, want 2", len(root.Kids[0].Kids))
	}
}

// TestClassify_TableGrid pins the table pass: a 3x3 grid of same-baseline
// rows with aligned columns becomes one Table with TR > TD structure,
// and surrounding paragraphs survive.
func TestClassify_TableGrid(t *testing.T) {
	ctx := context.Background()
	cs := "BT /F1 12 Tf 72 720 Td (Before the table paragraph.) Tj ET\n" +
		"BT /F1 12 Tf 72 690 Td (a1) Tj ET\nBT /F1 12 Tf 200 690 Td (b1) Tj ET\nBT /F1 12 Tf 330 690 Td (c1) Tj ET\n" +
		"BT /F1 12 Tf 72 672 Td (a2) Tj ET\nBT /F1 12 Tf 200 672 Td (b2) Tj ET\nBT /F1 12 Tf 330 672 Td (c2) Tj ET\n" +
		"BT /F1 12 Tf 72 654 Td (a3) Tj ET\nBT /F1 12 Tf 200 654 Td (b3) Tj ET\nBT /F1 12 Tf 330 654 Td (c3) Tj ET\n" +
		"BT /F1 12 Tf 72 600 Td (After the table paragraph.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	var roles []Role
	for _, k := range root.Kids {
		roles = append(roles, k.Role)
	}
	want := []Role{RoleP, RoleTable, RoleP}
	if len(roles) != len(want) || roles[0] != want[0] || roles[1] != want[1] || roles[2] != want[2] {
		t.Fatalf("roles = %v, want %v", roles, want)
	}
	table := root.Kids[1]
	if len(table.Kids) != 3 {
		t.Fatalf("table has %d rows, want 3", len(table.Kids))
	}
	for ri, tr := range table.Kids {
		if tr.Role != RoleTR || len(tr.Kids) != 3 {
			t.Fatalf("row %d: role %q with %d cells, want TR with 3", ri, tr.Role, len(tr.Kids))
		}
		for ci, td := range tr.Kids {
			if td.Role != RoleTD || td.Span == nil {
				t.Errorf("cell %d/%d: role %q span %v, want TD leaf", ri, ci, td.Role, td.Span)
			}
		}
	}
}

// TestClassify_NoTableFalseClaims pins the guards: a single header-like
// multi-column line (one row) and ordinary stacked paragraphs never
// become tables.
func TestClassify_NoTableFalseClaims(t *testing.T) {
	ctx := context.Background()
	cs := "BT /F1 12 Tf 72 720 Td (left) Tj ET\nBT /F1 12 Tf 300 720 Td (right) Tj ET\n" +
		"BT /F1 12 Tf 72 690 Td (An ordinary paragraph line follows here.) Tj ET\n" +
		"BT /F1 12 Tf 72 676 Td (Another ordinary paragraph line here.) Tj ET\n"
	data := buildTaggedTestPDF(cs)
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	root, err := Classify(ctx, doc)
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	for _, k := range root.Kids {
		if k.Role == RoleTable {
			t.Fatalf("no table may be claimed, got %v", root.Kids)
		}
	}
}
