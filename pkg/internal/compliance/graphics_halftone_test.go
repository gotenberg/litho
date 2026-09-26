package compliance

import (
	"context"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// TestHalftoneJudgedAsVeraPDF pins the halftone tests of ISO 19005-2, 6.2.5
// t4 to t6 as veraPDF 1.30 states them on its PDHalftone objects (see
// [halftoneJudge]), each case on a page setting one indirect graphics state
// whose /HT is ht, with the objects extra writes after it. Every count was
// measured with veraPDF 1.30 at PDF/A-2b on the same construct.
func TestHalftoneJudgedAsVeraPDF(t *testing.T) {
	t.Parallel()
	const t1 = "/Type /Halftone /HalftoneType 1 /Frequency 60 /Angle 45 /SpotFunction /Round"
	const t6 = "<< /Type /Halftone /HalftoneType 6 /Width 2 /Height 2 /Length 4 >>\nstream\n\x00\x40\x80\xc0\nendstream"
	tests := []struct {
		name       string
		ht         string
		extra      []string
		t4, t5, t6 int
	}{
		{"a name other than Default", "/Foo", nil, 1, 1, 0},
		{"Default", "/Default", nil, 0, 0, 0},
		{"a transfer function named Default", "6 0 R", []string{"<< " + t1 + " /TransferFunction /Default >>"}, 0, 0, 1},
		{"a transfer function named Identity", "6 0 R", []string{"<< " + t1 + " /TransferFunction /Identity >>"}, 0, 0, 1},
		{"a null transfer function", "6 0 R", []string{"<< " + t1 + " /TransferFunction null >>"}, 0, 0, 0},
		{"a transfer function naming null", "6 0 R", []string{"<< " + t1 + " /TransferFunction 7 0 R >>", "null"}, 0, 0, 0},
		{"no halftone type", "6 0 R", []string{"<< /Type /Halftone /Frequency 60 /Angle 45 /SpotFunction /Round >>"}, 1, 0, 0},
		{"a type 6 stream", "6 0 R", []string{t6}, 1, 0, 0},
		{"type 5 with direct components", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Cyan << " + t1 + " /TransferFunction /Identity >> /Red << " + t1 +
				" >> /Default << /Type /Halftone /Frequency 60 /Angle 0 /SpotFunction /Round >> /Spot 7 0 R >>",
			t6,
		}, 2, 0, 3},
		{"type 5 with a transfer function dictionary", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << " + t1 +
				" >> /TransferFunction << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >>",
		}, 1, 0, 2},
		{"Blue then Yellow naming one component", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Blue 7 0 R /Yellow 7 0 R /Default 8 0 R >>",
			"<< " + t1 + " >>", "<< " + t1 + " >>",
		}, 0, 0, 0},
		{"Yellow then Blue naming one component", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Yellow 7 0 R /Blue 7 0 R /Default 8 0 R >>",
			"<< " + t1 + " >>", "<< " + t1 + " >>",
		}, 0, 0, 1},
		{"a component with a halftone name", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << " + t1 + " /HalftoneName (x) >> >>",
		}, 0, 1, 0},
		{"a type 5 component", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << " + t1 + " >> /Spot1 << /Type /Halftone /HalftoneType 5 /Red << " +
				t1 + " >> /Default << " + t1 + " >> /TransferFunction /Identity >> >>",
		}, 0, 0, 1},
		{"components whose transfer function is null", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << " + t1 + " >> /Red << " + t1 +
				" /TransferFunction null >> /Cyan << " + t1 + " /TransferFunction null >> >>",
		}, 0, 0, 1},
		{"a component typed as a real", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << /Type /Halftone /HalftoneType 1.0 /Frequency 60 /Angle 0 /SpotFunction /Round >> >>",
		}, 0, 0, 0},
		{"a component written as a name", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << " + t1 + " >> /Red /Default >>",
		}, 0, 0, 0},
		{"type 5 with a transfer function named Default", "6 0 R", []string{
			"<< /Type /Halftone /HalftoneType 5 /Default << " + t1 + " >> /TransferFunction /Default >>",
		}, 0, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objs := []string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R /Resources << /ExtGState << /G 5 0 R >> >> >>",
				streamObj("/G gs 0 0 10 10 re f"),
				"<< /Type /ExtGState /HT " + tt.ht + " >>",
			}
			doc, _ := objsDocPDF(t, append(objs, tt.extra...)...)
			viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			for _, c := range []struct {
				rule Rule
				want int
			}{{Rule_19005_2_6_2_5_t04, tt.t4}, {Rule_19005_2_6_2_5_t05, tt.t5}, {Rule_19005_2_6_2_5_t06, tt.t6}} {
				if got := countRuleErrs(t, viols, c.rule); got != c.want {
					t.Errorf("6.2.5 t%d fired %d times, want %d", c.rule.TestNumber, got, c.want)
				}
			}
		})
	}
}

