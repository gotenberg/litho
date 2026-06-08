package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// ocDocPDF builds a minimal single-page PDF whose catalog carries the given
// /OCProperties dictionary text, plus any extra indirect objects (e.g. OCG
// dicts, referenced as "4 0 R", "5 0 R", ...) appended after the page. Object 1
// is the catalog.
func ocDocPDF(t *testing.T, ocProps string, extraObjs ...string) (*document.Document, ast.PdfDict) {
	t.Helper()

	catalog := "<< /Type /Catalog /Pages 2 0 R"
	if ocProps != "" {
		catalog += " /OCProperties " + ocProps
	}
	catalog += " >>"

	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	objs = append(objs, extraObjs...)

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
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

func TestForbiddenOCProperties_A1(t *testing.T) {
	// PDF/A-1 forbids optional content: any catalog /OCProperties is a violation.
	if v := checkForbiddenOCProperties(ast.PdfDict{"OCProperties": ast.PdfDict{}}); len(v) != 1 || v[0].Rule != Rule_19005_1_6_1_13_t01 {
		t.Errorf("with /OCProperties: got %v, want one §6.1.13 t1", v)
	}
	if v := checkForbiddenOCProperties(ast.PdfDict{}); len(v) != 0 {
		t.Errorf("without /OCProperties: got %v, want none", v)
	}
}

func TestOCConfigNames_MissingName(t *testing.T) {
	ctx := context.Background()
	doc, cat := ocDocPDF(t, "<< /D << >> >>")
	for part, want := range map[int]Rule{
		2: Rule_19005_2_6_9_t01,
		3: Rule_19005_3_6_9_t01,
		4: Rule_19005_4_6_10_t01,
	} {
		got := checkOCConfigNamesPDFA(ctx, doc, cat, part)
		if len(got) != 1 || got[0].Rule != want {
			t.Errorf("part %d missing /Name: got %v, want one %v", part, got, want)
		}
	}
}

func TestOCConfigNames_EmptyName(t *testing.T) {
	ctx := context.Background()
	doc, cat := ocDocPDF(t, "<< /D << /Name () >> >>")

	// PDF/A-2/3 require a non-empty name (§6.9 t1).
	for part, want := range map[int]Rule{2: Rule_19005_2_6_9_t01, 3: Rule_19005_3_6_9_t01} {
		got := checkOCConfigNamesPDFA(ctx, doc, cat, part)
		if len(got) != 1 || got[0].Rule != want {
			t.Errorf("part %d empty /Name: got %v, want one %v", part, got, want)
		}
	}

	// PDF/A-4 requires only presence (§6.10 t1): an empty name is conformant.
	if got := checkOCConfigNamesPDFA(ctx, doc, cat, 4); len(got) != 0 {
		t.Errorf("part 4 empty /Name: got %v, want none", got)
	}
}

func TestOCConfigNames_DuplicateName(t *testing.T) {
	ctx := context.Background()
	doc, cat := ocDocPDF(t, "<< /D << /Name (X) >> /Configs [4 0 R] >>", "<< /Name (X) >>")
	for part, want := range map[int]Rule{
		2: Rule_19005_2_6_9_t02,
		3: Rule_19005_3_6_9_t02,
		4: Rule_19005_4_6_10_t02,
	} {
		got := checkOCConfigNamesPDFA(ctx, doc, cat, part)
		if len(got) != 1 || got[0].Rule != want {
			t.Errorf("part %d duplicate /Name: got %v, want one %v", part, got, want)
		}
	}
}

func TestOCConfigNames_Valid(t *testing.T) {
	ctx := context.Background()
	doc, cat := ocDocPDF(t, "<< /D << /Name (Default) >> /Configs [4 0 R] >>", "<< /Name (Alt) >>")
	for _, part := range []int{2, 3, 4} {
		if got := checkOCConfigNamesPDFA(ctx, doc, cat, part); len(got) != 0 {
			t.Errorf("part %d valid configs: got %v, want none", part, got)
		}
	}
}

func TestOCGOrderCompleteness(t *testing.T) {
	ctx := context.Background()
	ocg1 := "<< /Type /OCG /Name (L1) >>"
	ocg2 := "<< /Type /OCG /Name (L2) >>"

	// /Order references only OCG 4, omitting OCG 5: incomplete.
	doc, cat := ocDocPDF(t, "<< /OCGs [4 0 R 5 0 R] /D << /Name (D) /Order [4 0 R] >> >>", ocg1, ocg2)
	for part, want := range map[int]Rule{
		2: Rule_19005_2_6_9_t03,
		3: Rule_19005_3_6_9_t03,
		4: Rule_19005_4_6_10_t03,
	} {
		got := checkOCGOrderCompleteness(ctx, doc, cat, part)
		if len(got) != 1 || got[0].Rule != want {
			t.Errorf("part %d incomplete /Order: got %v, want one %v", part, got, want)
		}
	}

	// /Order references both OCGs: complete.
	docOK, catOK := ocDocPDF(t, "<< /OCGs [4 0 R 5 0 R] /D << /Name (D) /Order [4 0 R 5 0 R] >> >>", ocg1, ocg2)
	for _, part := range []int{2, 3, 4} {
		if got := checkOCGOrderCompleteness(ctx, docOK, catOK, part); len(got) != 0 {
			t.Errorf("part %d complete /Order: got %v, want none", part, got)
		}
	}
}
