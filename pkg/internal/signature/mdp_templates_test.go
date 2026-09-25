package signature

import (
	"strings"
	"testing"
)

// keystroke is the keystroke action of the template's amount field.
const keystroke = "<< /S /JavaScript /JS (AFNumber_Keystroke\\(2, 0, 0, 0, \"\", true\\);) >>"

// templateCertifiedPDF is a document certified at the given DocMDP policy
// whose page (3) lists the certification signature widget (4, value 5) and a
// text field widget (6). The name dictionary (12) names, in its /Templates
// tree (13) when templates is set, an invoice template page (8) that draws a
// content stream (11) and lists the amount field widget (9), whose keystroke
// action (10) formats a number. The form (7) holds the three fields.
func templateCertifiedPDF(policy int, templates bool) []byte {
	names := ""
	if templates {
		names = " /Names 12 0 R"
	}
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R" + names + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R 9 0 R] /SigFlags 3 /DA (/Helv 0 Tf 0 g) >>",
		"<< /Type /Page /MediaBox [0 0 612 792] /Contents 11 0 R /Annots [9 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (amount) /Rect [50 50 150 70] /P 8 0 R /DA (/Helv 12 Tf 0 g) /AA << /K 10 0 R >> >>",
		keystroke,
		streamObject("BT /Helv 12 Tf 50 80 Td (Amount) Tj ET"),
		"<< /Templates 13 0 R >>",
		"<< /Names [(invoice) 8 0 R] >>",
	}, "")
}

// spawned is the update that instantiates the invoice template of
// [templateCertifiedPDF] on a new page (20) and renames its field, as a
// script's spawn does: the new amount field widget (23) sits under the new
// nodes P1 (21) and P1.invoice (22). field is the widget's dictionary, and
// objs are more objects the update writes.
func spawned(field string, objs ...updatedObject) []updatedObject {
	return append([]updatedObject{
		{2, "<< /Type /Pages /Kids [3 0 R 20 0 R] /Count 2 >>"},
		{20, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 11 0 R /Annots [23 0 R] >>"},
		{21, "<< /T (P1) /Kids [22 0 R] >>"},
		{22, "<< /T (invoice) /Parent 21 0 R /Kids [23 0 R] >>"},
		{23, field},
		{7, "<< /Fields [4 0 R 6 0 R 9 0 R 21 0 R] /SigFlags 3 /DA (/Helv 0 Tf 0 g) >>"},
	}, objs...)
}

// spawnedAmount is the amount field widget a spawn copies to the new page, its
// keystroke action written as extra.
func spawnedAmount(extra string) string {
	return "<< /Type /Annot /Subtype /Widget /FT /Tx /T (amount) /Parent 22 0 R /Rect [50 50 150 70] /P 20 0 R /DA (/Helv 12 Tf 0 g)" + extra + " >>"
}

// TestEvaluateMDP_TemplateInstantiation covers ISO 32000-1, 12.8.2.2, Table
// 254, which permits instantiating page templates from P=2 up, and 12.7.6,
// where an instantiation copies a template page with its fields to a new page.
// The fields it adds copy the template's, actions included. A field placed on
// a signed page, one that copies no template widget, one whose actions differ
// from the template's, or one arriving with a template the signed document
// did not hold is none of these.
func TestEvaluateMDP_TemplateInstantiation(t *testing.T) {
	copied := spawnedAmount(" /AA << /K 24 0 R >>")
	script := "<< /S /JavaScript /JS (app.launchURL\\(\"https://example.com\"\\);) >>"

	tests := []struct {
		name      string
		policy    int
		templates bool
		update    []updatedObject
		want      bool
		reported  string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 template instantiated", 2, true, spawned(copied, updatedObject{24, keystroke}), true, ""},
		{"P=2 template instantiated sharing its action", 2, true, spawned(spawnedAmount(" /AA << /K 10 0 R >>")), true, ""},
		{"P=3 template instantiated", 3, true, spawned(copied, updatedObject{24, keystroke}), true, ""},

		{"P=2 copy runs another action", 2, true, spawned(copied, updatedObject{24, script}), false, "field 23 (Tx) was added with an /AA action"},
		{"P=2 copy drops its action", 2, true, spawned(spawnedAmount("")), false, "field 23 (Tx) was added, which P=2 permits only for a signature field"},
		{"P=2 copy renamed to no template field", 2, true, spawned(strings.Replace(copied, "(amount)", "(total)", 1), updatedObject{24, keystroke}),
			false, "field 21 was added, which P=2 permits only for a signature field"},
		{"P=2 copy moved", 2, true, spawned(strings.Replace(copied, "[50 50 150 70]", "[50 50 150 90]", 1), updatedObject{24, keystroke}),
			false, "field 21 was added, which P=2 permits only for a signature field"},
		{"P=2 grouping node carries an action", 2, true,
			replaced(spawned(copied, updatedObject{24, keystroke}), updatedObject{21, "<< /T (P1) /Kids [22 0 R] /AA << /F 25 0 R >> >>"}, updatedObject{25, script}),
			false, "field 21 was added with an /AA action"},
		{"P=2 copy placed on a signed page", 2, true, []updatedObject{
			{3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 23 0 R] >>"},
			{23, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (amount) /Rect [50 50 150 70] /P 3 0 R /DA (/Helv 12 Tf 0 g) /AA << /K 10 0 R >> >>"},
			{7, "<< /Fields [4 0 R 6 0 R 9 0 R 23 0 R] /SigFlags 3 /DA (/Helv 0 Tf 0 g) >>"},
		}, false, "field 23 (Tx) was added"},
		{"P=2 no template signed", 2, false, spawned(copied, updatedObject{24, keystroke}), false, "field 21 was added, which P=2 permits only for a signature field"},
		{"P=2 template added after signing", 2, false, spawned(copied,
			updatedObject{24, keystroke},
			updatedObject{1, "<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /Names 12 0 R >>"},
			updatedObject{12, "<< /Templates 13 0 R >>"},
			updatedObject{13, "<< /Names [(invoice) 8 0 R] >>"},
		), false, "was added"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := templateCertifiedPDF(tc.policy, tc.templates)
			result := mdpVerdict(t, appendUpdate(t, base, tc.update...), len(base), tc.policy)
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
