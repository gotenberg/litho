package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// ltvBasePDF is a document certified at the given DocMDP policy, with a page
// content stream (9), the certification signature field (4, value 5) and a
// text field (6) in the form (7).
func ltvBasePDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		"null",
		"<< /Length 5 >>\nstream\nBT ET\nendstream",
	}, "")
}

// dssUpdate is the update a PAdES long-term validation writer appends: the
// catalog gains /DSS and the ESIC extension (ETSI EN 319 142-1, 5.6), and the
// DSS holds a certificate, an OCSP response and a VRI dictionary with its time
// stamp (5.4.2.2, 5.4.2.3).
func dssUpdate(catalogExtra string) []updatedObject {
	return []updatedObject{
		{1, "<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /DSS 20 0 R /Extensions << /ESIC << /BaseVersion /1.7 /ExtensionLevel 1 >> >> " + catalogExtra + ">>"},
		{20, "<< /Type /DSS /Certs [21 0 R] /OCSPs [22 0 R] /VRI 23 0 R >>"},
		{21, "<< /Length 4 >>\nstream\ncert\nendstream"},
		{22, "<< /Length 4 >>\nstream\nocsp\nendstream"},
		{23, "<< /ABCDEF0123456789ABCDEF0123456789ABCDEF01 24 0 R >>"},
		{24, "<< /Type /VRI /Cert [21 0 R] /OCSP [22 0 R] /TS 25 0 R >>"},
		{25, "<< /Length 2 >>\nstream\nts\nendstream"},
	}
}

