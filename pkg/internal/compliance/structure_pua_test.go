package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// puaPDF builds a tagged one-page document whose single P element carries the
// given key with a Private Use Area code point, and whose outline item title
// carries whatever title says.
//
// The strings are hexadecimal so the UTF-16BE byte order mark is explicit:
// FEFF is the mark and E000 is the first Private Use Area code point.
func puaPDF(elemKey, elemValue, title string) []byte {
	elem := "<< /Type /StructElem /S /P /NS 6 0 R /P 5 0 R /Pg 3 0 R /K 0"
	if elemKey != "" {
		elem += " /" + elemKey + " " + elemValue
	}
	elem += " >>"
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> "+
			"/Lang (en-US) /ViewerPreferences << /DisplayDocTitle true >> /Outlines 8 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /StructParents 0 /Tabs /S >>",
		"<< /Type /StructTreeRoot /Namespaces [6 0 R] /K 5 0 R /ParentTree 9 0 R >>",
		"<< /Type /StructElem /S /Document /NS 6 0 R /P 4 0 R /Lang (en-US) /K 7 0 R >>",
		"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		elem,
		"<< /Type /Outlines /First 10 0 R /Last 10 0 R /Count 1 >>",
		"<< /Nums [0 [7 0 R]] >>",
		"<< /Title "+title+" /Parent 8 0 R >>",
	)
}

const (
	puaHex   = "<FEFF0041E0000042>" // "A", U+E000, "B"
	plainHex = "<FEFF00410042>"     // "A", "B"
)

// firesRuleUA is the PDF/UA counterpart of [firesRuleProfile].
func firesRuleUA(t *testing.T, data []byte, profile models.PDFUAProfile, clause string, test int) bool {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			return true
		}
	}
	return false
}

// TestPUAScopePerProfile pins which keys each profile judges for Private Use
// Area content.
//
// ISO 14289-2 splits the requirement across three veraPDF objects, one per
// place a description can live. ISO 19005-4 §6.2.10.8 covers CosActualText
// ALONE: its profile defines no CosAlt rule, so an /Alt carrying a PUA code
// point is out of scope at PDF/A-4 and reporting it there was a false positive.
func TestPUAScopePerProfile(t *testing.T) {
	t.Parallel()

	actualText := puaPDF("ActualText", puaHex, plainHex)
	alt := puaPDF("Alt", puaHex, plainHex)

	// PDF/A-4 judges /ActualText and nothing else.
	if !firesRuleProfile(t, actualText, models.PDFA4, "6.2.10.8", 1) {
		t.Error("/ActualText with PUA must fire ISO 19005-4 §6.2.10.8 t1")
	}
	if firesRuleProfile(t, alt, models.PDFA4, "6.2.10.8", 1) {
		t.Error("/Alt with PUA must not fire ISO 19005-4 §6.2.10.8 t1: the clause is scoped to ActualText")
	}

	// PDF/UA-2 judges both, on separate tests.
	if !firesRuleUA(t, actualText, models.PDFUA2, "8.4.3", 2) {
		t.Error("/ActualText with PUA must fire ISO 14289-2 §8.4.3 t2")
	}
	if firesRuleUA(t, actualText, models.PDFUA2, "8.4.3", 3) {
		t.Error("/ActualText with PUA must not fire §8.4.3 t3, which judges /Alt")
	}
	if !firesRuleUA(t, alt, models.PDFUA2, "8.4.3", 3) {
		t.Error("/Alt with PUA must fire ISO 14289-2 §8.4.3 t3")
	}
	if firesRuleUA(t, alt, models.PDFUA2, "8.4.3", 2) {
		t.Error("/Alt with PUA must not fire §8.4.3 t2, which judges /ActualText")
	}
}

// TestOutlineTitlePUA pins ISO 14289-2 §8.6 t1 for the text strings a consumer
// reads out. An outline item's /Title is the position measured to reach
// veraPDF's CosTextString.
func TestOutlineTitlePUA(t *testing.T) {
	t.Parallel()

	if !firesRuleUA(t, puaPDF("", "", puaHex), models.PDFUA2, "8.6", 1) {
		t.Error("an outline title with PUA must fire ISO 14289-2 §8.6 t1")
	}
	if firesRuleUA(t, puaPDF("", "", plainHex), models.PDFUA2, "8.6", 1) {
		t.Error("an outline title without PUA must stay silent")
	}
	// The clause belongs to PDF/UA-2 alone; no other profile defines it.
	if firesRuleProfile(t, puaPDF("", "", puaHex), models.PDFA4, "8.6", 1) {
		t.Error("§8.6 t1 must not be reported outside PDF/UA-2")
	}
}

// textFieldPDF builds a PDF/UA-2 document whose AcroForm holds one text field
// carrying the given /V and /RV entries verbatim.
func textFieldPDF(entries string) []byte {
	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /Lang (en-US) "+
			"/ViewerPreferences << /DisplayDocTitle true >> /AcroForm << /Fields [5 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] /StructParents 0 >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 100 20] /F 4 /FT /Tx /T (f) "+entries+" >>",
	)
}

// TestTextFieldRichValue pins ISO 14289-2 §8.10.3.3 t1, which reads
// `containsRV == false || (V != null && RV == V)`.
//
// veraPDF reads /RV through the same rich text helper as a markup annotation's
// /RC: parsed as XML and reduced to its text nodes, then compared to the plain
// /V exactly. The rule was declared and registered but never fired.
func TestTextFieldRichValue(t *testing.T) {
	t.Parallel()

	const (
		body    = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>agreed text</p></body>"
		other   = "<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>something else</p></body>"
		clause  = "8.10.3.3"
		testNum = 1
	)

	for _, tc := range []struct {
		name    string
		entries string
		want    bool
	}{
		{name: "no /RV at all", entries: "/V (agreed text)", want: false},
		{name: "agreeing", entries: "/V (agreed text) /RV (" + body + ")", want: false},
		{name: "disagreeing", entries: "/V (the plain text) /RV (" + other + ")", want: true},
		{name: "/RV with no /V", entries: "/RV (" + body + ")", want: true},
		// Markup contributes nothing, so the tags around the text drop out.
		{name: "nested markup", entries: "/V (agreed text) /RV (" +
			"<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'><p>agreed <b>text</b></p></body>)", want: false},
		// Whitespace is significant on both sides.
		{name: "whitespace differs", entries: "/V (agreed text) /RV (" +
			"<?xml version='1.0'?><body xmlns='http://www.w3.org/1999/xhtml'> <p>agreed text</p></body>)", want: true},
		// An /RV that is not a parseable document yields no text to compare.
		{name: "not XML", entries: "/V (agreed text) /RV (agreed text)", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := firesRuleUA(t, textFieldPDF(tc.entries), models.PDFUA2, clause, testNum)
			if got != tc.want {
				t.Errorf("%s: fired=%v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
