package remediation

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestRewriteExponentNumbers pins how a number written with an exponent is
// rewritten: as the plain number it denotes, spliced in place, and only in a
// stream where one stands as an operand, which is the finding ISO 19005-1,
// 6.2.10 t1 and ISO 19005-2, 6.2.2 t1 report.
//
// veraPDF reads "1.5e1 w" as the number 1.5, an undefined operator "e1" and a
// w with no operand. Stripping "e1" the way an undefined operator is stripped
// would take the 1.5 with it; a reader that accepts the form reads 15, the
// number the producer wrote.
func TestRewriteExponentNumbers(t *testing.T) {
	t.Parallel()

	image := "BI /W 1 /H 1 /BPC 8 /CS /G ID \x80\x1e\x31 EI"
	for _, tc := range []struct {
		name, in, want string
		part, count    int
	}{
		{"a real operand", "1.5e1 w", "15 w", 2, 1},
		{"an integer operand", "/F1 1e2 Tf", "/F1 100 Tf", 2, 1},
		{"a signed exponent", "-1.5E-1 w", "-0.15 w", 2, 1},
		{"an explicit plus", "+2e+1 0 m", "20 0 m", 2, 1},
		{"nested alone is no finding", "[2.5e1 3] 0 d", "[2.5e1 3] 0 d", 2, 0},
		{"nested beside a finding", "1.5e1 w [2.5e1 3] 0 d", "15 w [25 3] 0 d", 2, 2},
		{"inside a string", "(1.5e1) Tj", "(1.5e1) Tj", 2, 0},
		{"inside a comment", "% 1e5\n0 w", "% 1e5\n0 w", 2, 0},
		{"a keyword that only looks like one", "1e 1e2e3 w", "1e 1e2e3 w", 2, 0},
		{"after an inline image", image + " 1e1 w", image + " 10 w", 2, 1},
		{"past the PDF/A-1 real limit", "1e5 w", "32767 w", 1, 1},
		{"past the ISO 32000-1 real limit", "-1e40 w", "-340300000000000000000000000000000000000.0 w", 2, 1},
		{"past the float64 range", "1e999 w", "340300000000000000000000000000000000000.0 w", 4, 1},
		{"closer to 0 than a normal real", "1e-40 w", "0 w", 2, 1},
		{"a whole real past the integer range", "3e10 0 m", "30000000000.0 0 m", 2, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, n := rewriteExponentNumbers(context.Background(), []byte(tc.in), tc.part)
			if string(got) != tc.want || n != tc.count {
				t.Errorf("rewriteExponentNumbers(%q) = %q, %d; want %q, %d", tc.in, got, n, tc.want, tc.count)
			}
			if n == 0 {
				return
			}
			// The check's own parse finds nothing left to report.
			if _, violations := content.ParseStreamDataFormatChecked(context.Background(), got); len(violations) > 0 {
				for _, v := range violations {
					if errors.Is(v, parser.ErrNumberExponent) {
						t.Errorf("the rewritten stream still holds an exponent: %v", v)
					}
				}
			}
		})
	}
}

// exponentPagePDF builds a page drawing pageContent, whose resources name a
// form XObject (object 5) with formContent. When innerContent is set, that form
// draws a second form (object 6) with innerContent as /X1; otherwise it has no
// /Resources of its own and reaches /CS0, a page colour space, through the
// page. Object 7 holds an annotation whose normal appearance, object 8, is
// apContent.
func exponentPagePDF(pageContent, formContent, innerContent, apContent string) []byte {
	stream := func(dict, body string) string {
		return "<< " + dict + " /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream"
	}
	formDict := "/Type /XObject /Subtype /Form /BBox [0 0 10 10]"
	if innerContent != "" {
		formDict += " /Resources << /XObject << /X1 6 0 R >> >>"
	}
	return buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Annots [7 0 R]"+
			" /Resources << /XObject << /X0 5 0 R >> /ColorSpace << /CS0 [/CalGray << /WhitePoint [0.9505 1 1.089] >>] >> >> >>",
		stream("", pageContent),
		stream(formDict, formContent),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", innerContent),
		"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 8 0 R >> >>",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", apContent),
	)
}

