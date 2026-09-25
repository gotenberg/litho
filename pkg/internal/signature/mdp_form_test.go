package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// xfaCertifiedPDF is a document certified at the given DocMDP policy whose
// interactive form (7) carries xfa as its /XFA entry. The XFA packets are the
// datasets (6) and the template (8), both typed /EmbeddedFile as some
// producers write them, and the page (3) draws an image (9).
func xfaCertifiedPDF(policy int, xfa string) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] /Resources << /XObject << /Im 9 0 R >> >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy),
		"<< /Type /EmbeddedFile /Length 4 >>\nstream\n<a/>\nendstream",
		"<< /Fields [4 0 R] /SigFlags 3 /XFA " + xfa + " >>",
		"<< /Type /EmbeddedFile /Length 11 >>\nstream\n<template/>\nendstream",
		"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\x00\nendstream",
	}, "")
}

// TestEvaluateMDP_XFADatasets covers ISO 32000-1, 12.8.2.2, Table 254: from
// P=2, filling in an XFA form rewrites the datasets packet the /XFA array
// names (12.7.8), whatever type its producer gives it, and that is the only
// packet a form fill changes.
func TestEvaluateMDP_XFADatasets(t *testing.T) {
	const packets = "[(template) 8 0 R (datasets) 6 0 R]"
	filled := updatedObject{6, "<< /Type /EmbeddedFile /Length 11 >>\nstream\n<a>Jane</a>\nendstream"}

	tests := []struct {
		name     string
		policy   int
		xfa      string
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 datasets rewritten", 2, packets, []updatedObject{filled}, true, ""},
		{"P=3 datasets rewritten", 3, packets, []updatedObject{filled}, true, ""},
		{"P=2 new datasets stream the form now names", 2, packets, []updatedObject{
			{7, "<< /Fields [4 0 R] /SigFlags 3 /XFA [(template) 8 0 R (datasets) 10 0 R] >>"},
			{10, "<< /Type /EmbeddedFile /Length 11 >>\nstream\n<a>Jane</a>\nendstream"},
		}, true, ""},

		{"P=1 datasets rewritten", 1, packets, []updatedObject{filled}, false, "object 6 (EmbeddedFile)"},
		{"P=2 template rewritten", 2, packets, []updatedObject{
			{8, "<< /Type /EmbeddedFile /Length 15 >>\nstream\n<template new/>\nendstream"},
		}, false, "object 8 (EmbeddedFile)"},
		{"P=2 page image renamed as the datasets packet", 2, packets, []updatedObject{
			{7, "<< /Fields [4 0 R] /SigFlags 3 /XFA [(template) 8 0 R (datasets) 9 0 R] >>"},
			{9, "<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n\xff\nendstream"},
		}, false, "object 9 (XObject)"},
		{"P=2 datasets named as a dictionary", 2, packets, []updatedObject{
			{6, "<< /Type /EmbeddedFile /Contents (Jane) >>"},
		}, false, "object 6 (EmbeddedFile)"},
		{"P=2 single XFA stream rewritten", 2, "6 0 R", []updatedObject{filled}, false, "object 6 (EmbeddedFile)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := xfaCertifiedPDF(tc.policy, tc.xfa)
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

// TestEvaluateMDP_XFADatasetsWithoutASignedRevision pins that the datasets
// packet is recognized only against the revision the signature covers: when
// the /ByteRange ends where no revision ends, a rewritten datasets stream stays
// a change, and MDPInfo says why.
func TestEvaluateMDP_XFADatasetsWithoutASignedRevision(t *testing.T) {
	base := xfaCertifiedPDF(2, "[(template) 8 0 R (datasets) 6 0 R]")
	data := appendUpdate(t, base, updatedObject{6, "<< /Type /EmbeddedFile /Length 11 >>\nstream\n<a>Jane</a>\nendstream"})
	result := mdpVerdict(t, data, bytes.Index(base, []byte("xref")), 2)
	if result.MDPValid || !strings.Contains(result.MDPInfo, "XFA form data not recognized") {
		t.Errorf("MDPValid = %v, MDPInfo %q: want a violation that says the form data was not recognized", result.MDPValid, result.MDPInfo)
	}
}
