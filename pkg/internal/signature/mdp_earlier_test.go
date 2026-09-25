package signature

import (
	"fmt"
	"strings"
	"testing"
)

// sharedAppearanceCertifiedPDF is [appearanceCertifiedPDF] whose comment (9)
// is drawn by an appearance stream of its own (12).
func sharedAppearanceCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 9 0 R] /Resources << /Font << /F1 10 0 R >> >> /Contents 11 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /AP << /N 8 0 R >> >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 /DR << /Font << /F1 10 0 R >> >> >>",
		formXObject("/Font << /F1 10 0 R >>", "/Tx BMC EMC"),
		"<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) /AP << /N 12 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Length 26 >>\nstream\nBT /F1 12 Tf (Hello) Tj ET\nendstream",
		formXObject("", "1 1 0 rg 0 0 20 20 re f"),
	}, "")
}

// filledAs is the text field widget 6 of [appearanceCertifiedPDF] filled in
// with value and drawn by ap, with extra entries.
func filledAs(value, ap, extra string) updatedObject {
	return updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /V (" + value + ") /AP << /N " + ap + " >>" + extra + " >>"}
}

// drawn is an appearance stream showing text with the font /F1 names.
func drawn(text, font string) string {
	return formXObject("/Font << /F1 "+font+" >>", "/Tx BMC BT /F1 12 Tf ("+text+") Tj ET EMC")
}

// TestEvaluateMDP_EarlierAppearances covers ISO 32000-1, 12.8.2.2, Table 254
// across several incremental updates: each form fill redraws the widgets it
// fills in, and a later fill that redraws a widget again leaves the earlier
// appearance stream unreferenced. That stream was part of a permitted change in
// the revision its update made. An object no update drew with a widget it
// filled in, or that the signed document used elsewhere, stays a change.
func TestEvaluateMDP_EarlierAppearances(t *testing.T) {
	font := func(num int) updatedObject {
		return updatedObject{num, fmt.Sprintf("<< /Type /Font /Subtype /TrueType /BaseFont /Arial /FontDescriptor %d 0 R >>", num+1)}
	}
	descriptor := func(num int) updatedObject {
		return updatedObject{num, "<< /Type /FontDescriptor /FontName /Arial /Flags 32 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>"}
	}

	tests := []struct {
		name     string
		policy   int
		base     func(policy int) []byte
		updates  [][]updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 second fill leaves the first appearance", 2, appearanceCertifiedPDF, [][]updatedObject{
			{filledAs("Jane", "20 0 R", ""), {20, drawn("Jane", "10 0 R")}},
			{filledAs("John", "21 0 R", ""), {21, drawn("John", "10 0 R")}},
		}, true, ""},
		{"P=2 three fills, the first embedding a font", 2, appearanceCertifiedPDF, [][]updatedObject{
			{filledAs("Jane", "20 0 R", ""), {20, drawn("Jane", "21 0 R")}, font(21), descriptor(22)},
			{filledAs("John", "23 0 R", ""), {23, drawn("John", "10 0 R")}},
			{filledAs("Joan", "24 0 R", ""), {24, drawn("Joan", "10 0 R")}},
		}, true, ""},
		{"P=3 comment redrawn twice", 3, appearanceCertifiedPDF, [][]updatedObject{
			{{9, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (one) /AP << /N 20 0 R >> >>"}, {20, formXObject("", "0 g 0 0 20 20 re f")}},
			{{9, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (two) /AP << /N 21 0 R >> >>"}, {21, formXObject("", "1 g 0 0 20 20 re f")}},
		}, true, ""},

		{"P=1 second fill leaves the first appearance", 1, appearanceCertifiedPDF, [][]updatedObject{
			{filledAs("Jane", "20 0 R", ""), {20, drawn("Jane", "10 0 R")}},
			{filledAs("John", "21 0 R", ""), {21, drawn("John", "10 0 R")}},
		}, false, "added after signing"},
		{"P=2 earlier update adds an appearance stream no widget draws", 2, appearanceCertifiedPDF, [][]updatedObject{
			{filledAs("Jane", "20 0 R", ""), {20, drawn("Jane", "10 0 R")}, {30, formXObject("", "0 g 0 0 612 792 re f")}},
			{filledAs("John", "21 0 R", ""), {21, drawn("John", "10 0 R")}},
		}, false, "object 30 (XObject)"},
		{"P=2 earlier appearance of a widget changed beyond filling", 2, appearanceCertifiedPDF, [][]updatedObject{
			{filledAs("Jane", "20 0 R", " /Ff 4096"), {20, drawn("Jane", "10 0 R")}},
			{filledAs("John", "21 0 R", ""), {21, drawn("John", "10 0 R")}},
		}, false, "object 20 (XObject)"},
		{"P=2 comment appearance rewritten as a widget's own", 2, sharedAppearanceCertifiedPDF, [][]updatedObject{
			{filledAs("Jane", "12 0 R", "")},
			{filledAs("John", "12 0 R", ""), {12, drawn("John", "10 0 R")}},
		}, false, "object 12 (XObject)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base(tc.policy)
			data := base
			for _, update := range tc.updates {
				data = appendUpdate(t, data, update...)
			}
			result := mdpVerdict(t, data, len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not recognized") || strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a check did not run", result.MDPInfo)
			}
		})
	}
}