// TestFixUnknownOps_RewritesAnExponentOnlyStream pins that a content stream
// whose only defect is a number written with an exponent is written back, on
// the page, in a form XObject, in a form one level below a clean form, and in
// an annotation appearance, and that a clean stream is not.
//
// The fixer wrote a stream back only when it removed an operator or folded a
// malformed number, and the content parser reads "1.5e1" as the number 15, so
// a stream holding one kept it and the finding survived remediation. The form
// walk also descended only from a form it had rewritten, so the inner form was
// never read.
func TestFixUnknownOps_RewritesAnExponentOnlyStream(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF(
		"1.5e1 w 0 0 m 10 10 l S /X0 Do",
		"q 0 0 1 1 re f /X1 Do Q",
		"1e1 w 0 0 m 5 5 l S",
		"1.5E-1 w 0 0 m 5 5 l S",
	))

	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if !strings.Contains(strings.Join(fixes, "\n"), "rewrote 3 number(s) written with an exponent") {
		t.Errorf("fixes = %v, want three exponents reported", fixes)
	}

	for _, tc := range []struct {
		obj  int
		want string
	}{
		{4, "15 w 0 0 m 10 10 l S /X0 Do"},
		{6, "10 w 0 0 m 5 5 l S"},
		{8, "0.15 w 0 0 m 5 5 l S"},
	} {
		got := string(bytes.TrimSpace(pendingStreamBytes(t, alloc, tc.obj)))
		if got != tc.want {
			t.Errorf("object %d holds %q, want %q spliced in place", tc.obj, got, tc.want)
		}
	}
	if _, rewritten := alloc.Pending()[5]; rewritten {
		t.Errorf("the clean form was rewritten: %v", alloc.Pending()[5])
	}
}

// TestFixUnknownOps_KeepsTheOperandsOfTheNextOperator reproduces the page of
// the corpus file that carries the defect: an undefined operator, then a font
// size written with an exponent. The removal re-serializes the page, and the
// content parser cannot read "1e2", so without the rewrite first Tf lost its
// size, and the operator itself went with the unreadable token.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: remediating it at PDF/A-2b wrote "/F1 50 600
//     Td", a Td with three operands, where the page sets "/F1 1e2 Tf".
func TestFixUnknownOps_KeepsTheOperandsOfTheNextOperator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF(
		"BT /F1 24 Tf 50 500 Td (a) Tj ETBT /F1 1e2 Tf 50 600 Td (b) Tj ET", "", "", ""))
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	ops, err := content.ParseStreamData(ctx, pendingStreamBytes(t, alloc, 4))
	if err != nil {
		t.Fatalf("parsing the rewritten page: %v", err)
	}
	var tf []content.Operation
	for _, op := range ops {
		if op.Operator == "ETBT" {
			t.Errorf("the undefined operator ETBT survived")
		}
		if op.Operator == "Tf" {
			tf = append(tf, op)
		}
	}
	if len(tf) != 2 {
		t.Fatalf("got %d Tf operators, want 2: %v", len(tf), ops)
	}
	if size := tf[1].Operands; len(size) != 2 || size[1] != ast.PdfInt(100) {
		t.Errorf("the second Tf has operands %v, want [/F1 100]", size)
	}
}

// TestFixUnknownOps_LeavesAnInlineImageStreamItCannotSerialize pins the
// residual: removing an operator re-serializes the parsed operations, which
// keep an inline image's dictionary and drop its data, so a stream holding one
// is not rewritten for an operator. An exponent in it is still rewritten,
// since that splices the token and carries the image bytes across.
func TestFixUnknownOps_LeavesAnInlineImageStreamItCannotSerialize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	image := "q BI /W 1 /H 1 /BPC 8 /CS /G ID \x80 EI Q"

	doc, alloc := newTestDoc(t, exponentPagePDF(image+" foo 1 0 0 RG", "", "", ""))
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if got, rewritten := alloc.Pending()[4]; rewritten {
		body, _ := got.(ast.PdfStream).Bytes(ctx)
		t.Errorf("the page was re-serialized to %q, which loses the inline image", body)
	}

	doc, alloc = newTestDoc(t, exponentPagePDF(image+" foo 1e1 w", "", "", ""))
	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if got, want := string(bytes.TrimSpace(pendingStreamBytes(t, alloc, 4))), image+" foo 10 w"; got != want {
		t.Errorf("page = %q, want %q", got, want)
	}
}

