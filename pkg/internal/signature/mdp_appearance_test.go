package signature

import (
	"fmt"
	"strings"
	"testing"
)

// appearanceCertifiedPDF is a document certified at the given DocMDP policy.
// Its page (3) draws with a font (10) that the text field widget's (6)
// appearance stream (8) shares, and lists the certification signature widget
// (4, value 5), that text field widget and a comment (9).
func appearanceCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 9 0 R] /Resources << /Font << /F1 10 0 R >> >> /Contents 11 0 R >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /AP << /N 8 0 R >> >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 /DR << /Font << /F1 10 0 R >> >> >>",
		formXObject("/Font << /F1 10 0 R >>", "/Tx BMC EMC"),
		"<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Length 26 >>\nstream\nBT /F1 12 Tf (Hello) Tj ET\nendstream",
	}, "")
}

// formXObject is a form XObject, as an appearance stream is (ISO 32000-1,
// 12.5.5), with the given resources and content.
func formXObject(resources, content string) string {
	return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 100 20] /Resources << %s >> /Length %d >>\nstream\n%s\nendstream", resources, len(content), content)
}

// filledWidget is the text field widget 6 filled in and drawn by ap.
func filledWidget(ap string) updatedObject {
	return updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /V (Jane) /AP << /N " + ap + " >> >>"}
}

// TestEvaluateMDP_AppearanceStreams covers ISO 32000-1, 12.8.2.2, Table 254:
// filling in a field or signing redraws a widget, and the XObjects and fonts
// its new appearance draws with are part of that change from P=2 up. They are
// only when a widget the update rewrote draws with them and they are new, or
// were that widget's own appearance stream when the document was signed.
func TestEvaluateMDP_AppearanceStreams(t *testing.T) {
	font := func(num int, base string) updatedObject {
		return updatedObject{num, "<< /Type /Font /Subtype /TrueType /BaseFont /" + base + " /FontDescriptor " + fmt.Sprint(num+1) + " 0 R >>"}
	}
	descriptor := func(num int, base string) updatedObject {
		return updatedObject{num, "<< /Type /FontDescriptor /FontName /" + base + " /Flags 32 /FontBBox [0 0 1000 1000] /ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 >>"}
	}
	page := func(extra string) updatedObject {
		return updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 9 0 R" + extra + "] /Resources << /Font << /F1 10 0 R >> >> /Contents 11 0 R >>"}
	}

	tests := []struct {
		name     string
		policy   int
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 new appearance stream", 2, []updatedObject{
			filledWidget("20 0 R"),
			{20, formXObject("/Font << /F1 10 0 R >>", "/Tx BMC BT /F1 12 Tf (Jane) Tj ET EMC")},
		}, true, ""},
		{"P=2 new appearance stream with a new embedded font", 2, []updatedObject{
			filledWidget("20 0 R"),
			{20, formXObject("/Font << /F2 21 0 R >>", "/Tx BMC BT /F2 12 Tf (Jane) Tj ET EMC")},
			font(21, "Arial"), descriptor(22, "Arial"),
		}, true, ""},
		{"P=2 own appearance stream rewritten", 2, []updatedObject{
			filledWidget("8 0 R"),
			{8, formXObject("/Font << /F1 10 0 R >>", "/Tx BMC BT /F1 12 Tf (Jane) Tj ET EMC")},
		}, true, ""},
		{"P=2 appearance states", 2, []updatedObject{
			filledWidget("<< /Off 20 0 R /Yes 21 0 R >>"),
			{20, formXObject("", "")},
			{21, formXObject("/Font << /F1 10 0 R >>", "BT /F1 12 Tf (4) Tj ET")},
		}, true, ""},
		{"P=2 visible approval signature", 2, []updatedObject{
			{7, "<< /Fields [4 0 R 6 0 R 30 0 R] /SigFlags 3 /DR << /Font << /F1 10 0 R >> >> >>"},
			page(" 30 0 R"),
			{30, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [300 300 400 350] /P 3 0 R /V 31 0 R /AP << /N 32 0 R >> >>"},
			{31, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> >>"},
			{32, formXObject("/XObject << /Img 33 0 R >> /Font << /F3 34 0 R >> /ExtGState << /GS 36 0 R >>", "q /GS gs 100 0 0 50 0 0 cm /Img Do Q BT /F3 8 Tf (Signed) Tj ET")},
			{33, "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream"},
			font(34, "Courier"), descriptor(35, "Courier"),
			{36, "<< /Type /ExtGState /CA 0.5 >>"},
		}, true, ""},
		{"P=2 appearance whose resources cycle", 2, []updatedObject{
			filledWidget("20 0 R"),
			{20, formXObject("/XObject << /Self 20 0 R /Other 21 0 R >>", "/Other Do")},
			{21, formXObject("/XObject << /Back 20 0 R >>", "/Back Do")},
		}, true, ""},
		{"P=3 comment appearance", 3, []updatedObject{
			{9, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (edited) /AP << /N 20 0 R >> >>"},
			{20, formXObject("", "0 g 0 0 20 20 re f")},
		}, true, ""},

		{"P=1 new appearance stream", 1, []updatedObject{
			filledWidget("20 0 R"),
			{20, formXObject("/Font << /F1 10 0 R >>", "/Tx BMC EMC")},
		}, false, "object 6 (Annot:Widget)"},
		{"P=2 appearance stream no widget draws", 2, []updatedObject{
			{20, formXObject("", "0 g 0 0 20 20 re f")},
		}, false, "object 20 (XObject)"},
		{"P=2 page font rewritten through a widget appearance", 2, []updatedObject{
			filledWidget("8 0 R"),
			{10, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>"},
		}, false, "object 10 (Font)"},
		{"P=2 page font the walk reaches only through the page", 2, []updatedObject{
			filledWidget("20 0 R"),
			{20, formXObject("/XObject << /Page 3 0 R >>", "")},
			{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 9 0 R] /Resources << /Font << /F1 21 0 R >> >> /Contents 11 0 R >>"},
			font(21, "Arial"), descriptor(22, "Arial"),
		}, false, "object 21 (Font)"},
		{"P=2 comment appearance", 2, []updatedObject{
			{9, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (edited) /AP << /N 20 0 R >> >>"},
			{20, formXObject("", "0 g 0 0 20 20 re f")},
		}, false, "object 9 (Annot:Text)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := appearanceCertifiedPDF(tc.policy)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
		})
	}
}
