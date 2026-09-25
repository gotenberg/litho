package remediation

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/models"
)

// numberLimitsPDF builds a document whose object 4, which the catalog names,
// holds a number of each kind the implementation limits judge: a real past the
// PDF/A-1 limit and inside the PDF/A-2 one, a real past both, an integer past
// the integer range, an integer literal past the 64-bit range, which the parse
// reads as a real, a denormal real, and a number inside every limit. version
// is written as the catalog /Version when set.
func numberLimitsPDF(version string) []byte {
	catalog := "<< /Type /Catalog /Pages 2 0 R /Numbers 4 0 R"
	if version != "" {
		catalog += " /Version /" + version
	}
	catalog += " >>"
	return assemblePDF([]string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Real 40000.5 /Huge 1" + strings.Repeat("0", 40) + ".0 /Int 99999999999" +
			" /Literal 9" + strings.Repeat("9", 29) + " /Tiny 0." + strings.Repeat("0", 39) + "1" +
			" /Small 12.5 /Arr [40000.5 -99999999999] >>",
	})
}

// numberLimitsWant is what object 4 of [numberLimitsPDF] holds after a repair.
type numberLimitsWant struct {
	real, huge, tiny, arr0 float64
	integer, literal, arr1 any
}

// numberLimitsKept is object 4 of [numberLimitsPDF] as written.
var numberLimitsKept = numberLimitsWant{
	real: 40000.5, huge: 1e40, tiny: 1e-40, arr0: 40000.5,
	integer: ast.PdfInt(99999999999), literal: ast.PdfReal(1e30), arr1: ast.PdfInt(-99999999999),
}

// TestFixRealLimitsGatesEachRepair pins that the real-limits repair changes a
// number only when the violations name the test it fails under the part of
// the profile, and brings it to that part's limit. A real past 32767 is
// clamped under PDF/A-1 alone, which caps a real there (ISO 19005-1, 6.1.12
// t2), and PDF/A-2 clamps a real past 3.403e38 (ISO 19005-2, 6.1.13 t2) to
// that. A denormal real becomes 0 under the test that forbids it (ISO 19005-2,
// 6.1.13 t5). An integer, the literal the parse reads as a real among them,
// is clamped to the integer range under the integer test. PDF/A-4 states no
// numeric limit, and a rule of another part repairs nothing.
func TestFixRealLimitsGatesEachRepair(t *testing.T) {
	t.Parallel()

	clampedInts := numberLimitsKept
	clampedInts.integer, clampedInts.literal = ast.PdfInt(2147483647), ast.PdfInt(2147483647)
	clampedInts.arr1 = ast.PdfInt(-2147483648)
	realsA1 := numberLimitsKept
	realsA1.real, realsA1.huge, realsA1.arr0 = 32767, 32767, 32767
	realsA2 := numberLimitsKept
	realsA2.huge = compliance.MaxRealAbs2
	denormals := numberLimitsKept
	denormals.tiny = 0

	for _, tc := range []struct {
		name    string
		profile string
		rules   []compliance.Rule
		want    numberLimitsWant
		changed bool
	}{
		{"PDF/A-1 real limit", "PDF/A-1b", []compliance.Rule{compliance.Rule_19005_1_6_1_12_t02}, realsA1, true},
		{"PDF/A-1 integer limit", "PDF/A-1b", []compliance.Rule{compliance.Rule_19005_1_6_1_12_t01}, clampedInts, true},
		{"PDF/A-2 real limit", "PDF/A-2b", []compliance.Rule{compliance.Rule_19005_2_6_1_13_t02}, realsA2, true},
		{"PDF/A-2 denormal limit", "PDF/A-2b", []compliance.Rule{compliance.Rule_19005_2_6_1_13_t05}, denormals, true},
		{"PDF/A-3 integer limit", "PDF/A-3b", []compliance.Rule{compliance.Rule_19005_3_6_1_13_t01}, clampedInts, true},
		{"PDF/A-2 name limit", "PDF/A-2b", []compliance.Rule{compliance.Rule_19005_2_6_1_13_t04}, numberLimitsKept, false},
		{"PDF/A-4 catalog version", "PDF/A-4", []compliance.Rule{compliance.Rule_19005_4_6_1_12_t01}, numberLimitsKept, false},
		{"a PDF/A-1 rule under PDF/A-2", "PDF/A-2b", []compliance.Rule{compliance.Rule_19005_1_6_1_12_t02}, numberLimitsKept, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, alloc := newTestDoc(t, numberLimitsPDF(""))
			// Resolved first, as validation parses every object before a
			// repair runs, which records the literal read as a real.
			obj, err := doc.Resolve(context.Background(), ast.PdfIndirectRef{ObjectNum: 4})
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			violations := make([]compliance.Violation, 0, len(tc.rules))
			for _, r := range tc.rules {
				violations = append(violations, compliance.Violation{Rule: r, Context: "object 4: out of range"})
			}
			fixes, err := fixRealLimits(context.Background(), alloc, doc, Options{}, violations, tc.profile)
			if err != nil {
				t.Fatalf("fixRealLimits: %v", err)
			}
			pending, changed := alloc.Pending()[4]
			if changed != tc.changed || (len(fixes) > 0) != tc.changed {
				t.Fatalf("rewrote object 4: %v, reported %v, want %v", changed, fixes, tc.changed)
			}
			if !changed {
				pending = obj
			}
			assertNumberLimits(t, pending, tc.want)
		})
	}
}

