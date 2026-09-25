package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// twoPageCertifiedPDF is a document certified at the given DocMDP policy with
// two pages: the first (3) carries the certification signature widget (4,
// value 5) and a text field widget (6), the second (8) nothing.
func twoPageCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R 8 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}, "")
}

// TestEvaluateMDP_RemovedPage covers ISO 32000-1, 12.8.2.2, Table 254: no P
// permits deleting a page, whether the update rewrites the page tree or only
// frees the page in a cross-reference section, while instantiating a page
// template, which adds one, is permitted from P=2.
func TestEvaluateMDP_RemovedPage(t *testing.T) {
	pages := func(kids string, count int) func(t *testing.T, base []byte) []byte {
		return func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base, updatedObject{2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", kids, count)})
		}
	}
	freed := func(num int) func(t *testing.T, base []byte) []byte {
		return func(t *testing.T, base []byte) []byte { return freeObject(t, base, num) }
	}

	tests := []struct {
		name   string
		policy int
		update func(t *testing.T, base []byte) []byte
		want   bool
	}{
		{"P=1 page freed by the cross-reference section", 1, freed(8), false},
		{"P=2 page freed by the cross-reference section", 2, freed(8), false},
		{"P=2 page dropped from /Kids", 2, pages("3 0 R", 1), false},
		{"P=3 page dropped from /Kids", 3, pages("3 0 R", 1), false},

		{"P=1 unrelated object freed", 1, freed(9), true},
		{"P=2 pages reordered", 2, pages("8 0 R 3 0 R", 2), true},
		{"P=2 template page added", 2, func(t *testing.T, base []byte) []byte {
			return appendUpdate(t, base,
				updatedObject{2, "<< /Type /Pages /Kids [3 0 R 8 0 R 9 0 R] /Count 3 >>"},
				updatedObject{9, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>"},
			)
		}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := twoPageCertifiedPDF(tc.policy)
			result := mdpVerdict(t, tc.update(t, base), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if !tc.want && !strings.Contains(result.MDPInfo, "page 2 (object 8) was removed") {
				t.Errorf("MDPInfo %q does not name the removed page", result.MDPInfo)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: the signed revision was not read", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_PageRemovalUncheckedWithoutASignedRevision pins that a
// /ByteRange ending where no revision ends leaves the page comparison undone,
// and says so, rather than comparing against a guessed revision.
func TestEvaluateMDP_PageRemovalUncheckedWithoutASignedRevision(t *testing.T) {
	base := twoPageCertifiedPDF(1)
	result := mdpVerdict(t, freeObject(t, base, 8), bytes.Index(base, []byte("xref")), 1)
	if !strings.Contains(result.MDPInfo, "page removals not checked") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want MDPInfo to say page removals were not checked", result.MDPValid, result.MDPInfo)
	}
}

// streamObject is a stream object holding content.
func streamObject(content string) string {
	return fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

// signedContent is what the page of [contentCertifiedPDF] draws.
const signedContent = "BT /F1 12 Tf 72 700 Td (Pay 100) Tj ET"

// signedPage is the page of [contentCertifiedPDF] with the given /Annots
// entries and extra entries.
func signedPage(annots, extra string) updatedObject {
	return updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Resources 10 0 R /Annots [" + annots + "]" + extra + " >>"}
}

// contentCertifiedPDF is a document certified at the given DocMDP policy. Its
// page (3) draws a content stream (9) with a resource dictionary (10) naming a
// font (11), and lists the certification signature widget (4, value 5) and a
// text field widget (6). A free text annotation (8) is held by the document
// and listed by no page. catalogExtra is added to the catalog (1) and extra
// objects are numbered from 12.
func contentCertifiedPDF(policy int, catalogExtra string, extra ...string) []byte {
	return buildSigPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R" + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		signedPage("4 0 R 6 0 R", "").body,
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		"<< /Type /Annot /Subtype /FreeText /Rect [100 100 500 200] /DA (/Helv 48 Tf 1 0 0 rg) /Contents (VOID) >>",
		streamObject(signedContent),
		"<< /Font << /F1 11 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}, extra...), "")
}

// TestEvaluateMDP_ChangedPage covers ISO 32000-1, 12.8.2.2, Table 254: no P
// permits changing what a signed page draws. Filling in a form and signing
// reach a page only through the widgets its /Annots lists, P=3 adds annotation
// changes, and a page's content streams, resources, boxes and rotation, stated
// or inherited, stay as they were signed.
func TestEvaluateMDP_ChangedPage(t *testing.T) {
	tests := []struct {
		name     string
		policy   int
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 /Contents swapped", 2, []updatedObject{
			{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 40 0 R /Resources 10 0 R /Annots [4 0 R 6 0 R] >>"},
			{40, streamObject("BT /F1 12 Tf 72 700 Td (Pay 900) Tj ET")},
		}, false, "page 1 (object 3) changed its /Contents"},
		{"P=2 /Resources swapped", 2, []updatedObject{
			{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Resources 40 0 R /Annots [4 0 R 6 0 R] >>"},
			{40, "<< /Font << /F1 11 0 R >> /ExtGState << /GS << /CA 0 >> >> >>"},
		}, false, "page 1 (object 3) changed its /Resources"},
		{"P=2 /MediaBox changed", 2, []updatedObject{
			{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 400] /Contents 9 0 R /Resources 10 0 R /Annots [4 0 R 6 0 R] >>"},
		}, false, "page 1 (object 3) changed its /MediaBox"},
		{"P=3 /Rotate added", 3, []updatedObject{signedPage("4 0 R 6 0 R", " /Rotate 180")}, false, "page 1 (object 3) changed its /Rotate"},
		{"P=2 content stream redefined", 2, []updatedObject{
			{9, streamObject("BT /F1 12 Tf 72 700 Td (Pay 900) Tj ET")},
		}, false, "object 9 (untyped) redefines part of page 1"},
		{"P=3 content stream redefined", 3, []updatedObject{
			{9, streamObject("BT /F1 12 Tf 72 700 Td (Pay 900) Tj ET")},
		}, false, "object 9 (untyped) redefines part of page 1"},
		{"P=2 resource dictionary redefined", 2, []updatedObject{
			{10, "<< /Font << /F1 11 0 R >> /ExtGState << /GS << /CA 0 >> >> >>"},
		}, false, "object 10 (untyped) redefines part of page 1"},
		{"P=2 inherited /Rotate", 2, []updatedObject{
			{2, "<< /Type /Pages /Kids [3 0 R] /Count 1 /Rotate 90 >>"},
		}, false, "page 1 (object 3) inherits a changed /Rotate"},
		{"P=2 page moved under a node with other resources", 2, []updatedObject{
			{2, "<< /Type /Pages /Kids [40 0 R] /Count 1 >>"},
			{40, "<< /Type /Pages /Parent 2 0 R /Kids [3 0 R] /Count 1 /Resources << /XObject << /Im 11 0 R >> >> >>"},
			{3, "<< /Type /Page /Parent 40 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Resources 10 0 R /Annots [4 0 R 6 0 R] >>"},
		}, false, "page 1 (object 3) inherits a changed /Resources"},
		{"P=2 held annotation added to /Annots", 2, []updatedObject{signedPage("4 0 R 6 0 R 8 0 R", "")}, false, "annotation 8 (Annot:FreeText) was added to page 1"},

		{"P=2 form fill", 2, []updatedObject{
			{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /V (Jane) /AP << /N 40 0 R >> >>"},
			{40, formXObject("/Font << /F1 11 0 R >>", "/Tx BMC BT /F1 12 Tf (Jane) Tj ET EMC")},
		}, true, ""},
		{"P=2 visible approval signature", 2, []updatedObject{
			{7, "<< /Fields [4 0 R 6 0 R 40 0 R] /SigFlags 3 >>"},
			signedPage("4 0 R 6 0 R 40 0 R", ""),
			{40, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [300 300 400 350] /P 3 0 R /V 41 0 R /AP << /N 42 0 R >> >>"},
			{41, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> >>"},
			{42, formXObject("/Font << /F1 11 0 R >>", "BT /F1 8 Tf (Approved) Tj ET")},
		}, true, ""},
		{"P=2 page and content stream written again unchanged", 2, []updatedObject{
			signedPage("4 0 R 6 0 R", ""),
			{9, streamObject(signedContent)},
			{10, "<< /Font << /F1 11 0 R >> >>"},
		}, true, ""},
		{"P=2 page moved under a new node", 2, []updatedObject{
			{2, "<< /Type /Pages /Kids [40 0 R] /Count 1 >>"},
			{40, "<< /Type /Pages /Parent 2 0 R /Kids [3 0 R] /Count 1 >>"},
			{3, "<< /Type /Page /Parent 40 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Resources 10 0 R /Annots [4 0 R 6 0 R] >>"},
		}, true, ""},
		{"P=1 DSS", 1, dssUpdate(""), true, ""},
		{"P=2 DSS", 2, dssUpdate(""), true, ""},
		{"P=3 held annotation added to /Annots", 3, []updatedObject{signedPage("4 0 R 6 0 R 8 0 R", "")}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := contentCertifiedPDF(tc.policy, "")
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: the signed revision was not read", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_PageReach covers what a signed page reaches beyond its own
// dictionary: the walk follows a form XObject cycle to its end, and a
// reference the signed page makes to an object the signed revision does not
// define reads as null (ISO 32000-1, 7.3.10), so defining it later changes the
// page. The form XObjects carry no /Type, which Table 95 makes optional, so
// the classifier permits them and the page comparison is what reports them.
func TestEvaluateMDP_PageReach(t *testing.T) {
	base := buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /SigFlags 3 >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /Resources << /XObject << /X 7 0 R /Z 20 0 R >> >> /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		"<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P 2 /V /1.2 >> >>] >>",
		streamObject("/X Do /Z Do"),
		"<< /Subtype /Form /BBox [0 0 612 792] /Resources << /XObject << /Y 8 0 R >> >> /Length 5 >>\nstream\n/Y Do\nendstream",
		"<< /Subtype /Form /BBox [0 0 612 792] /Resources 9 0 R /Length 5 >>\nstream\n/X Do\nendstream",
		"<< /XObject << /X 7 0 R >> >>",
	}, "")
	void := "<< /Subtype /Form /BBox [0 0 612 792] /Length 36 >>\nstream\nBT /F1 48 Tf 100 400 Td (VOID) Tj ET\nendstream"

	tests := []struct {
		name     string
		update   []updatedObject
		want     bool
		reported string
	}{
		{"resources at the end of a cycle redefined", []updatedObject{
			{9, "<< /XObject << /X 7 0 R >> /ExtGState << /GS << /CA 0 >> >> >>"},
		}, false, "object 9 (untyped) redefines part of page 1"},
		{"dangling resource defined", []updatedObject{{20, void}}, false, "object 20 (untyped) redefines part of page 1"},
		{"object no page reaches defined", []updatedObject{{21, void}}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), 2)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
		})
	}
}

// TestEvaluateMDP_ContentPassedAsValidationData pins that validation data
// does not cover page content. A document whose signed DSS already lists the
// page's content stream passes its rewrite as validation data at P=1, and the
// page comparison still reports it.
func TestEvaluateMDP_ContentPassedAsValidationData(t *testing.T) {
	base := contentCertifiedPDF(1, " /DSS 12 0 R", "<< /Certs [9 0 R] >>")
	result := mdpVerdict(t, appendUpdate(t, base,
		updatedObject{12, "<< /Certs [9 0 R] >>"},
		updatedObject{9, streamObject("BT /F1 12 Tf 72 700 Td (Pay 900) Tj ET")},
	), len(base), 1)
	if result.MDPValid || !strings.Contains(result.MDPInfo, "object 9 (untyped) redefines part of page 1") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want the rewritten content stream, object 9, reported", result.MDPValid, result.MDPInfo)
	}
}
