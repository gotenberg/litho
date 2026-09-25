package signature

import (
	"strings"
	"testing"
)

// fieldsCertifiedPDF is a document certified at the given DocMDP policy whose
// page (3) lists the certification signature widget (4, value 5), a text field
// widget (6) whose keystroke action (9) formats a number, and an empty
// signature field widget (8) that locks every field when signed. The form (7)
// lists all three.
func fieldsCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fieldsPage("4 0 R 6 0 R 8 0 R").body,
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		textField("").body,
		fieldsForm("4 0 R 6 0 R 8 0 R", "").body,
		emptySignature("").body,
		"<< /S /JavaScript /JS (AFNumber_Keystroke\\(2, 0, 0, 0, \"\", true\\);) >>",
	}, "")
}

// fieldsPage is the page of [fieldsCertifiedPDF] listing annots.
func fieldsPage(annots string) updatedObject {
	return updatedObject{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [" + annots + "] >>"}
}

// textField is the text field widget of [fieldsCertifiedPDF] with extra
// entries.
func textField(extra string) updatedObject {
	return updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (amount) /Rect [20 20 120 40] /P 3 0 R /DA (/Helv 12 Tf 0 g) /AA << /K 9 0 R >>" + extra + " >>"}
}

// fieldsForm is the form of [fieldsCertifiedPDF] listing fields, with extra
// entries.
func fieldsForm(fields, extra string) updatedObject {
	return updatedObject{7, "<< /Fields [" + fields + "] /SigFlags 3 /DA (/Helv 0 Tf 0 g)" + extra + " >>"}
}

// emptySignature is the empty signature field widget of [fieldsCertifiedPDF]
// with extra entries.
func emptySignature(extra string) updatedObject {
	return updatedObject{8, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [300 300 400 350] /P 3 0 R /Lock << /Type /SigFieldLock /Action /All >>" + extra + " >>"}
}

// approvalValue is a signature value (ISO 32000-1, 12.8.1, Table 252).
const approvalValue = "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> >>"

// TestEvaluateMDP_FormFields covers ISO 32000-1, 12.8.2.2, Table 254 for the
// fields of the interactive form at P=2: filling in a field and signing are
// permitted, and any other change invalidates the signature. Filling in
// rewrites a field's value and the appearance of its widget, signing gives an
// empty signature field its value and locks the fields its /Lock names
// (12.7.4.5), and a signer may add a signature field. A new field of another
// type, an action a field gains or that an update rewrites, a renamed field or
// a replaced signature is none of these.
func TestEvaluateMDP_FormFields(t *testing.T) {
	script := "<< /S /JavaScript /JS (this.getField\\(\"amount\"\\).value = 900;) >>"
	newWidget := func(ft, extra string) []updatedObject {
		return []updatedObject{
			{40, "<< /Type /Annot /Subtype /Widget /FT /" + ft + " /T (added) /Rect [300 400 400 450] /P 3 0 R" + extra + " >>"},
			fieldsForm("4 0 R 6 0 R 8 0 R 40 0 R", ""),
			fieldsPage("4 0 R 6 0 R 8 0 R 40 0 R"),
		}
	}

	tests := []struct {
		name     string
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"text field filled", []updatedObject{
			textField(" /V (100.00) /AP << /N 41 0 R >>"),
			{41, formXObject("/Font << /Helv 42 0 R >>", "/Tx BMC BT /Helv 12 Tf (100.00) Tj ET EMC")},
			{42, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
		}, true, ""},
		{"empty signature field signed", []updatedObject{
			emptySignature(" /V 41 0 R /F 4 /MK << /BG [1] >> /BS << /W 1 >> /Border [0 0 1] /AP << /N 42 0 R >>"),
			{41, approvalValue},
			{42, formXObject("", "q 0 0 100 50 re S Q")},
			textField(" /Ff 1"),
		}, true, ""},
		{"signature field added", append(newWidget("Sig", " /V 41 0 R"), updatedObject{41, approvalValue}), true, ""},
		{"signature field with a widget of its own added", []updatedObject{
			{40, "<< /FT /Sig /T (added) /Kids [41 0 R] /V 42 0 R >>"},
			{41, "<< /Type /Annot /Subtype /Widget /Parent 40 0 R /Rect [300 400 400 450] /P 3 0 R >>"},
			{42, approvalValue},
			fieldsForm("4 0 R 6 0 R 8 0 R 40 0 R", ""),
			fieldsPage("4 0 R 6 0 R 8 0 R 41 0 R"),
		}, true, ""},
		{"need appearances set", []updatedObject{fieldsForm("4 0 R 6 0 R 8 0 R", " /NeedAppearances true")}, true, ""},

		{"text field added", newWidget("Tx", ""), false, "field 40 (Tx) was added, which P=2 permits only for a signature field"},
		{
			"text field added with a JavaScript /AA", append(newWidget("Tx", " /AA << /K 41 0 R >>"), updatedObject{41, script}),
			false, "field 40 (Tx) was added with an /AA action",
		},
		{
			"signature field added with a JavaScript /A", append(newWidget("Sig", " /A 41 0 R"), updatedObject{41, script}),
			false, "field 40 (Sig) was added with an /A action",
		},
		{"widget of its own added with a JavaScript /AA", []updatedObject{
			{40, "<< /FT /Sig /T (added) /Kids [41 0 R] >>"},
			{41, "<< /Type /Annot /Subtype /Widget /Parent 40 0 R /Rect [300 400 400 450] /P 3 0 R /AA << /E 43 0 R >> >>"},
			{43, script},
			fieldsForm("4 0 R 6 0 R 8 0 R 40 0 R", ""),
			fieldsPage("4 0 R 6 0 R 8 0 R 41 0 R"),
		}, false, "widget 41 (Sig) was added with an /AA action"},
		{"action added to an existing field", []updatedObject{
			emptySignature(" /AA << /U 41 0 R >>"), {41, script},
		}, false, "field 8 (Sig) added its /AA"},
		{
			"existing field's action rewritten",
			[]updatedObject{{9, script}},
			false, "object 9 (untyped) is reached through field 6 (Tx)'s /AA",
		},
		{"existing field renamed", []updatedObject{
			{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (total) /Rect [20 20 120 40] /P 3 0 R /DA (/Helv 12 Tf 0 g) /AA << /K 9 0 R >> >>"},
		}, false, "field 6 (Tx) changed its /T"},
		{"field flags beyond read-only", []updatedObject{textField(" /Ff 4096")}, false, "field 6 (Tx) added its /Ff"},
		{"certification signature replaced", []updatedObject{
			{4, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 41 0 R >>"},
			{41, approvalValue},
		}, false, "field 4 (Sig) changed its /V"},
		{
			"calculation order added",
			[]updatedObject{fieldsForm("4 0 R 6 0 R 8 0 R", " /CO [6 0 R]")},
			false, "the interactive form's /CO was added",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := fieldsCertifiedPDF(2)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), 2)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a comparison did not run", result.MDPInfo)
			}
		})
	}
}

