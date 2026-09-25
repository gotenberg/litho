package remediation

import (
	"context"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// TestClampContentNumbers pins that the content-limits repair respells, in
// place, every number of a content stream past the limits of the part,
// wherever it stands: an operand, an element of a nested array, a value of a
// property list or of an inline image's dictionary. A number the part admits,
// a run that spells no number and a number written with an exponent are left
// as written.
func TestClampContentNumbers(t *testing.T) {
	t.Parallel()

	const (
		path   = " 0 0 m 10 10 l S"
		image  = "q BI /W 1 /H 1 /BPC 8 /CS /G "
		denorm = "0.000000000000000000000000000000000000000001"
	)
	for _, tc := range []struct {
		name, data, want string
		part, n          int
	}{
		{"an integer in a dash array", "[99999999999 2] 0 d" + path, "[32767 2] 0 d" + path, 1, 1},
		{"an integer in a dash array, PDF/A-2", "[99999999999 2] 0 d" + path, "[99999999999.0 2] 0 d" + path, 2, 1},
		{"a real in a TJ array", "BT [(a) -39455.5 (b)] TJ ET", "BT [(a) -32767 (b)] TJ ET", 1, 1},
		{"a real in a nested dictionary", "/Span << /X << /Y 99999.5 >> >> BDC EMC", "/Span << /X << /Y 32767 >> >> BDC EMC", 1, 1},
		{"a denormal in a property list", "/Span << /X " + denorm + " >> BDC EMC", "/Span << /X 0 >> BDC EMC", 2, 1},
		{"an integer in an inline image", image + "/D [0 99999999999] ID \x00\xff EI Q", image + "/D [0 32767] ID \x00\xff EI Q", 1, 1},
		{"an operand", "99999.5 0 m 0 99999.5 l S", "32767 0 m 0 32767 l S", 1, 2},
		{"an integer PDF/A-1 admits", "40000 0 m" + path, "", 1, 0},
		{"a real PDF/A-2 admits", "50000.5 0 m" + path, "", 2, 0},
		{"a denormal PDF/A-1 admits", "BT [(a) " + denorm + " (b)] TJ ET", "", 1, 0},
		{"a run that spells no number", "[- 2] 0 d - 0 m" + path, "", 1, 0},
		{"an exponent", "1e40 w" + path, "", 1, 0},
		{"a part with no limits", "[99999999999 2] 0 d" + path, "", 4, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, tally := clampContentNumbers(context.Background(), []byte(tc.data), tc.part)
			n := tally.clamped
			if n != tc.n {
				t.Errorf("clamped %d numbers, want %d", n, tc.n)
			}
			want := tc.want
			if tc.n == 0 {
				want = tc.data
			}
			if string(out) != want {
				t.Errorf("the repair wrote %q, want %q", out, want)
			}
		})
	}
}

// TestClampContentNumbersPastADouble pins that an integer too long for a
// double is written back as a number inside the limits of the part.
func TestClampContentNumbersPastADouble(t *testing.T) {
	t.Parallel()

	for _, part := range []int{1, 2} {
		out, tally := clampContentNumbers(context.Background(), []byte(strings.Repeat("9", 400)+" 0 m"), part)
		n := tally.clamped
		if n != 1 {
			t.Fatalf("part %d: clamped %d numbers, want 1", part, n)
		}
		splices, _ := contentTokenSplices(context.Background(), out, func(tok lexer.Token, _ tokenPlace) (string, bool) {
			return "", tok.Type == lexer.TokenNumber && breaksNumberLimits(tok.Value, part)
		})
		if len(splices) != 0 {
			t.Errorf("part %d: the repair wrote %q, still past the limits", part, out)
		}
	}
}

