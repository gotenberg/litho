package remediation

import (
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestMatchOutputIntentCleanup_ClaimsPartOneSameProfileRule pins that the
// cleanup fixer claims ISO 19005-1, 6.2.2 t2, the part-1 form of the rule
// requiring every output intent to name one destination profile object. Part
// 1 numbers the clause 6.2.2 where parts 2-4 number it 6.2.3, so a clause
// prefix of "6.2.3" left the part-1 violation to corrupt-icc, which claims it
// and repairs nothing.
func TestMatchOutputIntentCleanup_ClaimsPartOneSameProfileRule(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		v    compliance.Violation
		want bool
	}{
		{"part 1 6.2.2 t2", compliance.Violation{
			Rule:    compliance.Rule_19005_1_6_2_2_t02,
			Context: "OutputIntent[1]: DestOutputProfile does not reference same object as first entry",
		}, true},
		{"part 2 6.2.3 t2", compliance.Violation{
			Rule:    compliance.Rule_19005_2_6_2_3_t02,
			Context: "OutputIntent[1]: DestOutputProfile does not reference same object as first entry",
		}, true},
		{"part 1 6.2.2 t1", compliance.Violation{
			Rule:    compliance.Rule_19005_1_6_2_2_t01,
			Context: "OutputIntent[0]: ICC profile header too short",
		}, false},
	} {
		if got := matchOutputIntentCleanup(tc.v); got != tc.want {
			t.Errorf("%s: matchOutputIntentCleanup = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestFixOutputIntentCleanup_KeepsThePDFAIntentProfile pins that unifying the
// destination profiles keeps the one of the GTS_PDFA1 intent, wherever it sits
// in the array, and points the other intents at it.
//
// The PDF/A intent's profile is the one that characterises the document's
// device colour under PDF/A (ISO 19005-2, 6.2.4.3). Unifying on the first
// entry instead gave the PDF/A intent the CMYK profile of a GTS_PDFX intent
// listed before it, which leaves the page's DeviceRGB uncovered and changes
// how every device colour of the PDF/A reading converts.
func TestFixOutputIntentCleanup_KeepsThePDFAIntentProfile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cmyk := deflateAt(t, cmykICCProfile, zlib.BestCompression)
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R 5 0 R] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S /GTS_PDFX /OutputConditionIdentifier (CGATS TR 001) /DestOutputProfile 6 0 R >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 7 0 R >>",
		iccObj(cmyk, 4),
		iccObj(srgb, 3),
	}))

	const pdfxIntent, pdfaIntent, rgbProfile = 4, 5, 7

	fixes, err := fixOutputIntentCleanup(ctx, alloc, doc, Options{}, nil, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixOutputIntentCleanup: %v", err)
	}
	if len(fixes) == 0 {
		t.Fatal("fixOutputIntentCleanup reported no fix: the intents name two profiles")
	}

	want := ast.PdfIndirectRef{ObjectNum: rgbProfile}
	for _, obj := range []int{pdfxIntent, pdfaIntent} {
		intent := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: obj})
		if got := intent["DestOutputProfile"]; got != want {
			t.Errorf("intent %d (/S %v) names /DestOutputProfile %v, want %v, the GTS_PDFA1 intent's",
				obj, intent["S"], got, want)
		}
	}
}

// TestFixOutputIntentCleanup_UnifiesADirectIntent pins that an intent written
// directly in the /OutputIntents array, which has no object number of its own
// (ISO 32000-1, 7.3.10), is unified too, by rewriting the array.
func TestFixOutputIntentCleanup_UnifiesADirectIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	srgb := deflateAt(t, sRGBICCProfile, zlib.BestCompression)
	doc, alloc := newTestDoc(t, assemblePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /OutputIntents [4 0 R << /Type /OutputIntent /S /GTS_PDFA1" +
			" /OutputConditionIdentifier (sRGB) /DestOutputProfile 5 0 R >>] >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 6 0 R >>",
		iccObj(srgb, 3),
		iccObj(srgb, 3),
	}))

	if _, err := fixOutputIntentCleanup(ctx, alloc, doc, Options{}, nil, "PDF/A-1b"); err != nil {
		t.Fatalf("fixOutputIntentCleanup: %v", err)
	}

	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: 1})
	arr, ok := catalog["OutputIntents"].(ast.PdfArray)
	if !ok || len(arr) != 2 {
		t.Fatalf("catalog /OutputIntents = %v, want a two-entry array", catalog["OutputIntents"])
	}
	direct, ok := arr[1].(ast.PdfDict)
	if !ok {
		t.Fatalf("OutputIntent[1] is %T, want the direct dictionary", arr[1])
	}
	want := ast.PdfIndirectRef{ObjectNum: 6}
	if got := direct["DestOutputProfile"]; got != want {
		t.Errorf("OutputIntent[1] names /DestOutputProfile %v, want %v", got, want)
	}
}
