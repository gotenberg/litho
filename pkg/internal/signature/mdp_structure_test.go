package signature

import (
	"strings"
	"testing"
)

// taggedCertifiedPDF is a tagged document certified at the given DocMDP
// policy. Its page (3) marks its content (8) as MCID 0 and lists the
// certification signature widget (4, value 5) and a text field widget (6) with
// /StructParent 1. The structure tree root (11) holds a Document element (12)
// whose kids are a paragraph (13) owning MCID 0 and a Form element (15) tagging
// the text field widget, and a parent tree (14) mapping key 0 to the paragraph
// and key 1 to the Form element.
func taggedCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /StructTreeRoot 11 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		taggedPage("4 0 R 6 0 R").body,
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /StructParent 1 >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
		streamObject("/P << /MCID 0 >> BDC BT /F1 12 Tf 72 700 Td (Pay 100) Tj ET EMC"),
		"<< /Font << /F1 10 0 R >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		taggedRoot("12 0 R", "2", "").body,
		documentElement("13 0 R 15 0 R").body,
		"<< /Type /StructElem /S /P /P 12 0 R /Pg 3 0 R /K 0 >>",
		parentTreeNode("0 [13 0 R] 1 15 0 R").body,
		"<< /Type /StructElem /S /Form /P 12 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 6 0 R /Pg 3 0 R >> >>",
	}, "")
}

// taggedPage is the page of [taggedCertifiedPDF] listing annots.
func taggedPage(annots string) updatedObject {
	return updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 8 0 R /Resources 9 0 R /StructParents 0 /Tabs /S /Annots [" + annots + "] >>"}
}

// taggedRoot is the structure tree root of [taggedCertifiedPDF] with the given
// kids, next parent tree key and extra entries.
func taggedRoot(kids, nextKey, extra string) updatedObject {
	return updatedObject{11, "<< /Type /StructTreeRoot /K " + kids + " /ParentTree 14 0 R /ParentTreeNextKey " + nextKey + " /RoleMap << /Custom /P >>" + extra + " >>"}
}

// documentElement is the Document element of [taggedCertifiedPDF] with the
// given kids.
func documentElement(kids string) updatedObject {
	return updatedObject{12, "<< /Type /StructElem /S /Document /P 11 0 R /K [" + kids + "] >>"}
}

// parentTreeNode is the parent tree of [taggedCertifiedPDF] with the given
// /Nums entries.
func parentTreeNode(nums string) updatedObject {
	return updatedObject{14, "<< /Nums [" + nums + "] >>"}
}