// assertNumberLimits fails t unless obj, object 4 of [numberLimitsPDF], holds
// want.
func assertNumberLimits(t *testing.T, obj ast.PdfObject, want numberLimitsWant) {
	t.Helper()
	d, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("object 4 is %T", obj)
	}
	arr, _ := d["Arr"].(ast.PdfArray)
	if len(arr) != 2 {
		t.Fatalf("/Arr is %v", d["Arr"])
	}
	for _, c := range []struct {
		key  string
		got  ast.PdfObject
		want float64
	}{
		{"/Real", d["Real"], want.real},
		{"/Huge", d["Huge"], want.huge},
		{"/Tiny", d["Tiny"], want.tiny},
		{"/Arr[0]", arr[0], want.arr0},
		{"/Small", d["Small"], 12.5},
	} {
		r, ok := c.got.(ast.PdfReal)
		if !ok || !closeTo(float64(r), c.want) {
			t.Errorf("%s = %v (%T), want %v", c.key, c.got, c.got, c.want)
		}
	}
	for _, c := range []struct {
		key       string
		got, want any
	}{
		{"/Int", d["Int"], want.integer},
		{"/Literal", d["Literal"], want.literal},
		{"/Arr[1]", arr[1], want.arr1},
	} {
		if c.got != c.want {
			t.Errorf("%s = %v (%T), want %v (%T)", c.key, c.got, c.got, c.want, c.want)
		}
	}
}

// closeTo reports whether got is want to the precision a parsed real keeps.
func closeTo(got, want float64) bool {
	if want == 0 {
		return got == 0
	}
	return math.Abs(got-want) <= math.Abs(want)*1e-12
}

// TestRemediateNumberLimitsByPart pins the real-limits repair through the
// remediation run, whose violations come from validating the document. Under
// PDF/A-4 a catalog /Version that is not 2.n fails ISO 19005-4, 6.1.12 t1 and
// no numeric limit exists, so the numbers stay as written. Under PDF/A-2 a
// real of 40000.5, which ISO 19005-2, 6.1.13 t2 admits, stays as written while
// the integer past the range is clamped.
func TestRemediateNumberLimitsByPart(t *testing.T) {
	t.Parallel()

	t.Run("PDF/A-4", func(t *testing.T) {
		t.Parallel()

		doc, alloc := newTestDoc(t, numberLimitsPDF("1.7"))
		if _, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA4, Options{PreserveContent: true}); err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		if obj, rewritten := alloc.Pending()[4]; rewritten {
			t.Errorf("rewrote the numbers of object 4 under PDF/A-4: %v", obj)
		}
	})
	t.Run("PDF/A-2b", func(t *testing.T) {
		t.Parallel()

		doc, alloc := newTestDoc(t, numberLimitsPDF(""))
		if _, err := RemediatePDFA(context.Background(), doc, alloc, models.PDFA2b, Options{PreserveContent: true}); err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		want := numberLimitsKept
		want.huge = compliance.MaxRealAbs2
		want.tiny = 0
		want.integer, want.literal, want.arr1 = ast.PdfInt(2147483647), ast.PdfInt(2147483647), ast.PdfInt(-2147483648)
		assertNumberLimits(t, pendingDict(t, alloc, 4), want)
	})
}