// TestHalftoneDirectComponentsRepeatPerGS pins that a type 5 halftone written
// directly in a graphics state two gs operators set is judged at each, and a
// component it names as an indirect object once for the whole document, as
// veraPDF judges model objects with and without an object of their own.
func TestHalftoneDirectComponentsRepeatPerGS(t *testing.T) {
	t.Parallel()
	const t1 = "/Type /Halftone /HalftoneType 1 /Frequency 60 /Angle 45 /SpotFunction /Round"
	doc, _ := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 100 100] /Contents 4 0 R"+
			" /Resources << /ExtGState << /G << /HT << /HalftoneType 5 /Default << "+t1+" >> /Red << "+t1+" >> /Green 5 0 R >> >> >> >> >>",
		streamObj("/G gs /G gs 0 0 10 10 re f"),
		"<< "+t1+" >>",
	)
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	if got := countRuleErrs(t, viols, Rule_19005_2_6_2_5_t06); got != 3 {
		t.Errorf("6.2.5 t6 fired %d times, want 3: Red at each gs and Green once", got)
	}
}

// TestJavaHashMapOrder pins the iteration order of a Java HashMap the keys of
// a halftone dictionary are read in, against the one veraPDF 1.30 reports for
// govdocs thread_400_400570.pdf and thread_100_100518.pdf, whose halftones
// write the same keys in two orders.
func TestJavaHashMapOrder(t *testing.T) {
	t.Parallel()
	names := func(s ...string) []ast.PdfName {
		out := make([]ast.PdfName, len(s))
		for i, n := range s {
			out[i] = ast.PdfName(n)
		}
		return out
	}
	tests := []struct {
		written, want []ast.PdfName
	}{
		{
			names("Type", "HalftoneType", "Red", "Green", "Blue", "Gray", "Cyan", "Magenta", "Yellow", "Black", "Default"),
			names("Red", "Type", "Gray", "Cyan", "Blue", "Yellow", "HalftoneType", "Magenta", "Black", "Default", "Green"),
		},
		{
			names("Cyan", "Gray", "Default", "Magenta", "Yellow", "Black", "Blue", "Red", "Green", "HalftoneType"),
			names("Red", "Gray", "Cyan", "Yellow", "Blue", "Magenta", "HalftoneType", "Black", "Default", "Green"),
		},
	}
	for _, tt := range tests {
		if got := javaHashMapOrder(tt.written); !slices.Equal(got, tt.want) {
			t.Errorf("javaHashMapOrder(%v) = %v, want %v", tt.written, got, tt.want)
		}
	}
	// Past twelve entries the table doubles to 32 buckets.
	many := names("A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K", "L", "Q")
	if got := javaHashMapOrder(many); got[0] != "A" || got[len(got)-1] != "Q" {
		t.Errorf("javaHashMapOrder over 13 keys = %v, want A first and Q last", got)
	}
}
