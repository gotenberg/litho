package remediation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// pdfA1DeviceNViolation is the violation that selects the 8-colourant trim,
// for tests that exercise that repair's write path.
var pdfA1DeviceNViolation = []compliance.Violation{{Rule: compliance.Rule_19005_1_6_1_12_t09}}

// Object numbers in [limitsPDF].
const (
	limitsContentObj = 4
	limitsCS12Obj    = 5
	limitsCS33Obj    = 6
	limitsBigDictObj = 8
)

// limitsQDepth nests the fixture's content two levels past [pdfAGraphicsDepth].
const limitsQDepth = pdfAGraphicsDepth + 2

// deviceNNames renders n colourant names with the given prefix.
func deviceNNames(prefix string, n int) string {
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("/%s%02d", prefix, i)
	}
	return strings.Join(names, " ")
}

// limitsPDF builds one page that breaks every limit fixAggressiveLimits
// repairs, each on its own object, so a test can tell which repairs ran:
//
//   - the content stream (object 4) nests q/Q [limitsQDepth] levels deep;
//   - two indirect DeviceN spaces carry 12 (object 5) and 33 (object 6)
//     colourants, one between the part 1 and part 2/3 caps and one above
//     both;
//   - an indirect dictionary (object 8) holds one entry past [pdfADictMax].
func limitsPDF() []byte {
	content := strings.Repeat("q ", limitsQDepth) + strings.Repeat("Q ", limitsQDepth)
	bigDict := make([]string, 0, pdfADictMax+1)
	for i := range pdfADictMax + 1 {
		bigDict = append(bigDict, fmt.Sprintf("/K%04d %d", i, i))
	}
	return assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /ColorSpace << /CS12 5 0 R /CS33 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"[/DeviceN [" + deviceNNames("C", 12) + "] /DeviceCMYK 7 0 R]",
		"[/DeviceN [" + deviceNNames("D", 33) + "] /DeviceCMYK 7 0 R]",
		"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0.1 0 0 0] /N 1 >>",
		"<< " + strings.Join(bigDict, " ") + " >>",
	})
}

// limitsOutcome records what one fixAggressiveLimits run changed in
// [limitsPDF]: whether the content was rewritten, how many entries the large
// dictionary keeps, and how many colourants each DeviceN space keeps.
type limitsOutcome struct {
	flattened   bool
	dictEntries int
	cs12, cs33  int
}

// runAggressiveLimits runs the fixer on a fresh [limitsPDF] with one matched
// violation per rule and reads the live view of each object back.
func runAggressiveLimits(t *testing.T, profile string, rules []compliance.Rule, preserve bool) limitsOutcome {
	t.Helper()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, limitsPDF())
	violations := make([]compliance.Violation, len(rules))
	for i, rule := range rules {
		violations[i] = compliance.Violation{Rule: rule}
	}
	if _, err := fixAggressiveLimits(ctx, alloc, doc, Options{PreserveContent: preserve}, violations, profile); err != nil {
		t.Fatalf("fixAggressiveLimits: %v", err)
	}

	var out limitsOutcome
	_, out.flattened = alloc.Pending()[limitsContentObj].(ast.PdfStream)
	if d, ok := alloc.Pending()[limitsBigDictObj].(ast.PdfDict); ok {
		out.dictEntries = len(d)
	} else if d, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: limitsBigDictObj}); err == nil {
		out.dictEntries = len(d)
	}
	colourants := func(objNum int) int {
		arr := liveArray(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if len(arr) < 2 {
			t.Fatalf("object %d is no longer a DeviceN array: %v", objNum, arr)
		}
		names, err := document.ResolveArray(ctx, doc, arr[1])
		if err != nil {
			t.Fatalf("object %d colourant names: %v", objNum, err)
		}
		return len(names)
	}
	out.cs12 = colourants(limitsCS12Obj)
	out.cs33 = colourants(limitsCS33Obj)
	return out
}

