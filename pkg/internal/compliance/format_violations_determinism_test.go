package compliance

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// formatDefectPDF builds a one-page file whose object 5 carries two §6.1.9
// defects at once: its body runs straight into the endobj keyword with no EOL
// between them, and the parser rejects the body. Object 4 takes its /Length
// from object 5, so the stream checks resolve it again after the warming
// pass, and a rejected parse is never cached, so each of those resolutions
// re-reads the header. That combination is the shape pdfjs issue1045 carries,
// and it is what made the reported §6.1.9 count move from run to run.
func formatDefectPDF() []byte {
	bodies := []string{
		"<< /Type /Catalog /Pages 2 0 R >>\n",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>\n",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\n",
		"<< /Length 5 0 R >>\nstream\nBT ET\nendstream\n",
		"20184",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(bodies)+1)
	for i, body := range bodies {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%sendobj\n", i+1, body)
	}
	xrefOff := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(bodies)+1)
	for i := 1; i <= len(bodies); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(bodies)+1, xrefOff)
	return buf.Bytes()
}

// runValidation runs a PDF/A-2b validation and returns the violations as
// strings, in the order the engine emitted them. It reports errors through
// its return value so the concurrent test can call it off the test goroutine.
func runValidationReport(doc *document.Document) ([]string, error) {
	violations, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		return nil, fmt.Errorf("ValidatePDFA: %w", err)
	}
	out := make([]string, len(violations))
	for i, v := range violations {
		out[i] = v.Error()
	}
	return out, nil
}

// validationReport is [runValidationReport] for the test goroutine.
func validationReport(t *testing.T, doc *document.Document) []string {
	t.Helper()
	report, err := runValidationReport(doc)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func newFormatDefectDoc(t *testing.T, data []byte) *document.Document {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	return doc
}

// countClause returns how many of the reported violations name the clause.
func countClause(report []string, clause string) int {
	n := 0
	for _, v := range report {
		if strings.HasPrefix(v, "clause "+clause+" ") {
			n++
		}
	}
	return n
}

// TestValidatePDFA_FormatViolationsAreDeterministic pins that repeated
// validations of the same bytes report the same violations, in the same
// order. Format violations used to be collected per parse rather than per
// object, and read from a slice other check groups were still appending to,
// so the §6.1.9 count moved between runs of one unchanged binary.
func TestValidatePDFA_FormatViolationsAreDeterministic(t *testing.T) {
	t.Parallel()
	data := formatDefectPDF()

	want := validationReport(t, newFormatDefectDoc(t, data))
	if got := countClause(want, "6.1.9"); got != 1 {
		t.Fatalf("§6.1.9 reported %d times, want 1 (object 5 carries one defect)", got)
	}

	const runs = 24
	for i := range runs {
		got := validationReport(t, newFormatDefectDoc(t, data))
		if len(got) != len(want) {
			t.Fatalf("run %d reported %d violations, run 0 reported %d (§6.1.9: %d vs %d)",
				i+1, len(got), len(want), countClause(got, "6.1.9"), countClause(want, "6.1.9"))
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("run %d violation %d = %q, run 0 had %q", i+1, j, got[j], want[j])
			}
		}
	}
}

// TestValidatePDFA_SecondPassMatchesFirst pins that validating the same
// Document twice reports the same violations both times. The record is keyed
// per object and survives the run, because the objects the first pass cached
// are never parsed again: clearing it between passes would leave the second
// pass reporting only the objects that fail to parse.
func TestValidatePDFA_SecondPassMatchesFirst(t *testing.T) {
	t.Parallel()
	doc := newFormatDefectDoc(t, formatDefectPDF())

	first := validationReport(t, doc)
	second := validationReport(t, doc)

	if len(first) != len(second) {
		t.Fatalf("second pass reported %d violations, first reported %d (§6.1.9: %d vs %d)",
			len(second), len(first), countClause(second, "6.1.9"), countClause(first, "6.1.9"))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("second pass violation %d = %q, first pass had %q", i, second[i], first[i])
		}
	}
	if got := countClause(second, "6.1.9"); got != 1 {
		t.Errorf("second pass reported §6.1.9 %d times, want 1", got)
	}
}

// TestValidatePDFA_ConcurrentRunsAgree pins that validations running in
// parallel report the same violations as a lone one, both when they share a
// Document (serialized through its run mutex) and when each gets its own.
func TestValidatePDFA_ConcurrentRunsAgree(t *testing.T) {
	t.Parallel()
	data := formatDefectPDF()
	want := validationReport(t, newFormatDefectDoc(t, data))

	shared := newFormatDefectDoc(t, data)

	const goroutines = 8
	reports := make([][]string, 2*goroutines)
	errs := make([]error, 2*goroutines)
	own := make([]*document.Document, goroutines)
	for i := range own {
		own[i] = newFormatDefectDoc(t, data)
	}

	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Add(2)
		go func() {
			defer wg.Done()
			reports[i], errs[i] = runValidationReport(shared)
		}()
		go func() {
			defer wg.Done()
			reports[goroutines+i], errs[goroutines+i] = runValidationReport(own[i])
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent run %d: %v", i, err)
		}
	}

	for i, got := range reports {
		if len(got) != len(want) {
			t.Fatalf("concurrent run %d reported %d violations, want %d (§6.1.9: %d vs %d)",
				i, len(got), len(want), countClause(got, "6.1.9"), countClause(want, "6.1.9"))
		}
		for j := range got {
			if got[j] != want[j] {
				t.Fatalf("concurrent run %d violation %d = %q, want %q", i, j, got[j], want[j])
			}
		}
	}
}
