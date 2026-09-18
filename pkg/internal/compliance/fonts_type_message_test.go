package compliance

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// ruleContexts returns the Context of every violation of clause/test, which is
// the message [failf] formatted.
func ruleContexts(t *testing.T, data []byte, profile models.PDFAProfile, clause string, test int) []string {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	var out []string
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			out = append(out, v.Context)
		}
	}
	return out
}

// TestFontTypeMessageDistinguishesDefects pins that the /Type rule says which
// defect it found.
//
// Three different files reach ISO 19005-2 §6.2.11.2 t1: one with no /Type, one
// whose /Type is not a name, and one naming the wrong type. [ast.PdfName]
// renders with a leading slash and %q quotes whatever String returns, so the
// first two both reported `/Type is "/", not Font`, describing a name made of a
// single slash that no file contains. ISO 32000-1, 7.3.9 makes a null entry
// equivalent to an absent one, so those two state the same defect and share a
// message; a wrong name keeps its own.
//
// The rule fires in every case either way, so this is about the diagnosis being
// true, not about the verdict.
func TestFontTypeMessageDistinguishesDefects(t *testing.T) {
	t.Parallel()

	const content = "BT /F1 12 Tf 10 50 Td (a) Tj ET"

	simpleFont := func(typeEntry string) []byte {
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< "+typeEntry+" /Subtype /Type1 /BaseFont /Helvetica "+
				"/FirstChar 97 /LastChar 97 /Widths [500] >>",
		)
	}

	descendantFont := func(typeEntry string) []byte {
		body := "BT /F1 12 Tf 10 50 Td <0001> Tj ET"
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] "+
				"/Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
			"<< /Type /Font /Subtype /Type0 /BaseFont /Test /Encoding /Identity-H "+
				"/DescendantFonts [6 0 R] >>",
			"<< "+typeEntry+" /Subtype /CIDFontType2 /BaseFont /Test /DW 1000 "+
				"/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> >>",
		)
	}

	has := func(got []string, want string) bool {
		return slices.Contains(got, want)
	}

	tests := []struct {
		name      string
		typeEntry string
		want      string // "" means the rule must not fire
	}{
		{"absent /Type", "", "missing /Type"},
		{"null /Type", "/Type null", "missing /Type"},
		{"/Type is a string", "/Type (Font)", "/Type is not a name"},
		{"/Type names the wrong type", "/Type /Foo", "/Type is /Foo, not Font"},
		{"/Type /Font", "/Type /Font", ""},
	}

	for _, tt := range tests {
		t.Run("simple font: "+tt.name, func(t *testing.T) {
			t.Parallel()
			got := ruleContexts(t, simpleFont(tt.typeEntry), models.PDFA2b, "6.2.11.2", 1)
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("expected no §6.2.11.2 t1 violation, got %q", got)
				}
				return
			}
			want := "font " + tt.want
			if !has(got, want) {
				t.Errorf("messages = %q, want one equal to %q", got, want)
			}
		})

		t.Run("descendant CIDFont: "+tt.name, func(t *testing.T) {
			t.Parallel()
			got := ruleContexts(t, descendantFont(tt.typeEntry), models.PDFA2b, "6.2.11.2", 1)
			if tt.want == "" {
				if len(got) != 0 {
					t.Errorf("expected no §6.2.11.2 t1 violation, got %q", got)
				}
				return
			}
			want := "descendant CIDFont " + tt.want
			if !has(got, want) {
				t.Errorf("messages = %q, want one equal to %q", got, want)
			}
		})
	}

	t.Run("no message ever reports a bare slash", func(t *testing.T) {
		t.Parallel()
		for _, data := range [][]byte{
			simpleFont(""), simpleFont("/Type null"),
			simpleFont("/Type (Font)"), descendantFont(""), descendantFont("/Type (Font)"),
		} {
			for _, msg := range ruleContexts(t, data, models.PDFA2b, "6.2.11.2", 1) {
				if msg == `font /Type is "/", not Font` || msg == `descendant CIDFont /Type is "/", not Font` {
					t.Errorf("message still reports a bare slash: %q", msg)
				}
			}
		}
	})
}