// TestClampContentNumbersSplitsTranslations pins that a cm whose translation
// breaks the PDF/A-1 real limit is written as translations inside the limit
// ahead of it, which compose to the matrix the producer wrote, so nothing
// drawn after it moves. A cm with another entry past the limit, a translation
// past the split cap and a translation PDF/A-2 admits keep the clamp or stay
// as written.
//
// # Corpus evidence
//
//   - pdfjs issue19281.pdf: places a drawing with the first cm below.
func TestClampContentNumbersSplitsTranslations(t *testing.T) {
	t.Parallel()

	far := strconv.Itoa((maxTranslationSplits+2)*32767) + ".5"
	for _, tc := range []struct {
		name, data, want string
		part             int
		clamped, split   int
	}{
		{"a translation below the page", "q 3.1264944 0 0 3.1264944 150 -34310.223 cm 0 0 m Q",
			"q 1 0 0 1 0 -32767 cm 3.1264944 0 0 3.1264944 150 -1543.223 cm 0 0 m Q", 1, 0, 1},
		{"two translations of several splits", "1 0 0 1 70000.5 -100000.25 cm",
			"1 0 0 1 32767 -32767 cm 1 0 0 1 32767 -32767 cm 1 0 0 1 0 -32767 cm 1 0 0 1 4466.5 -1699.25 cm", 1, 0, 1},
		{"an integer translation PDF/A-1 admits", "1 0 0 1 70000 -34310.5 cm",
			"1 0 0 1 0 -32767 cm 1 0 0 1 70000 -1543.5 cm", 1, 0, 1},
		{"a scale past the limit", "99999.5 0 0 1 40000.5 0 cm", "32767 0 0 1 32767 0 cm", 1, 2, 0},
		{"a translation past the split cap", "1 0 0 1 " + far + " 0 cm", "1 0 0 1 32767 0 cm", 1, 1, 0},
		{"a translation PDF/A-2 admits", "1 0 0 1 70000.5 0 cm", "1 0 0 1 70000.5 0 cm", 2, 0, 0},
		{"a cm with a name among its operands", "/X 1 0 0 1 70000.5 cm", "/X 1 0 0 1 32767 cm", 1, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			out, tally := clampContentNumbers(context.Background(), []byte(tc.data), tc.part)
			if tally.clamped != tc.clamped || tally.split != tc.split {
				t.Errorf("clamped %d numbers and split %d translations, want %d and %d", tally.clamped, tally.split, tc.clamped, tc.split)
			}
			if string(out) != tc.want {
				t.Errorf("the repair wrote %q, want %q", out, tc.want)
			}
			if tc.split == 0 {
				return
			}
			if got, want := composedCM(t, out), composedCM(t, []byte(tc.data)); !sameMatrix(got, want) {
				t.Errorf("the cm operators compose to %v, want %v", got, want)
			}
		})
	}
}

// composedCM returns the product of the cm operators of data, in the order a
// reader concatenates them (ISO 32000-1, 8.4.4).
func composedCM(t *testing.T, data []byte) [6]float64 {
	t.Helper()

	ops, err := content.ParseStreamData(context.Background(), data)
	if err != nil {
		t.Fatalf("ParseStreamData: %v", err)
	}
	ctm := [6]float64{1, 0, 0, 1, 0, 0}
	for _, op := range ops {
		if op.Operator != "cm" || len(op.Operands) != 6 {
			continue
		}
		var m [6]float64
		for i, o := range op.Operands {
			switch v := o.(type) {
			case ast.PdfInt:
				m[i] = float64(v)
			case ast.PdfReal:
				m[i] = float64(v)
			}
		}
		// The new CTM is m times the old one.
		ctm = [6]float64{
			m[0]*ctm[0] + m[1]*ctm[2], m[0]*ctm[1] + m[1]*ctm[3],
			m[2]*ctm[0] + m[3]*ctm[2], m[2]*ctm[1] + m[3]*ctm[3],
			m[4]*ctm[0] + m[5]*ctm[2] + ctm[4], m[4]*ctm[1] + m[5]*ctm[3] + ctm[5],
		}
	}
	return ctm
}

// sameMatrix reports whether a and b agree to a millionth of a unit.
func sameMatrix(a, b [6]float64) bool {
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-6 {
			return false
		}
	}
	return true
}

