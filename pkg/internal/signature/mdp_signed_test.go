package signature

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// certification is the DocMDP certification signature value at policy.
func certification(policy int) string {
	return fmt.Sprintf("<< /Type /Sig /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /DocMDP /TransformParams << /Type /TransformParams /P %d /V /1.2 >> >>] >>", policy)
}

// TestEvaluateMDP_DanglingReferenceFilledIn covers ISO 32000-1, 7.3.10: a
// reference to an object the signed revision does not define reads as null,
// so an update that defines that number changes what the signed document
// means. It is a modification under 12.8.2.2, Table 254, and no exemption for
// new objects covers it: not validation data (ISO 32000-2, Table 257), not a
// new XFA datasets packet, not a new appearance stream. A number the signed
// revision neither defines nor refers to stays new.
func TestEvaluateMDP_DanglingReferenceFilledIn(t *testing.T) {
	const script = "<< /S /JavaScript /JS (app.alert\\(1\\)) >>"
	openAction := func(policy int) []byte { return contentCertifiedPDF(policy, " /OpenAction 40 0 R") }
	dss := func(num int) []updatedObject {
		return replaced(dssUpdate("/OpenAction 40 0 R "),
			updatedObject{20, fmt.Sprintf("<< /Type /DSS /Certs [21 0 R %d 0 R] /OCSPs [22 0 R] /VRI 23 0 R >>", num)},
			updatedObject{num, script},
		)
	}

	// The stamp (8) shows appearance 40, which the signed revision does not
	// define, and the text field widget (6) is drawn by 9.
	stamped := func(policy int) []byte {
		return buildSigPDF([]string{
			"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 8 0 R] >>",
			"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
			certification(policy),
			"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /AP << /N 9 0 R >> >>",
			"<< /Fields [4 0 R 6 0 R] /SigFlags 3 >>",
			"<< /Type /Annot /Subtype /Stamp /Rect [100 100 500 200] /P 3 0 R /AP << /N 40 0 R >> >>",
			formXObject("", "/Tx BMC EMC"),
		}, "")
	}
	redrawn := func(num int) []updatedObject {
		return []updatedObject{
			{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /V (Jane) /AP << /N 41 0 R >> >>"},
			{41, formXObject(fmt.Sprintf("/XObject << /S %d 0 R >>", num), "/Tx BMC EMC")},
			{num, formXObject("", "BT /F1 48 Tf 0 50 Td (VOID) Tj ET")},
		}
	}

	// The attachment (10) names its file stream 40, which the signed revision
	// does not define.
	attached := func(policy int) []byte {
		return buildSigPDF([]string{
			"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Names << /EmbeddedFiles << /Names [(invoice.xml) 10 0 R] >> >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
			certification(policy),
			"<< /Type /EmbeddedFile /Length 4 >>\nstream\n<a/>\nendstream",
			"<< /Fields [4 0 R] /SigFlags 3 /XFA [(template) 8 0 R (datasets) 6 0 R] >>",
			"<< /Type /EmbeddedFile /Length 11 >>\nstream\n<template/>\nendstream",
			"null",
			"<< /Type /Filespec /F (invoice.xml) /EF << /F 40 0 R >> >>",
		}, "")
	}
	datasets := func(num int) []updatedObject {
		return []updatedObject{
			{7, fmt.Sprintf("<< /Fields [4 0 R] /SigFlags 3 /XFA [(template) 8 0 R (datasets) %d 0 R] >>", num)},
			{num, "<< /Type /EmbeddedFile /Length 20 >>\nstream\n<total>9000</total>\nendstream"},
		}
	}

	tests := []struct {
		name     string
		policy   int
		base     func(policy int) []byte
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=1 open action filled in as validation data", 1, openAction, dss(40), false, "object 40 (untyped)"},
		{"P=2 stamp appearance filled in through a widget appearance", 2, stamped, redrawn(40), false, "object 40 (XObject)"},
		{"P=2 attachment filled in as the XFA datasets packet", 2, attached, datasets(40), false, "object 40 (EmbeddedFile)"},

		{"P=1 new validation data beside a dangling open action", 1, openAction, dss(42), true, ""},
		{"P=1 DSS beside a dangling open action", 1, openAction, dssUpdate("/OpenAction 40 0 R "), true, ""},
		{"P=2 new appearance beside a dangling stamp appearance", 2, stamped, redrawn(42), true, ""},
		{"P=2 new XFA datasets packet beside a dangling attachment", 2, attached, datasets(42), true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base(tc.policy)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not collected") {
				t.Errorf("MDPInfo %q: the signed revision's references were not collected", result.MDPInfo)
			}
		})
	}
}

// TestSignedRevision_Held pins what counts as held when the document was
// signed: a number the signed revision defines or refers to, and every number
// when its references could not be collected, which the note then says.
func TestSignedRevision_Held(t *testing.T) {
	ctx := context.Background()
	base := contentCertifiedPDF(2, " /OpenAction 40 0 R")
	data := appendUpdate(t, base, updatedObject{40, "<< /S /GoTo /D [3 0 R /Fit] >>"})
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	signed := newSignedRevision(doc, int64(len(base)))
	rev, err := signed.revision(ctx)
	if err != nil {
		t.Fatalf("reading the signed revision: %v", err)
	}

	for _, tc := range []struct {
		num  int
		want bool
	}{{9, true}, {40, true}, {41, false}} {
		if got := signed.held(ctx, rev, tc.num); got != tc.want {
			t.Errorf("held(%d) = %v, want %v", tc.num, got, tc.want)
		}
	}
	if note := signed.note(); note != "" {
		t.Errorf("note() = %q, want none", note)
	}

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	unwalked := newSignedRevision(doc, int64(len(base)))
	if !unwalked.held(canceled, rev, 41) {
		t.Error("held(41) = false with no references collected, want every number held")
	}
	if !strings.Contains(unwalked.note(), "none counts as new") {
		t.Errorf("note() = %q, want it to say no number counts as new", unwalked.note())
	}
}