// TestEvaluateMDP_FormFieldsP3 covers ISO 32000-1, 12.8.2.2, Table 254 for the
// fields of the interactive form at P=3, which adds annotation creation,
// deletion and modification to what P=2 permits. A widget may then move or
// change how it looks, and comments may come and go, but the form changes as
// at P=2 only: no new field other than a signature field, no action added or
// rewritten, no field renamed.
func TestEvaluateMDP_FormFieldsP3(t *testing.T) {
	script := "<< /S /JavaScript /JS (this.getField\\(\"amount\"\\).value = 900;) >>"

	tests := []struct {
		name     string
		update   []updatedObject
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"text field filled", []updatedObject{
			textField(" /V (100.00) /AP << /N 41 0 R >>"),
			{41, formXObject("/Font << /Helv 42 0 R >>", "/Tx BMC BT /Helv 12 Tf (100.00) Tj ET EMC")},
			{42, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"},
		}, true, ""},
		{"widget moved and recoloured", []updatedObject{
			{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (amount) /Rect [40 40 140 60] /P 3 0 R /DA (/Helv 12 Tf 0 g) /AA << /K 9 0 R >> /C [1 0 0] /Contents (Amount due) >>"},
		}, true, ""},
		{"comment added", []updatedObject{
			{40, "<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /P 3 0 R /Contents (note) >>"},
			fieldsPage("4 0 R 6 0 R 8 0 R 40 0 R"),
		}, true, ""},

		{"text field added", []updatedObject{
			{40, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (added) /Rect [300 400 400 450] /P 3 0 R >>"},
			fieldsForm("4 0 R 6 0 R 8 0 R 40 0 R", ""),
			fieldsPage("4 0 R 6 0 R 8 0 R 40 0 R"),
		}, false, "field 40 (Tx) was added, which P=3 permits only for a signature field"},
		{"action added to an existing field", []updatedObject{
			emptySignature(" /AA << /U 41 0 R >>"), {41, script},
		}, false, "field 8 (Sig) added its /AA, which P=3 does not permit"},
		{"widget action added", []updatedObject{
			textField(" /A 41 0 R"), {41, script},
		}, false, "field 6 (Tx) added its /A, which P=3 does not permit"},
		{
			"existing field's action rewritten",
			[]updatedObject{{9, script}},
			false, "object 9 (untyped) is reached through field 6 (Tx)'s /AA, which P=3 does not permit changing",
		},
		{"existing field renamed", []updatedObject{
			{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (total) /Rect [20 20 120 40] /P 3 0 R /DA (/Helv 12 Tf 0 g) /AA << /K 9 0 R >> >>"},
		}, false, "field 6 (Tx) changed its /T"},
		{"field flags beyond read-only", []updatedObject{textField(" /Ff 4096")}, false, "field 6 (Tx) added its /Ff"},
		{
			"calculation order added",
			[]updatedObject{fieldsForm("4 0 R 6 0 R 8 0 R", " /CO [6 0 R]")},
			false, "the interactive form's /CO was added, which P=3 does not permit",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := fieldsCertifiedPDF(3)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), 3)
			if result.MDPValid != tc.want {
				t.Fatalf("MDPValid = %v, want %v (MDPInfo %q)", result.MDPValid, tc.want, result.MDPInfo)
			}
			if tc.reported != "" && !strings.Contains(result.MDPInfo, tc.reported) {
				t.Errorf("MDPInfo %q does not name %s", result.MDPInfo, tc.reported)
			}
			if strings.Contains(result.MDPInfo, "not checked") {
				t.Errorf("MDPInfo %q: a comparison did not run", result.MDPInfo)
			}
		})
	}
}
