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
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
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
	ops, err := content.ParseStreamData(context.Background(), []byte(got))
	if err != nil {
		t.Fatalf("parsing the rewrite: %v", err)
	}
	var shown []ast.PdfObject
	for _, op := range ops {
		if op.Operator == "Tj" {
			shown = append(shown, op.Operands...)
		}
	}
	if len(shown) != 1 || shown[0] != ast.PdfString("Hell\x60") {
		t.Errorf("the rewrite shows %v, want the one string %q:\n%s", shown, "Hell\x60", got)
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

// TestRespellHexStringsAgreesWithTheParser pins the repair against the
// detector's own parse: a stream is respelled exactly when that parse records a
// violation wrapping one of the two hex-string sentinels, the respelled stream
// records none, and it parses to the same operations, so every string keeps
// the value the parser read (ISO 32000-1, 7.3.4.3).
func TestRespellHexStringsAgreesWithTheParser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tt := range []struct {
		name    string
		content string
		want    int
	}{
		{"odd digit count", "BT <484> Tj ET", 1},
		{"invalid character", "BT <41!4> Tj ET", 1},
		{"an invalid character making the count odd", "BT <41!> Tj ET", 1},
		{"white space inside an odd string", "BT <4 8\n4> Tj ET", 1},
		{"well-formed", "BT <4848> Tj ET", 0},
		{"well-formed with white space", "BT <48 48> Tj ET", 0},
		{"empty", "BT <> Tj ET", 0},
		{"inside an array", "BT [<414> 120 <42>] TJ ET", 1},
		{"inside a property list", "/Span << /ActualText <FEFF004> >> BDC EMC", 1},
		{"two on one page", "BT <414> Tj <4!2> Tj ET", 2},
		{"an inline image's Indexed lookup", "q BI /W 1 /H 1 /BPC 8 /CS [/I /RGB 0 <FF000>] ID \x00 EI Q", 1},
		{"an invalid character in an inline image dictionary", "BI /W 1 /H 1 /BPC 8 /CS [/I /G 1 <0!F>] ID \x00 EI", 1},
		{"inline image data that reads like one", "BI /W 5 /H 1 /BPC 8 /CS /G ID <414> EI", 0},
		// The parser records a scientific-notation real as a format
		// violation too, which the hex fixer does not repair.
		{"another parse violation", "1.5e2 0 Td", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			src := []byte(tt.content)
			_, before := content.ParseStreamDataFormatChecked(ctx, src)
			if got := hexViolations(before) > 0; got != (tt.want > 0) {
				t.Fatalf("control: the parse recorded %v, want hex violations = %v", before, tt.want > 0)
			}
			out, n := respellHexStrings(ctx, src)
			if n != tt.want {
				t.Fatalf("respellHexStrings(%q) respelled %d strings, want %d: %q", src, n, tt.want, out)
			}
			if n == 0 {
				if !bytes.Equal(out, src) {
					t.Errorf("a stream with nothing to respell came back changed: %q", out)
				}
				return
			}
			after, violations := content.ParseStreamDataFormatChecked(ctx, out)
			if hexViolations(violations) > 0 {
				t.Errorf("the respelled stream %q still records %v", out, violations)
			}
			original, err := content.ParseStreamData(ctx, src)
			if err != nil {
				t.Fatalf("parsing the source: %v", err)
			}
			if !sameOperations(after, original) {
				t.Errorf("the respelled stream %q reads as\n  %v\nwant what the source reads as\n  %v", out, after, original)
			}
		})
	}
}

// hexViolations counts the violations that wrap one of the two hex-string
// sentinels.
func hexViolations(violations []error) int {
	n := 0
	for _, v := range violations {
		if errors.Is(v, parser.ErrHexStringOddDigits) || errors.Is(v, parser.ErrHexStringInvalidChar) {
			n++
		}
	}
	return n
}

// TestWellFormedHexString pins the spelling: the hexadecimal digits as
// written, a final 0 when their count is odd, and nothing for a well-formed
// string, white space inside it included.
func TestWellFormedHexString(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		tok, want string
		malformed bool
	}{
		{"<48656C6C6>", "<48656C6C60>", true},
		{"<41!4>", "<4140>", true},
		{"<41!>", "<41>", true},
		{"<4 1 4>", "<4140>", true},
		{"<41\x80>", "<41>", true},
		{"<4141>", "", false},
		{"<41 41>", "", false},
		{"<>", "", false},
	} {
		got, malformed := wellFormedHexString(tt.tok)
		if got != tt.want || malformed != tt.malformed {
			t.Errorf("wellFormedHexString(%q) = %q, %v; want %q, %v", tt.tok, got, malformed, tt.want, tt.malformed)
		}
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

// TestFixHexStringKeepsTheOperatorAfterAMalformedOperand pins the round trip
// the rewrite depends on: parsing the rewritten stream gives back the operators
// the original parsed to, with a malformed operand elsewhere on the page.
//
// The content parser used to consume one token more than a failed operand
// parse had, so "/F1 1e2 Tf" lost its Tf and the rewrite wrote the page back
// without it, the stray /F1 joined to the operator that followed. The streams
// are read as PDF.js reads them, where "1e2" is one number the parser cannot
// read; the reading of veraPDF ends it at the "e".
func TestFixHexStringKeepsTheOperatorAfterAMalformedOperand(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	operand := "<48656C6C6> Tj /F1 1e2 Tf [(a) 1e2 (b)] TJ (c)"
	original := "BT /F1 12 Tf 10 700 Td " + operand + " Tj ET\n"
	rewritten := rewrittenContent(t, hexStringPDF(operand))
	if rewritten == "" {
		t.Fatal("the content stream was not rewritten")
	}

	want := []string{"BT", "Tf", "Td", "Tj", "Tf", "TJ", "Tj", "ET"}
	var after []content.Operation
	for name, src := range map[string]string{"original": original, "rewrite": rewritten} {
		ops, err := content.ParseStreamData(ctx, []byte(src), content.SplitGluedOperators())
		if err != nil {
			t.Fatalf("parsing the %s: %v", name, err)
		}
		var got []string
		for _, op := range ops {
			got = append(got, op.Operator)
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("the %s parses to %v, want %v", name, got, want)
		}
		if name == "rewrite" {
			after = ops
		}
	}
	if tf := after[4].Operands; len(tf) != 1 || tf[0] != ast.PdfName("F1") {
		t.Errorf("the rewritten Tf carries %v, want [/F1]", tf)
	}
	if tj, ok := after[5].Operands[0].(ast.PdfArray); !ok || len(tj) != 2 {
		t.Errorf("the rewritten TJ carries %v, want [(a) (b)]", after[5].Operands)
	}
}