// TestFixContentLimitsReachesDrawnStreams pins that the content-limits repair
// reaches every content stream a page draws, and not only the page's own
// content: a real past the PDF/A-1 limit in a form XObject the page paints is
// clamped there, through the allocator, and the form stays a form.
//
// # Corpus evidence
//
//   - pdfjs 22060_A1_01_Plans.pdf: holds its reals past the limit in tiling
//     patterns.
func TestFixContentLimitsReachesDrawnStreams(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /XObject << /X0 5 0 R >> >> >>",
		hexStreamObj("", "[99999.5 2] 0 d 0 0 m 10 10 l S /X0 Do"),
		hexStreamObj("/Type /XObject /Subtype /Form /BBox [0 0 10 10]", "99999.5 w 0 0 m 10 10 l S"),
	))
	fixes, err := fixContentLimits(ctx, alloc, doc, Options{}, nil, "PDF/A-1b")
	if err != nil {
		t.Fatalf("fixContentLimits: %v", err)
	}
	if !slices.Contains(fixes, "clamped 2 out-of-range number(s) in content streams") {
		t.Errorf("fixes = %v, want the two numbers clamped", fixes)
	}
	for _, tc := range []struct {
		obj  int
		want string
	}{
		{4, "[32767 2] 0 d 0 0 m 10 10 l S /X0 Do"},
		{5, "32767 w 0 0 m 10 10 l S"},
	} {
		stream, ok := alloc.Pending()[tc.obj].(ast.PdfStream)
		if !ok {
			t.Fatalf("object %d not rewritten: %T", tc.obj, alloc.Pending()[tc.obj])
		}
		body, err := stream.Bytes(ctx)
		if err != nil {
			t.Fatalf("reading object %d: %v", tc.obj, err)
		}
		if got := strings.TrimSpace(string(body)); got != tc.want {
			t.Errorf("object %d holds %q, want %q", tc.obj, got, tc.want)
		}
		if tc.obj == 5 && stream.Dict["Subtype"] != ast.PdfName("Form") {
			t.Errorf("the form lost its /Subtype: %v", stream.Dict)
		}
	}
}

// TestMatchContentLimitsClaimsContentNumbers pins that the content-limits
// repair claims the limit violations compliance states for a number of a
// content stream, and no other: not a number of an object, which real-limits
// clamps, and not a run veraPDF gives the largest value, which unknown-ops
// respells. It runs in the content tier, which the writer rolls back when the
// output loses text.
func TestMatchContentLimitsClaimsContentNumbers(t *testing.T) {
	t.Parallel()

	data := buildStructPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> /Rotate 99999999999 >>",
		hexStreamObj("", "[99999999999 2] 0 d 99999.5 w - 0 m 10 10 l S"),
	)
	for _, tc := range []struct {
		profile       models.PDFAProfile
		limits, match int
	}{
		{models.PDFA1b, 4, 2},
		{models.PDFA2b, 3, 1},
	} {
		doc, _ := newTestDoc(t, data)
		viols, err := compliance.ValidatePDFA(context.Background(), doc, tc.profile)
		if err != nil {
			t.Fatalf("ValidatePDFA: %v", err)
		}
		limits, matched := 0, 0
		for _, e := range viols {
			v, ok := e.(compliance.Violation)
			if !ok || (v.Rule.Clause != "6.1.12" && v.Rule.Clause != "6.1.13") {
				continue
			}
			limits++
			if matchContentLimits(v) {
				matched++
				if !strings.Contains(v.Context, contentStreamContext) {
					t.Errorf("%s: claimed %q", tc.profile, v.Context)
				}
			}
		}
		if limits != tc.limits || matched != tc.match {
			t.Errorf("%s: claimed %d of %d limit violations, want %d of %d: %v", tc.profile, matched, limits, tc.match, tc.limits, viols)
		}
	}
	i := slices.IndexFunc(registry, func(f fixer) bool { return f.name == "content-limits" })
	if i < 0 || registry[i].safety != safetyContent {
		t.Error("content-limits is not a content-tier repair")
	}
}

// TestFixContentLimitsLeavesNumbersThePartAdmits pins that a number the part
// admits is not rewritten. The repair used to clamp every number past 32767,
// the PDF/A-1 real limit, which an integer under every part and a real under
// PDF/A-2 may exceed.
func TestFixContentLimitsLeavesNumbersThePartAdmits(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ profile, content string }{
		{"PDF/A-1b", "40000 0 m 10 10 l S"},
		{"PDF/A-2b", "40000 0 m 50000.5 10 l S"},
	} {
		ctx := context.Background()
		doc, alloc := newTestDoc(t, buildStructPDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << >> >>",
			hexStreamObj("", tc.content),
		))
		fixes, err := fixContentLimits(ctx, alloc, doc, Options{}, nil, tc.profile)
		if err != nil {
			t.Fatalf("fixContentLimits: %v", err)
		}
		if len(fixes) != 0 {
			t.Errorf("%s: %q was clamped: %v", tc.profile, tc.content, fixes)
		}
		if _, rewritten := alloc.Pending()[4]; rewritten {
			t.Errorf("%s: the content stream was rewritten", tc.profile)
		}
	}
}
