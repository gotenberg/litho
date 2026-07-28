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

// TestTableCellIntersection covers ISO 14289-1 §7.2 t15: a table cell shall not
// intersect another cell. A cell takes the first free slot of its row, so a
// /RowSpan carried down from an earlier row SHIFTS the following row's cells
// rather than colliding with them. An intersection needs a cell wide or tall
// enough to reach past a free slot into a claimed one. veraPDF 1.30.2 reports
// the intersecting layout below and stays silent on both others.
func TestTableCellIntersection(t *testing.T) {
	t.Parallel()

	// row 0: TH(RowSpan 2) TH TH(RowSpan 2)
	// row 1: TH(ColSpan 2)  -> takes column 1, and its second column lands on
	//                          the slot row 0's last cell carries down into.
	intersecting := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",                       // 1
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",               // 2
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>", // 3
		"<< /Type /StructElem /S /Table /K [5 0 R 6 0 R] >>",      // 4
		"<< /Type /StructElem /S /TR /K [7 0 R 8 0 R 9 0 R] >>",   // 5
		"<< /Type /StructElem /S /TR /K [10 0 R] >>",              // 6
		"<< /Type /StructElem /S /TH /A << /RowSpan 2 >> >>",      // 7
		"<< /Type /StructElem /S /TH >>",                          // 8
		"<< /Type /StructElem /S /TH /A << /RowSpan 2 >> >>",      // 9
		"<< /Type /StructElem /S /TH /A << /ColSpan 2 >> >>",      // 10
	}

	// A /RowSpan that merely carries into the next row shifts that row's cells.
	shifted := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",                        // 1
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",                // 2
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",  // 3
		"<< /Type /StructElem /S /Table /K [5 0 R 6 0 R 7 0 R] >>", // 4
		"<< /Type /StructElem /S /TR /K [8 0 R 9 0 R] >>",          // 5
		"<< /Type /StructElem /S /TR /K [10 0 R 11 0 R] >>",        // 6
		"<< /Type /StructElem /S /TR /K [12 0 R] >>",               // 7
		"<< /Type /StructElem /S /TH >>",                           // 8
		"<< /Type /StructElem /S /TH >>",                           // 9
		"<< /Type /StructElem /S /TD /A << /RowSpan 2 >> >>",       // 10
		"<< /Type /StructElem /S /TD >>",                           // 11
		"<< /Type /StructElem /S /TD >>",                           // 12
	}

	// A conforming /ColSpan layout that exactly fills the grid width.
	conforming := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",                       // 1
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",               // 2
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>", // 3
		"<< /Type /StructElem /S /Table /K [5 0 R 6 0 R] >>",      // 4
		"<< /Type /StructElem /S /TR /K [7 0 R 8 0 R] >>",         // 5
		"<< /Type /StructElem /S /TR /K [9 0 R] >>",               // 6
		"<< /Type /StructElem /S /TH >>",                          // 7
		"<< /Type /StructElem /S /TH >>",                          // 8
		"<< /Type /StructElem /S /TD /A << /ColSpan 2 >> >>",      // 9
	}

	fires := func(t *testing.T, objs []string, profile string) bool {
		t.Helper()
		doc, table := tableDoc(t, 4, objs...)
		var results []Violation
		state := &walkState{doc: doc, results: &results, profile: profile}
		validateTableCellIntersection(context.Background(), state, table)
		want := Rule_14289_1_7_2_t15
		if profile == "PDF/UA-2" {
			want = Rule_14289_2_8_2_5_26_t01
		}
		return hasRule(results, want)
	}

	// ISO 14289-2 §8.2.5.26 t1 is the same rule as ISO 14289-1 §7.2 t15: both
	// scope to SETableCell and both read "hasIntersection != true", so one scan
	// serves both profiles. The THead/TBody/TFoot row-span limit belongs to
	// §8.2.5.26 t2, which scopes to SETable and is a different test. veraPDF
	// 1.30.2 reports each of these identically under both profiles.
	for _, profile := range []string{"PDF/UA-1", "PDF/UA-2"} {
		if !fires(t, intersecting, profile) {
			t.Errorf("%s: a cell reaching into a slot another cell already claims must fire", profile)
		}
		if fires(t, shifted, profile) {
			t.Errorf("%s: a /RowSpan shifts the next row's cells, so it must not fire", profile)
		}
		if fires(t, conforming, profile) {
			t.Errorf("%s: a /ColSpan filling the grid width must not fire", profile)
		}
	}
}

