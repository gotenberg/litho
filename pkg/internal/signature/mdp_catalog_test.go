package signature

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// certifiedCatalog is the catalog (1) of [contentCertifiedPDF] with extra
// entries.
func certifiedCatalog(extra string) updatedObject {
	return updatedObject{1, "<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R" + extra + " >>"}
}

// javaScript is a JavaScript action (ISO 32000-1, 12.6.4.16).
const javaScript = "<< /S /JavaScript /JS (app.alert\\(1\\)) >>"

// retrailered returns data with the entries of its last trailer that begin at
// "/Root 1 0 R" replaced by entries.
func retrailered(t *testing.T, data []byte, entries string) []byte {
	t.Helper()
	at := bytes.LastIndex(data, []byte("/Root 1 0 R"))
	if at < 0 {
		t.Fatal("data has no trailer /Root")
	}
	return append(append(append([]byte(nil), data[:at]...), entries...), data[at+len("/Root 1 0 R"):]...)
}

// markedCertifiedPDF is a document certified at the given DocMDP policy whose
// page (3) lists the certification signature widget (4, value 5), a text field
// widget (6) and a stamp (8) drawn by an appearance stream without /Type (9),
// and whose form (7) holds an XFA template (10) and datasets (11) packet.
func markedCertifiedPDF(policy int) []byte {
	return buildSigPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R 6 0 R 8 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
		certification(policy),
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R >>",
		"<< /Fields [4 0 R 6 0 R] /SigFlags 3 /XFA [(template) 10 0 R (datasets) 11 0 R] >>",
		"<< /Type /Annot /Subtype /Stamp /Rect [100 100 300 200] /P 3 0 R /AP << /N 9 0 R >> >>",
		untypedForm("BT /F1 24 Tf 10 50 Td (APPROVED) Tj ET"),
		streamObject("<template/>"),
		streamObject("<data/>"),
	}, "")
}

// untypedForm is a form XObject without the /Type ISO 32000-1, 8.10.2, Table 95
// makes optional, drawing content.
func untypedForm(content string) string {
	return fmt.Sprintf("<< /Subtype /Form /BBox [0 0 200 100] /Length %d >>\nstream\n%s\nendstream", len(content), content)
}