// taggedSigning is the update a PDF/UA signer writes to add a signature field
// (widget 40, value 41) to [taggedCertifiedPDF]: the widget keys parent tree
// entry 2 to a new Form element (42) that the Document element gains, and the
// form, the page, the parent tree and the root are rewritten for it. tag is the
// Form element's body, and with replaces objects of the update by number.
func taggedSigning(tag string, with ...updatedObject) []updatedObject {
	return replaced([]updatedObject{
		{40, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [300 300 400 350] /P 3 0 R /V 41 0 R /StructParent 2 >>"},
		{41, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> >>"},
		{42, tag},
		{7, "<< /Fields [4 0 R 6 0 R 40 0 R] /SigFlags 3 >>"},
		taggedPage("4 0 R 6 0 R 40 0 R"),
		documentElement("13 0 R 15 0 R 42 0 R"),
		parentTreeNode("0 [13 0 R] 1 15 0 R 2 42 0 R"),
		taggedRoot("12 0 R", "3", ""),
	}, with...)
}

// formTag is a Form element tagging the new signature widget 40 under the
// Document element, with extra entries.
func formTag(extra string) string {
	return "<< /Type /StructElem /S /Form /P 12 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 40 0 R /Pg 3 0 R >>" + extra + " >>"
}

// TestEvaluateMDP_AnnotationTags covers ISO 32000-1, 12.8.2.2, Table 254 in a
// tagged document. Signing is permitted from P=2 up and P=3 adds annotation
// creation, and ISO 14289-1, 7.18.1 and 7.18.4 require the annotation such a
// change adds to be tagged, a widget within a Form element. The structure
// element, the parent tree entry (ISO 32000-1, 14.7.4.4) and the kid its
// parent gains are part of the permitted change. Any other structure tree
// change is not: an existing element's entries or kids, the role map, a signed
// parent tree entry, an element that tags page content or an annotation the
// update did not write.
func TestEvaluateMDP_AnnotationTags(t *testing.T) {
	freeText := func(structParent string) updatedObject {
		return updatedObject{40, "<< /Type /Annot /Subtype /FreeText /Rect [300 300 400 350] /P 3 0 R /DA (/Helv 12 Tf 0 g) /Contents (note) /StructParent " + structParent + " >>"}
	}
	commentTag := "<< /Type /StructElem /S /Annot /P 12 0 R /K << /Type /OBJR /Obj 40 0 R >> >>"
	commenting := []updatedObject{
		freeText("2"),
		{42, commentTag},
		taggedPage("4 0 R 6 0 R 40 0 R"),
		documentElement("13 0 R 15 0 R 42 0 R"),
		parentTreeNode("0 [13 0 R] 1 15 0 R 2 42 0 R"),
		taggedRoot("12 0 R", "3", ""),
	}

	tests := []struct {
		name     string
		policy   int
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 signature widget tagged", 2, taggedSigning(formTag(" /Alt (Approval signature)")), true, ""},
		{"P=3 signature widget tagged", 3, taggedSigning(formTag("")), true, ""},
		{"P=2 signature widget tagged without /Type", 2, taggedSigning(
			"<< /S /Form /P 12 0 R /K << /Type /OBJR /Obj 40 0 R >> >>",
		), true, ""},
		{"P=2 object reference and kids written as objects", 2, taggedSigning(
			"<< /Type /StructElem /S /Form /P 12 0 R /K 43 0 R >>",
			updatedObject{43, "[44 0 R]"},
			updatedObject{44, "<< /Type /OBJR /Obj 40 0 R /Pg 3 0 R >>"},
		), true, ""},
		{"P=2 parent tree split into leaves", 2, taggedSigning(formTag(""),
			updatedObject{14, "<< /Kids [45 0 R 46 0 R] >>"},
			updatedObject{45, "<< /Limits [0 1] /Nums [0 [13 0 R] 1 15 0 R] >>"},
			updatedObject{46, "<< /Limits [2 2] /Nums [2 42 0 R] >>"},
		), true, ""},
		{"P=2 signature widget tagged under the root", 2, taggedSigning(
			"<< /Type /StructElem /S /Form /P 11 0 R /K << /Type /OBJR /Obj 40 0 R >> >>",
			documentElement("13 0 R 15 0 R"),
			taggedRoot("[12 0 R 42 0 R]", "3", ""),
		), true, ""},
		{"P=3 comment tagged", 3, commenting, true, ""},

		{"P=2 comment tagged", 2, commenting, false, "object 40 (Annot:FreeText) is an annotation"},
		{"P=2 existing element's alternate description changed", 2, taggedSigning(formTag(""),
			updatedObject{13, "<< /Type /StructElem /S /P /P 12 0 R /Pg 3 0 R /K 0 /Alt (Pay 900) >>"},
		), false, "object 13 (StructElem)"},
		{"P=2 existing element dropped from its parent's kids", 2, taggedSigning(formTag(""),
			documentElement("15 0 R 42 0 R"),
		), false, "is not a form field or signature"},
		{"P=2 existing element's /Type dropped", 2, taggedSigning(formTag(""),
			updatedObject{12, "<< /S /Document /P 11 0 R /K [13 0 R 15 0 R 42 0 R] >>"},
		), false, "is not a form field or signature"},
		{"P=2 Figure added over the page content", 2, taggedSigning(formTag(""),
			documentElement("13 0 R 15 0 R 42 0 R 43 0 R"),
			updatedObject{43, "<< /Type /StructElem /S /Figure /P 12 0 R /Pg 3 0 R /K 0 /Alt (Paid in full) >>"},
		), false, "violation"},
		{"P=2 signature widget tagged as a Figure", 2, taggedSigning(
			"<< /Type /StructElem /S /Figure /P 12 0 R /K << /Type /OBJR /Obj 40 0 R >> /Alt (Paid in full) >>",
		), false, "violation"},
		{"P=2 tag with attributes", 2, taggedSigning(formTag(" /A << /O /Layout /Placement /Block >>")), false, "violation"},
		{"P=2 role map changed", 2, taggedSigning(formTag(""),
			updatedObject{11, "<< /Type /StructTreeRoot /K 12 0 R /ParentTree 14 0 R /ParentTreeNextKey 3 /RoleMap << /Custom /Figure >> >>"},
		), false, "object 11 (StructTreeRoot)"},
		{"P=2 signed parent tree entry changed", 2, taggedSigning(formTag(""),
			parentTreeNode("0 [42 0 R] 1 15 0 R 2 42 0 R"),
		), false, "object 14 (untyped)"},
		{"P=2 widget the update did not write tagged", 2, taggedSigning(
			"<< /Type /StructElem /S /Form /P 12 0 R /K << /Type /OBJR /Obj 6 0 R >> >>",
			parentTreeNode("0 [13 0 R] 1 15 0 R 2 42 0 R"),
		), false, "violation"},
		{"P=1 signature widget tagged", 1, taggedSigning(formTag("")), false, "added after signing"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := taggedCertifiedPDF(tc.policy)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
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