// TestFixAggressiveLimits_RepairsFollowTheProfileAndTheViolation pins that
// each lossy repair answers only the rule stating its limit in the profile's
// own part.
//
// The fixer used to run every repair with the PDF/A-1 limits whatever the
// profile and whichever limit was violated. Under PDF/A-2 or PDF/A-3, any
// match, such as q/Q nesting or a CID value, also truncated dictionaries to
// 4095 entries, which only ISO 19005-1, 6.1.12 test 6 caps, and trimmed
// DeviceN to 8 colourants, where ISO 19005-2/3, 6.1.13 test 9 allows 32.
//
// The "part 1, every limit" row is the positive control: it proves the
// fixture reaches all three writes, so the rows expecting no write are not
// passing on a fixture that cannot fail.
//
// # Corpus evidence
//
//   - litho: limits_1b.pdf, remediated as PDF/A-2b or PDF/A-3b in aggressive
//     mode, lost one entry of its 4096-entry dictionary to a 6.1.13 test 10
//     CID violation, a rule neither part states for dictionaries.
func TestFixAggressiveLimits_RepairsFollowTheProfileAndTheViolation(t *testing.T) {
	t.Parallel()

	const (
		untouchedDict = pdfADictMax + 1
		untouched12   = 12
		untouched33   = 33
	)
	for _, tc := range []struct {
		name    string
		profile string
		rules   []compliance.Rule
		want    limitsOutcome
	}{
		{
			name: "part 1, every limit", profile: "PDF/A-1b",
			rules: []compliance.Rule{compliance.Rule_19005_1_6_1_12_t06, compliance.Rule_19005_1_6_1_12_t08, compliance.Rule_19005_1_6_1_12_t09},
			want:  limitsOutcome{flattened: true, dictEntries: pdfADictMax, cs12: pdfA1DeviceNComponents, cs33: pdfA1DeviceNComponents},
		},
		{
			name: "part 1, q/Q nesting only", profile: "PDF/A-1b",
			rules: []compliance.Rule{compliance.Rule_19005_1_6_1_12_t08},
			want:  limitsOutcome{flattened: true, dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 1, dictionary only", profile: "PDF/A-1b",
			rules: []compliance.Rule{compliance.Rule_19005_1_6_1_12_t06},
			want:  limitsOutcome{dictEntries: pdfADictMax, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 1, DeviceN only", profile: "PDF/A-1b",
			rules: []compliance.Rule{compliance.Rule_19005_1_6_1_12_t09},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: pdfA1DeviceNComponents, cs33: pdfA1DeviceNComponents},
		},
		{
			name: "part 2, q/Q nesting", profile: "PDF/A-2b",
			rules: []compliance.Rule{compliance.Rule_19005_2_6_1_13_t08},
			want:  limitsOutcome{flattened: true, dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 3, q/Q nesting", profile: "PDF/A-3b",
			rules: []compliance.Rule{compliance.Rule_19005_3_6_1_13_t08},
			want:  limitsOutcome{flattened: true, dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 2, DeviceN", profile: "PDF/A-2b",
			rules: []compliance.Rule{compliance.Rule_19005_2_6_1_13_t09},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: untouched12, cs33: pdfA23DeviceNComponents},
		},
		{
			name: "part 3, DeviceN", profile: "PDF/A-3u",
			rules: []compliance.Rule{compliance.Rule_19005_3_6_1_13_t09},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: untouched12, cs33: pdfA23DeviceNComponents},
		},
		{
			name: "part 2, CID value", profile: "PDF/A-2b",
			rules: []compliance.Rule{compliance.Rule_19005_2_6_1_13_t10},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 3, CID value", profile: "PDF/A-3b",
			rules: []compliance.Rule{compliance.Rule_19005_3_6_1_13_t10},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 1 rules under a part 2 profile", profile: "PDF/A-2b",
			rules: []compliance.Rule{compliance.Rule_19005_1_6_1_12_t06, compliance.Rule_19005_1_6_1_12_t08, compliance.Rule_19005_1_6_1_12_t09},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
		{
			name: "part 4 states none of these limits", profile: "PDF/A-4",
			rules: []compliance.Rule{compliance.Rule_19005_2_6_1_13_t08, compliance.Rule_19005_2_6_1_13_t09},
			want:  limitsOutcome{dictEntries: untouchedDict, cs12: untouched12, cs33: untouched33},
		},
	} {
		if got := runAggressiveLimits(t, tc.profile, tc.rules, false); got != tc.want {
			t.Errorf("%s (%s): got %+v, want %+v", tc.name, tc.profile, got, tc.want)
		}
	}
}

// TestFixAggressiveLimits_PreserveContentRunsNoRepair is the gate's control:
// every repair is lossy, so standard mode declines them all even when every
// limit is violated.
func TestFixAggressiveLimits_PreserveContentRunsNoRepair(t *testing.T) {
	t.Parallel()

	rules := []compliance.Rule{compliance.Rule_19005_1_6_1_12_t06, compliance.Rule_19005_1_6_1_12_t08, compliance.Rule_19005_1_6_1_12_t09}
	want := limitsOutcome{dictEntries: pdfADictMax + 1, cs12: 12, cs33: 33}
	if got := runAggressiveLimits(t, "PDF/A-1b", rules, true); got != want {
		t.Errorf("PreserveContent: got %+v, want %+v", got, want)
	}
}