// TestValidateTableColumnConsistency_RowSpanCrossesSection covers the profile
// split for a /RowSpan that starts in a THead and reaches into the TBody.
// PDF/UA-2 judges the grouping the sections form and reports §8.2.5.26 t2;
// PDF/UA-1 draws no section boundary, carries the span into the next row and
// reports its column rule §7.2 t42. Both were measured against veraPDF.
func TestValidateTableColumnConsistency_RowSpanCrossesSection(t *testing.T) {
	// Object 4 is the Table: a THead of one row whose first cell spans two
	// rows, then a TBody of one row with two cells.
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /Table /K [5 0 R 6 0 R] >>",
		"<< /Type /StructElem /S /THead /P 4 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 4 0 R /K [8 0 R] >>",
		"<< /Type /StructElem /S /TR /P 5 0 R /K [9 0 R 10 0 R] >>",
		"<< /Type /StructElem /S /TR /P 6 0 R /K [11 0 R 12 0 R] >>",
		"<< /Type /StructElem /S /TH /P 7 0 R /A << /O /Table /RowSpan 2 >> >>",
		"<< /Type /StructElem /S /TH /P 7 0 R /A << /O /Table >> >>",
		"<< /Type /StructElem /S /TD /P 8 0 R /A << /O /Table >> >>",
		"<< /Type /StructElem /S /TD /P 8 0 R /A << /O /Table >> >>",
	}
	run := func(profile string) []Violation {
		doc, table := tableDoc(t, 4, objs...)
		var results []Violation
		state := &walkState{doc: doc, results: &results, profile: profile}
		validateTableColumnConsistency(context.Background(), state, table)
		return results
	}

	if got := run("PDF/UA-2"); !hasRule(got, Rule_14289_2_8_2_5_26_t02) {
		t.Errorf("PDF/UA-2: got %v, want §8.2.5.26 t2", got)
	}
	if got := run("PDF/UA-2"); hasRule(got, Rule_14289_2_8_2_5_26_t03) {
		t.Errorf("PDF/UA-2: got %v, want no column-rule verdict", got)
	}
	if got := run("PDF/UA-1"); !hasRule(got, Rule_14289_1_7_2_t42) {
		t.Errorf("PDF/UA-1: got %v, want §7.2 t42", got)
	}
}

// A /RowSpan that stays inside its own section is regular, and so is a table
// whose rows sit directly under it with no section at all.
func TestValidateTableColumnConsistency_RowSpanInsideSection(t *testing.T) {
	inSection := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructElem /S /Table /K [5 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 4 0 R /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /TR /P 5 0 R /K [8 0 R 9 0 R] >>",
		"<< /Type /StructElem /S /TR /P 5 0 R /K [10 0 R] >>",
		"<< /Type /StructElem /S /TD /P 6 0 R /A << /O /Table /RowSpan 2 >> >>",
		"<< /Type /StructElem /S /TD /P 6 0 R /A << /O /Table >> >>",
		"<< /Type /StructElem /S /TD /P 7 0 R /A << /O /Table >> >>",
	}
	doc, table := tableDoc(t, 4, inSection...)
	var results []Violation
	state := &walkState{doc: doc, results: &results, profile: "PDF/UA-2"}
	validateTableColumnConsistency(context.Background(), state, table)
	if hasRule(results, Rule_14289_2_8_2_5_26_t02) {
		t.Errorf("span inside its section: got %v, want no t2", results)
	}
}
