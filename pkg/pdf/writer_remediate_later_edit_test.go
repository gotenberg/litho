package pdf

import (
	"bytes"
	"fmt"
	"slices"
)

// buildPDF writes objs as objects 1 to n of a PDF 1.7 file with the binary
// header comment and an exact cross-reference table, so a remediation repairs
// it in an incremental update. Object 1 is the catalog.
func buildPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// mixedColorPDF builds a page painting in DeviceRGB and DeviceCMYK through an
// indirect resource dictionary. A PDF/A-2b remediation adds an output intent
// for one of the two spaces and a Default colour space for the other into that
// dictionary, at its own object number (ISO 19005-2, 6.2.4.3).
func mixedColorPDF() []byte {
	const content = "1 0 0 rg 10 10 50 50 re f 0 0 0 1 k 100 100 50 50 re f\n" +
		"BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources 6 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Font << /F1 5 0 R >> >>",
	})
}

// placeholderFormPDF builds a page with one text field whose form asks the
// viewer to build appearances (ISO 32000-1, 12.7.3.3), whose stored appearance
// paints nothing, and whose widget carries a JavaScript action. A PDF/A-2b
// remediation clears the flag ISO 19005-2, 6.4.1 forbids, in the form
// dictionary at its own object number, and the action 6.4.1 and 6.5.1 forbid,
// in the widget at its own.
func placeholderFormPDF() []byte {
	const content = "BT /F1 12 Tf 20 700 Td (page) Tj ET\n"
	const ap = "/Tx BMC EMC\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm 6 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /Annots [7 0 R] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Fields [7 0 R] /NeedAppearances true /DA (/Helv 0 Tf 0 g) >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (value) /F 4 /P 3 0 R" +
			" /Rect [100 600 300 620] /AP << /N 8 0 R >> /A << /S /JavaScript /JS (app.alert\\(1\\)) >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 20] /Length %d >>\nstream\n%s\nendstream", len(ap), ap),
	})
}

// ruleHits returns the clause and test of each violation, sorted, so two
// outputs that number their new objects differently still compare.
func ruleHits(errs []error) []string {
	out := make([]string, 0, len(errs))
	for _, v := range toViolations(errs) {
		out = append(out, fmt.Sprintf("%s t%d", v.Clause, v.TestNumber))
	}
	slices.Sort(out)
	return out
}
