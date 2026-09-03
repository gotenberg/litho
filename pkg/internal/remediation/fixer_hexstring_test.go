package remediation

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// hexStringPDF builds a one-page document whose content stream shows text
// through the given operand, so a caller can supply a hex string with an odd
// or an even number of digits.
func hexStringPDF(operand string) []byte {
	body := "BT /F1 12 Tf 10 700 Td " + operand + " Tj ET\n"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
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

// rewrittenContent runs the hex fixer and returns the queued content stream,
// or "" when the fixer left the page alone.
func rewrittenContent(t *testing.T, data []byte) string {
	t.Helper()

	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if _, err := fixHexString(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixHexString: %v", err)
	}
	stream, ok := alloc.Pending()[4].(ast.PdfStream)
	if !ok {
		return ""
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the rewritten stream: %v", err)
	}
	return string(raw)
}

// TestFixHexStringRewritesAnOddDigitString pins that a hex string with an odd
// digit count is rewritten into a form that has none.
//
// ISO 32000-1, 7.3.4.3 lets a reader assume the missing final digit is 0, and
// litho's parser does, so the value is already understood: the defect is how it
// is written, and ISO 19005-2, 6.1.6 test 1 forbids it. The full rewrite the
// file-structure fixer asks for re-serialises objects and carries a content
// stream's body across as bytes, so these came back as residuals on every pass.
func TestFixHexStringRewritesAnOddDigitString(t *testing.T) {
	t.Parallel()

	// "48656C6C6F" is "Hello"; dropping the last digit leaves nine.
	got := rewrittenContent(t, hexStringPDF("<48656C6C6>"))
	if got == "" {
		t.Fatal("the content stream was not rewritten")
	}
	if strings.Contains(got, "48656C6C6>") {
		t.Errorf("the odd-digit hex string survived:\n%s", got)
	}
	// The value the parser understood must still be what is drawn: the final
	// digit is assumed to be 0, giving "Hell" and a 0x60 byte.
	if !strings.Contains(got, "Hell") {
		t.Errorf("the string's value was not preserved:\n%s", got)
	}
	if !strings.Contains(got, "Tj") {
		t.Errorf("the show-text operator was lost:\n%s", got)
	}
}

// TestFixHexStringLeavesAnEvenDigitString is the control: a well-formed hex
// string is no reason to rewrite a page, and an untouched page keeps its bytes.
func TestFixHexStringLeavesAnEvenDigitString(t *testing.T) {
	t.Parallel()

	if got := rewrittenContent(t, hexStringPDF("<48656C6C6F>")); got != "" {
		t.Errorf("a page with a well-formed hex string was rewritten:\n%s", got)
	}
}

// TestFixHexStringLeavesALiteralString is the control for the ordinary case:
// a page that uses literal strings has nothing to repair.
func TestFixHexStringLeavesALiteralString(t *testing.T) {
	t.Parallel()

	if got := rewrittenContent(t, hexStringPDF("(Hello)")); got != "" {
		t.Errorf("a page with a literal string was rewritten:\n%s", got)
	}
}

// violationFor builds the violation a detector would report for a clause.
func violationFor(clause string, test int, context string) compliance.Violation {
	return compliance.Violation{
		Rule:    compliance.Rule{Specification: "ISO_19005_2", Clause: clause, TestNumber: test},
		Context: context,
	}
}

// TestMatchHexStringClaimsOnlyTheOddDigitReport pins the match: the fixer must
// claim the odd-digit violation and leave the invalid-character one, which is a
// different repair.
func TestMatchHexStringClaimsOnlyTheOddDigitReport(t *testing.T) {
	t.Parallel()

	odd := violationFor("6.1.6", 1, "page 1: compliance: "+oddHexDigits)
	if !matchHexString(odd) {
		t.Error("the odd-digit violation is not claimed")
	}
	bad := violationFor("6.1.6", 2, "page 1: compliance: hex string has invalid character")
	if matchHexString(bad) {
		t.Error("the invalid-character violation was claimed by the odd-digit fixer")
	}
	other := violationFor("6.2.2", 2, "page 1: compliance: "+oddHexDigits)
	if matchHexString(other) {
		t.Error("an unrelated clause was claimed")
	}
}
