package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// tableDoc builds a minimal document whose objects are the given body strings
// (object 1..N), returns the document, and resolves object tableObjNum as the
// Table structure element dict.
func tableDoc(t *testing.T, tableObjNum int, objs ...string) (*document.Document, ast.PdfDict) {
	t.Helper()

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
	table, err := document.ResolveDict(context.Background(), doc, ast.PdfIndirectRef{ObjectNum: tableObjNum})
	if err != nil {
		t.Fatalf("resolve table obj %d: %v", tableObjNum, err)
	}
	return doc, table
}

// TestValidateTableHeaderDetermination_ImplicitScope covers the PDF/UA-1 §7.5 t1
// vs PDF/UA-2 §8.2.5.26 t5 split for a header table whose TH cells carry no
// /Scope and whose TD cells carry no /Headers. veraPDF fires §7.5 t1 (TH must
// carry /Scope) but not §8.2.5.26 t5 (the implicit-scope model of ISO 32000-2
// §14.8.5.7 determines the headers), so litho must do the same.
func TestValidateTableHeaderDetermination_ImplicitScope(t *testing.T) {
	t.Parallel()

	// Table:
	//   row 0: TH TH   (column headers, no /Scope)
	//   row 1: TH TD   (row header + one data cell, no /Scope, no /Headers)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",                       // 1
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",               // 2
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>", // 3
		"<< /Type /StructElem /S /Table /K [5 0 R 6 0 R] >>",      // 4
		"<< /Type /StructElem /S /TR /K [7 0 R 8 0 R] >>",         // 5
		"<< /Type /StructElem /S /TR /K [9 0 R 10 0 R] >>",        // 6
		"<< /Type /StructElem /S /TH >>",                          // 7  (0,0)
		"<< /Type /StructElem /S /TH >>",                          // 8  (0,1)
		"<< /Type /StructElem /S /TH >>",                          // 9  (1,0) row header
		"<< /Type /StructElem /S /TD >>",                          // 10 (1,1) data cell
	}

	fires := func(t *testing.T, tableObjs []string, profile string) bool {
		t.Helper()
		doc, table := tableDoc(t, 4, tableObjs...)
		var results []Violation
		state := &walkState{doc: doc, results: &results, profile: profile}
		validateTableHeaderDetermination(context.Background(), state, table)
		wantRule := Rule_14289_2_8_2_5_26_t05
		if profile == "PDF/UA-1" {
			wantRule = Rule_14289_1_7_5_t01
		}
		return hasRule(results, wantRule)
	}

	if !fires(t, objs, "PDF/UA-1") {
		t.Error("PDF/UA-1: a TD with no determinable header (no /Scope, no /Headers) must fire §7.5 t1")
	}
	if fires(t, objs, "PDF/UA-2") {
		t.Error("PDF/UA-2: implicit scope must connect the TD, so §8.2.5.26 t5 must not fire")
	}

	// Explicit mode: one TH carries /Scope, so the table is no longer a simple
	// implicit-header table. The TD at (1,1) whose only positional headers are
	// unscoped THs must fire §8.2.5.26 t5 even under PDF/UA-2.
	//   row 0: TH(/Scope Column) TH(no scope)
	//   row 1: TH(no scope)      TD
	mixed := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /Table /K [5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /TR /K [7 0 R 8 0 R] >>",
		"<< /Type /StructElem /S /TR /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /TH /A << /O /Table /Scope /Column >> >>", // 7  (0,0) scoped
		"<< /Type /StructElem /S /TH >>",                                   // 8  (0,1) unscoped
		"<< /Type /StructElem /S /TH >>",                                   // 9  (1,0) unscoped
		"<< /Type /StructElem /S /TD >>",                                   // 10 (1,1) data cell
	}
	if !fires(t, mixed, "PDF/UA-2") {
		t.Error("PDF/UA-2 explicit mode: a TD whose positional headers are all unscoped must fire §8.2.5.26 t5")
	}
}