// timestampUpdate is the update that adds a document timestamp (ETSI EN 319
// 142-1, 5.4.3; ISO 32000-2, 12.8.5): an invisible signature field merged with
// its widget, its /DocTimeStamp value and appearance, the form's /Fields and
// the page's /Annots each gaining the field.
func timestampUpdate(rect string, annotsExtra string) []updatedObject {
	return []updatedObject{
		{7, "<< /Fields [4 0 R 6 0 R 30 0 R] /SigFlags 3 >>"},
		{30, "<< /FT /Sig /Type /Annot /Subtype /Widget /T (timestamp) /Rect " + rect + " /P 3 0 R /V 31 0 R /AP << /N 32 0 R >> >>"},
		{31, "<< /Type /DocTimeStamp /Filter /Adobe.PPKLite /SubFilter /ETSI.RFC3161 /ByteRange [0 1 1 1] /Contents <30> >>"},
		{32, "<< /Type /XObject /Subtype /Form /BBox [0 0 0 0] /Length 0 >>\nstream\n\nendstream"},
		{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Annots [4 0 R 6 0 R 30 0 R" + annotsExtra + "] >>"},
	}
}

// replaced returns objs with each of with in place of the object of the same
// number, or added when objs has none. An update defines an object once: the
// cross-reference section keeps one entry per number.
func replaced(objs []updatedObject, with ...updatedObject) []updatedObject {
	out := append([]updatedObject(nil), objs...)
	for _, w := range with {
		found := false
		for i := range out {
			if out[i].num == w.num {
				out[i], found = w, true
			}
		}
		if !found {
			out = append(out, w)
		}
	}
	return out
}

// TestEvaluateMDP_ValidationData covers ISO 32000-2, 12.8.2.2, Table 257: an
// update that only adds a DSS or a document timestamp is not a change at any
// permission, and whatever else it adds still is.
func TestEvaluateMDP_ValidationData(t *testing.T) {
	type step []updatedObject
	comment := updatedObject{40, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) >>"}

	tests := []struct {
		name     string
		policy   int
		steps    []step
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=1 DSS", 1, []step{dssUpdate("")}, true, ""},
		{"P=1 document timestamp", 1, []step{timestampUpdate("[0 0 0 0]", "")}, true, ""},
		{"P=1 DSS then document timestamp", 1, []step{dssUpdate(""), timestampUpdate("[0 0 0 0]", "")}, true, ""},
		{"P=2 DSS", 2, []step{dssUpdate("")}, true, ""},
		{"P=2 document timestamp", 2, []step{timestampUpdate("[0 0 0 0]", "")}, true, ""},
		{"P=3 DSS then document timestamp", 3, []step{dssUpdate(""), timestampUpdate("[0 0 0 0]", "")}, true, ""},

		{"P=1 DSS with another catalog change", 1, []step{dssUpdate("/OpenAction [3 0 R /Fit] ")}, false, "object 1 (Catalog)"},
		{"P=1 DSS naming the page content it rewrites", 1, []step{replaced(dssUpdate(""),
			updatedObject{20, "<< /Type /DSS /Certs [21 0 R 9 0 R] /OCSPs [22 0 R] /VRI 23 0 R >>"},
			updatedObject{9, "<< /Length 8 >>\nstream\nBT 0 Tr ET\nendstream"},
		)}, false, "object 9 (untyped)"},
		{"P=1 document timestamp beside a new text field", 1, []step{replaced(timestampUpdate("[0 0 0 0]", ""),
			updatedObject{7, "<< /Fields [4 0 R 6 0 R 30 0 R 41 0 R] /SigFlags 3 >>"},
			updatedObject{41, "<< /FT /Tx /T (extra) >>"},
		)}, false, "object 7 (untyped)"},
		{"P=1 document timestamp beside a comment", 1, []step{replaced(timestampUpdate("[0 0 0 0]", " 40 0 R"), comment)}, false, "object 3 (Page)"},
		{"P=1 visible document timestamp appearance", 1, []step{timestampUpdate("[10 10 110 40]", "")}, false, "object 32 (XObject)"},
		{"P=1 document timestamp widget with actions", 1, []step{replaced(timestampUpdate("[0 0 0 0]", ""),
			updatedObject{30, "<< /FT /Sig /Type /Annot /Subtype /Widget /T (timestamp) /Rect [0 0 0 0] /P 3 0 R /V 31 0 R /AP << /N 32 0 R >> /AA << /PO << /S /JavaScript /JS (app.alert(1)) >> >> >>"},
		)}, false, "object 7 (untyped)"},
		{"P=2 document timestamp beside a comment", 2, []step{replaced(timestampUpdate("[0 0 0 0]", " 40 0 R"), comment)}, false, "object 40 (Annot:Text)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := ltvBasePDF(tc.policy)
			data := base
			for _, s := range tc.steps {
				data = appendUpdate(t, data, s...)
			}
			result := mdpVerdict(t, data, len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.want && !strings.Contains(result.MDPInfo, "adding validation data or document timestamps") {
				t.Errorf("MDPInfo %q does not count the validation data", result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
		})
	}
}

// TestEvaluateMDP_ValidationDataWithoutASignedRevision pins that validation
// data is exempted only against the revision the signature covers: when the
// /ByteRange ends where no revision ends, a P=1 DSS update stays a change, and
// MDPInfo says why.
func TestEvaluateMDP_ValidationDataWithoutASignedRevision(t *testing.T) {
	base := ltvBasePDF(1)
	data := appendUpdate(t, base, dssUpdate("")...)
	signedEnd := bytes.Index(base, []byte("xref"))
	result := mdpVerdict(t, data, signedEnd, 1)
	if result.MDPValid || !strings.Contains(result.MDPInfo, "validation data not recognized") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want a violation that says the validation data was not recognized", result.MDPValid, result.MDPInfo)
	}
}

// dropUpdate is a later long-term validation update that rewrites the DSS of
// [dssUpdate] without its OCSP response (22), which it leaves defined.
func dropUpdate() []updatedObject {
	return []updatedObject{
		{20, "<< /Type /DSS /Certs [21 0 R] /VRI 23 0 R >>"},
		{24, "<< /Type /VRI /Cert [21 0 R] /TS 25 0 R >>"},
	}
}

// TestEvaluateMDP_DroppedValidationData covers ISO 32000-2, 12.8.2.2, Table
// 257 across several updates: a stream an earlier update added as validation
// data and a later one dropped from the DSS is no change, while a stream the
// later update rewrites or puts to another use still is.
func TestEvaluateMDP_DroppedValidationData(t *testing.T) {
	withDSS := func(t *testing.T, base []byte) []byte {
		return appendUpdate(t, base, dssUpdate("")...)
	}
	tests := []struct {
		name     string
		policy   int
		steps    func(t *testing.T, base []byte) []byte
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=1 OCSP response dropped", 1, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, withDSS(t, base), dropUpdate()...)
		}, true, ""},
		{"P=1 OCSP response dropped and freed", 1, func(t *testing.T, base []byte) []byte {
			return freeObject(t, appendUpdate(t, withDSS(t, base), dropUpdate()...), 22)
		}, true, ""},
		{"P=2 VRI dictionary dropped", 2, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, withDSS(t, base),
				updatedObject{23, "<< /0123456789ABCDEF0123456789ABCDEF01234567 26 0 R >>"},
				updatedObject{26, "<< /Type /VRI /Cert [21 0 R] >>"},
			)
		}, true, ""},

		{"P=1 OCSP response dropped and rewritten", 1, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, withDSS(t, base),
				replaced(dropUpdate(), updatedObject{22, "<< /Length 5 >>\nstream\nother\nendstream"})...)
		}, false, "object 22 (untyped)"},
		{"P=1 OCSP response dropped into the page content", 1, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, withDSS(t, base),
				replaced(dropUpdate(), updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents [9 0 R 22 0 R] /Annots [4 0 R 6 0 R] >>"})...)
		}, false, "object 3 (Page)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := ltvBasePDF(tc.policy)
			result := mdpVerdict(t, tc.steps(t, base), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.want && !strings.Contains(result.MDPInfo, "adding validation data or document timestamps") {
				t.Errorf("MDPInfo %q does not count the validation data", result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not recognized") || strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a revision was not read", result.MDPInfo)
			}
		})
	}
}