// TestEvaluateMDP_CatalogChanges covers ISO 32000-1, 12.8.2.2, Table 254 for a
// rewritten catalog and the objects without /Type it reaches. P=2 permits
// filling in forms, instantiating page templates and signing, P=3 adds
// annotation changes, and any other change invalidates the signature: an
// action run on opening, document JavaScript, an embedded file, a toggled
// optional content group. What signing and validation data need passes: the
// form, a usage rights signature in /Perms, the DSS with its extension
// (ISO 32000-2, Table 257), a raised /Version, a template shown through
// /Names.
func TestEvaluateMDP_CatalogChanges(t *testing.T) {
	content := func(catalogExtra string, extra ...string) func(policy int) []byte {
		return func(policy int) []byte { return contentCertifiedPDF(policy, catalogExtra, extra...) }
	}
	plain := content("")
	appended := func(objs ...updatedObject) func(t *testing.T, base []byte) []byte {
		return func(t *testing.T, base []byte) []byte { return appendUpdate(t, base, objs...) }
	}
	ocg := "<< /Type /OCG /Name (Watermark) >>"

	tests := []struct {
		name     string
		policy   int
		base     func(policy int) []byte
		update   func(t *testing.T, base []byte) []byte
		want     bool
		reported string // what MDPInfo names when the verdict is a violation
	}{
		{"P=2 /OpenAction JavaScript added", 2, plain, appended(
			certifiedCatalog(" /OpenAction 40 0 R"), updatedObject{40, javaScript},
		), false, "the catalog's /OpenAction was added"},
		{"P=3 /OpenAction JavaScript added", 3, plain, appended(
			certifiedCatalog(" /OpenAction 40 0 R"), updatedObject{40, javaScript},
		), false, "the catalog's /OpenAction was added"},
		{"P=2 dangling /OpenAction filled in with JavaScript", 2, content(" /OpenAction 40 0 R"), appended(
			updatedObject{40, javaScript},
		), false, "object 40 (untyped) is reached through the catalog's /OpenAction"},
		{"P=3 dangling /OpenAction filled in with JavaScript", 3, content(" /OpenAction 40 0 R"), appended(
			updatedObject{40, javaScript},
		), false, "object 40 (untyped) is reached through the catalog's /OpenAction"},
		{"P=2 document JavaScript added to /Names", 2, plain, appended(
			certifiedCatalog(" /Names << /JavaScript << /Names [(run) 40 0 R] >> >>"), updatedObject{40, javaScript},
		), false, "the catalog's /Names /JavaScript was added"},
		{"P=2 document /AA added", 2, plain, appended(
			certifiedCatalog(" /AA << /WC 40 0 R >>"), updatedObject{40, javaScript},
		), false, "the catalog's /AA was added"},
		{"P=3 embedded file added to the /Names dictionary", 3, content(" /Names 12 0 R", "<< >>"), appended(
			updatedObject{12, "<< /EmbeddedFiles << /Names [(invoice.xml) 40 0 R] >> >>"},
			updatedObject{40, "<< /F (invoice.xml) /EF << /F 41 0 R >> >>"},
			updatedObject{41, streamObject("<total>9000</total>")},
		), false, "the catalog's /Names /EmbeddedFiles was added"},
		{"P=3 embedded file name tree redefined", 3, content(" /Names << /EmbeddedFiles 12 0 R >>", "<< /Names [] >>"), appended(
			updatedObject{12, "<< /Names [(invoice.xml) 40 0 R] >>"},
			updatedObject{40, "<< /F (invoice.xml) /EF << /F 41 0 R >> >>"},
			updatedObject{41, streamObject("<total>9000</total>")},
		), false, "object 12 (untyped) is reached through the catalog's /Names /EmbeddedFiles"},
		{"P=2 optional content toggled", 2, content(" /OCProperties << /OCGs [12 0 R] /D << /ON [12 0 R] >> >>", ocg), appended(
			certifiedCatalog(" /OCProperties << /OCGs [12 0 R] /D << /OFF [12 0 R] >> >>"),
		), false, "the catalog's /OCProperties changed"},
		{"P=3 optional content toggled in its dictionary", 3, content(" /OCProperties 12 0 R", "<< /OCGs [13 0 R] /D << /ON [13 0 R] >> >>", ocg), appended(
			updatedObject{12, "<< /OCGs [13 0 R] /D << /OFF [13 0 R] >> >>"},
		), false, "object 12 (untyped) is reached through the catalog's /OCProperties"},
		{"P=2 XMP metadata replaced", 2, content(" /Metadata 12 0 R", streamObject("<x:xmpmeta/>")), appended(
			certifiedCatalog(" /Metadata 40 0 R"), updatedObject{40, streamObject("<x:xmpmeta>changed</x:xmpmeta>")},
		), false, "the catalog's /Metadata changed"},
		{"P=2 /Perms /DocMDP replaced", 2, content(" /Perms << /DocMDP 5 0 R >>"), appended(
			certifiedCatalog(" /Perms << /DocMDP 40 0 R >>"), updatedObject{40, certification(3)},
		), false, "the catalog's /Perms /DocMDP changed"},
		{"P=2 /AcroForm removed", 2, plain, appended(
			updatedObject{1, "<< /Type /Catalog /Pages 2 0 R >>"},
		), false, "the catalog's /AcroForm was removed"},
		{"P=2 DSS beside an /OpenAction", 2, plain, appended(dssUpdate("/OpenAction [3 0 R /Fit] ")...), false, "the catalog's /OpenAction was added"},
		{"P=2 /Root pointed at another catalog", 2, plain, func(t *testing.T, base []byte) []byte {
			return retrailered(t, appendUpdate(t, base,
				updatedObject{40, "<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R /OpenAction 41 0 R >>"},
				updatedObject{41, javaScript},
			), "/Root 40 0 R")
		}, false, "the catalog's /OpenAction was added"},
		{"P=2 stamp appearance redefined", 2, markedCertifiedPDF, appended(
			updatedObject{9, untypedForm("BT /F1 24 Tf 10 50 Td (VOID) Tj ET")},
		), false, "object 9 (untyped) is reached through annotation 8 (Annot:Stamp)"},
		{"P=2 XFA template packet redefined", 2, markedCertifiedPDF, appended(
			updatedObject{10, streamObject("<template>other</template>")},
		), false, `object 10 (untyped) is reached through the XFA "template" packet`},
		{"P=2 information dictionary rewritten alone", 2, func(policy int) []byte {
			return buildSigPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R /AcroForm 7 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
				"<< /Type /Annot /Subtype /Widget /FT /Sig /T (cert) /Rect [0 0 0 0] /P 3 0 R /V 5 0 R >>",
				certification(policy),
				"<< /ModDate (D:20260101000000Z) >>",
				"<< /Fields [4 0 R] /SigFlags 3 >>",
			}, "/Info 6 0 R ")
		}, func(t *testing.T, base []byte) []byte {
			return retrailered(t, appendUpdate(t, base,
				updatedObject{6, "<< /ModDate (D:20260201000000Z) /Producer (iText) >>"},
			), "/Root 1 0 R /Info 6 0 R")
		}, false, "object 6 (untyped) is reached through the trailer's /Info"},

		{"P=2 form fill", 2, plain, appended(
			updatedObject{6, "<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /Rect [20 20 120 40] /P 3 0 R /DA (/F1 12 Tf 0 g) /V (Jane) /AP << /N 40 0 R >> >>"},
			updatedObject{40, formXObject("/Font << /F1 11 0 R >>", "/Tx BMC BT /F1 12 Tf (Jane) Tj ET EMC")},
		), true, ""},
		{"P=2 visible approval signature through a rewritten catalog", 2, plain, appended(
			certifiedCatalog(""),
			updatedObject{7, "<< /Fields [4 0 R 6 0 R 40 0 R] /SigFlags 3 >>"},
			signedPage("4 0 R 6 0 R 40 0 R", ""),
			updatedObject{40, "<< /Type /Annot /Subtype /Widget /FT /Sig /T (approval) /Rect [300 300 400 350] /P 3 0 R /V 41 0 R /AP << /N 42 0 R >> >>"},
			updatedObject{41, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> >>"},
			updatedObject{42, formXObject("/Font << /F1 11 0 R >>", "BT /F1 8 Tf (Approved) Tj ET")},
		), true, ""},
		{"P=2 usage rights signature added to /Perms", 2, content(" /Perms << /DocMDP 5 0 R >>"), appended(
			certifiedCatalog(" /Perms << /DocMDP 5 0 R /UR3 40 0 R >>"),
			updatedObject{40, "<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /ByteRange [0 1 1 1] /Contents <30> /Reference [<< /Type /SigRef /TransformMethod /UR3 /TransformParams << /Type /TransformParams /V /2.2 >> >>] >>"},
		), true, ""},
		{"P=2 DSS and its extension", 2, plain, appended(dssUpdate("")...), true, ""},
		{"P=2 signature extension and version declared", 2, plain, appended(
			certifiedCatalog(" /Version /2.0 /Extensions << /ISO_ [<< /BaseVersion /2.0 /ExtensionLevel 32002 >>] >>"),
		), true, ""},
		{"P=2 template shown as a page", 2, content(" /Names << /Templates << /Names [(extra) 12 0 R] >> >>", "<< /Type /Page /MediaBox [0 0 612 792] /Contents 13 0 R >>", streamObject("BT ET")), appended(
			certifiedCatalog(" /Names << /Pages << /Names [(extra) 12 0 R] >> >>"),
			updatedObject{2, "<< /Type /Pages /Kids [3 0 R 12 0 R] /Count 2 >>"},
			updatedObject{12, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 13 0 R >>"},
		), true, ""},
		{"P=2 catalog and /Names written again unchanged", 2, content(" /Names 12 0 R", "<< /JavaScript 13 0 R >>", "<< /Names [] >>"), appended(
			certifiedCatalog(" /Names 12 0 R"),
			updatedObject{12, "<< /JavaScript 13 0 R >>"},
			updatedObject{13, "<< /Names [] >>"},
		), true, ""},
		{"P=2 XFA datasets packet redefined", 2, markedCertifiedPDF, appended(
			updatedObject{11, streamObject("<data>Jane</data>")},
		), true, ""},
		{"P=3 stamp appearance redefined", 3, markedCertifiedPDF, appended(
			updatedObject{9, untypedForm("BT /F1 24 Tf 10 50 Td (VOID) Tj ET")},
		), true, ""},
		{"P=3 comment added with its appearance", 3, plain, appended(
			signedPage("4 0 R 6 0 R 40 0 R", ""),
			updatedObject{40, "<< /Type /Annot /Subtype /FreeText /Rect [300 300 400 350] /P 3 0 R /DA (/Helv 12 Tf 0 g) /AP << /N 41 0 R >> >>"},
			updatedObject{41, untypedForm("BT /F1 12 Tf (note) Tj ET")},
		), true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := tc.base(tc.policy)
			result := mdpVerdict(t, tc.update(t, base), len(base), tc.policy)
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
