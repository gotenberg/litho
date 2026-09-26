package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// annotatedCertifiedPDF is a document certified at the given DocMDP policy
// whose page lists, through annots, the certification signature widget (4,
// value 5), a text field widget (6) and a comment (8).
func annotatedCertifiedPDF(policy int, annots string) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots " + annots + " >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		"<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) >>",
		"[4 0 R 6 0 R 8 0 R]",
	}, "")
}

// freeObject appends an incremental update whose cross-reference section frees
// object num (ISO 32000-1, 7.5.4) and defines nothing.
func freeObject(t *testing.T, base []byte, num int) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write(base)
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 1\n0000000000 65535 f\r\n%d 1\n0000000000 00001 f\r\ntrailer\n<< /Size 10 /Root 1 0 R /Prev %d >>\nstartxref\n%d\n%%%%EOF\n",
		num, startXRef(t, base), xref)
	return buf.Bytes()
}

// TestEvaluateMDP_RemovedAnnotation covers ISO 32000-1, 12.8.2.2, Table 254:
// P=2 does not permit deleting an annotation, P=3 does, and a widget, which
// carries a form field, is not an annotation deletion P=2 reports.
func TestEvaluateMDP_RemovedAnnotation(t *testing.T) {
	page := func(annots string) updatedObject {
		return updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots " + annots + " >>"}
	}
	direct := "[4 0 R 6 0 R 8 0 R]"
	shared := "9 0 R"

	tests := []struct {
		name   string
		policy int
		annots string
		update func(t *testing.T, base []byte) []byte
		want   bool
	}{
		{"P=2 comment dropped from /Annots", 2, direct, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, page("[4 0 R 6 0 R]"))
		}, false},
		{"P=2 comment dropped from an indirect /Annots", 2, shared, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, updatedObject{9, "[4 0 R 6 0 R]"})
		}, false},
		{"P=2 comment redefined as null", 2, direct, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, updatedObject{8, "null"})
		}, false},
		{"P=2 comment freed by the cross-reference section", 2, direct, func(t *testing.T, base []byte) []byte {
			return freeObject(t, base, 8)
		}, false},
		{"P=2 widget dropped from /Annots", 2, direct, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, page("[4 0 R 8 0 R]"))
		}, true},
		{"P=2 annotations reordered", 2, direct, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, page("[8 0 R 6 0 R 4 0 R]"))
		}, true},
		{"P=3 comment dropped from /Annots", 3, direct, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, page("[4 0 R 6 0 R]"))
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := annotatedCertifiedPDF(tc.policy, tc.annots)
			result := mdpVerdict(t, tc.update(t, base), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if !tc.want && !strings.Contains(result.MDPInfo, "annotation 8 (Annot:Text) was removed from page 1") {
				t.Errorf("MDPInfo %q does not name the removed comment", result.MDPInfo)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: the signed revision was not read", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_RemovalUncheckedWithoutASignedRevision pins that a
// /ByteRange ending where no revision ends leaves the removal check undone,
// and says so, rather than comparing against a guessed revision.
func TestEvaluateMDP_RemovalUncheckedWithoutASignedRevision(t *testing.T) {
	base := annotatedCertifiedPDF(2, "[4 0 R 6 0 R 8 0 R]")
	data := appendUpdate(t, base, updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (Jane) >>"})
	signedEnd := bytes.Index(base, []byte("xref"))
	result := mdpVerdict(t, data, signedEnd, 2)
	if !result.MDPValid || !strings.Contains(result.MDPInfo, "annotation removals not checked") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want a pass that says removals were not checked", result.MDPValid, result.MDPInfo)
	}
}
