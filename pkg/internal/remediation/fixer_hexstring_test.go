package remediation

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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

// TestMatchHexStringClaimsBothMalformedReports pins the match: re-serialising a
// page repairs either way a hexadecimal string is written wrongly, so the fixer
// claims both the odd-count report and the invalid-character one, and nothing
// from another clause.
//
// The invalid-character report used to be left alone, repaired only as a side
// effect: the parity test counted hex digits, so "<41!4>" read as odd and the
// page was rewritten anyway. Counting non-white-space characters instead (ISO
// 19005-2, 6.1.6 t1, which veraPDF measures the same way) leaves that string
// even, so claiming the odd-count report alone stopped repairing it.
func TestMatchHexStringClaimsBothMalformedReports(t *testing.T) {
	t.Parallel()

	odd := violationFor("6.1.6", 1, "page 1: compliance: "+oddHexDigits)
	if !matchHexString(odd) {
		t.Error("the odd-count violation is not claimed")
	}
	bad := violationFor("6.1.6", 2, "page 1: compliance: "+invalidHexChar+" '!'")
	if !matchHexString(bad) {
		t.Error("the invalid-character violation is not claimed")
	}
	other := violationFor("6.2.2", 2, "page 1: compliance: "+oddHexDigits)
	if matchHexString(other) {
		t.Error("an unrelated clause was claimed")
	}
}

// TestHasMalformedHexViolationMatchesTheParserSentinels pins the page-level
// match: a page is rewritten when the detector's own parse records a violation
// wrapping one of the two hex-string sentinels, and for no other parse
// violation. The match is structural, so the same wording in an error that
// wraps neither sentinel is not claimed.
func TestHasMalformedHexViolationMatchesTheParserSentinels(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tt := range []struct {
		name     string
		content  string
		violated bool
		want     bool
	}{
		{"odd digit count", "BT <484> Tj ET", true, true},
		{"invalid character", "BT <41!4> Tj ET", true, true},
		{"well-formed", "BT <4848> Tj ET", false, false},
		// The parser records a scientific-notation real as a format
		// violation too, which the hex fixer does not repair.
		{"another parse violation", "1.5e2 0 Td", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, violations := content.ParseStreamDataFormatChecked(ctx, []byte(tt.content))
			if violated := len(violations) > 0; violated != tt.violated {
				t.Fatalf("the parse recorded %v, want violations = %v", violations, tt.violated)
			}
			if got := hasMalformedHexViolation(violations); got != tt.want {
				t.Errorf("hasMalformedHexViolation(%v) = %v, want %v", violations, got, tt.want)
			}
		})
	}

	if !hasMalformedHexViolation([]error{fmt.Errorf("page 1: %w", parser.ErrHexStringOddDigits)}) {
		t.Error("a wrapped sentinel is not matched")
	}
	if hasMalformedHexViolation([]error{errors.New("compliance: " + oddHexDigits), nil}) {
		t.Error("the wording alone was matched, without the sentinel")
	}
}

// TestFixHexStringRewritesAnInvalidCharacterString pins the repair the widened
// match enables: a hex string whose non-white-space count is even but which
// carries a character that is no hexadecimal digit is still malformed, and
// re-serialising the page writes the value the parser read (ISO 32000-1,
// 7.3.4.3), dropping the stray character.
func TestFixHexStringRewritesAnInvalidCharacterString(t *testing.T) {
	t.Parallel()

	got := rewrittenContent(t, hexStringPDF("<41!4>"))
	if got == "" {
		t.Fatal("the content stream was not rewritten")
	}
	if strings.Contains(got, "41!4") {
		t.Errorf("the malformed hex string survived:\n%s", got)
	}
	if !strings.Contains(got, "Tj") {
		t.Errorf("the show-text operator was lost:\n%s", got)
	}
}