// TestFixUnknownOps_EmptiesAStreamOfUndefinedOperators pins that a stream
// holding nothing but undefined operators is written back empty. Serializing no
// operations yields no bytes, which is still a change to write, so the repair
// keys "nothing changed" on what it counted, never on the bytes.
//
// # Corpus evidence
//
//   - pdfjs issue17554.pdf: its page holds the single operator "toString".
func TestFixUnknownOps_EmptiesAStreamOfUndefinedOperators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF("toString", "", "", ""))
	fixes, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	if body := pendingStreamBytes(t, alloc, 4); len(bytes.TrimSpace(body)) != 0 {
		t.Errorf("page = %q, want it emptied; fixes=%v", body, fixes)
	}
}

// TestFixUnknownOps_KeepsTheNestedResourcesRepair runs the real earlier fixer
// that writes the same form XObject: nested-resources gives the form the
// /Resources it lacks, and the exponent rewrite must build on that stream,
// not the document's.
func TestFixUnknownOps_KeepsTheNestedResourcesRepair(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	const formObj = 5
	doc, alloc := newTestDoc(t, exponentPagePDF("/X0 Do", "q /CS0 cs 0.5 sc 1.5e1 w Q", "", ""))

	if _, err := fixNestedResources(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixNestedResources: %v", err)
	}
	seeded, ok := pendingStream(t, alloc, formObj)
	if !ok || seeded.Dict["Resources"] == nil {
		t.Fatalf("control: nested-resources did not give object %d its /Resources, so there is no "+
			"earlier edit to lose", formObj)
	}

	if _, err := fixUnknownOps(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
		t.Fatalf("fixUnknownOps: %v", err)
	}
	got, _ := pendingStream(t, alloc, formObj)
	body, err := got.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the form: %v", err)
	}
	if !bytes.Contains(body, []byte("15 w")) {
		t.Fatalf("the form holds %q: the exponent was not rewritten, so the check below proves nothing", body)
	}
	if got.Dict["Resources"] == nil {
		t.Errorf("/Resources is gone: the rewrite rebuilt the form from the document and discarded "+
			"nested-resources' write; dict = %v", got.Dict)
	}
}

// TestMatchUnknownOps_ClaimsEveryPartsUndefinedOperatorRule pins that the
// fixer claims the rule in the spelling of every part: ISO 19005-1 numbers it
// 6.2.10 t1, and matching 6.2.2 alone left the part-1 finding unrepaired unless
// content could change.
func TestMatchUnknownOps_ClaimsEveryPartsUndefinedOperatorRule(t *testing.T) {
	t.Parallel()

	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_2_10_t01,
		compliance.Rule_19005_2_6_2_2_t01,
		compliance.Rule_19005_3_6_2_2_t01,
		compliance.Rule_19005_4_6_2_2_t01,
	} {
		if !matchUnknownOps(compliance.Violation{Rule: rule}) {
			t.Errorf("%s %s t%d is not claimed", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
	// Part 1 spells its OutputIntents rule 6.2.2, the clause parts 2 to 4 give
	// the undefined-operator rule.
	for _, rule := range []compliance.Rule{
		compliance.Rule_19005_1_6_2_2_t01,
		compliance.Rule_19005_2_6_2_2_t02,
	} {
		if matchUnknownOps(compliance.Violation{Rule: rule}) {
			t.Errorf("%s %s t%d is claimed, which is no undefined-operator rule", rule.Specification, rule.Clause, rule.TestNumber)
		}
	}
}
