package pdf

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// longDOCX returns a DOCX of n paragraphs at 40 points, about 14 a page.
func longDOCX(t *testing.T, n int) []byte {
	t.Helper()
	w := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`
	var body strings.Builder
	for i := range n {
		fmt.Fprintf(&body, `<w:p><w:r><w:rPr><w:sz w:val="80"/></w:rPr><w:t>Line %d</w:t></w:r></w:p>`, i+1)
	}
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":       `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="word/document.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` + w + `><w:body>` + body.String() + `</w:body></w:document>`,
	})
}

// longXLSX returns an XLSX of one sheet of n rows of two cells.
func longXLSX(t *testing.T, n int) []byte {
	t.Helper()
	ns := `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"`
	var rows strings.Builder
	for i := range n {
		fmt.Fprintf(&rows, `<row r="%d"><c r="A%d" t="inlineStr"><is><t>Row</t></is></c><c r="B%d"><v>%d</v></c></row>`, i+1, i+1, i+1, i+1)
	}
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
			`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<workbook ` + ns + `><sheets><sheet name="Rows" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relR + `worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<worksheet ` + ns + `><sheetData>` + rows.String() + `</sheetData></worksheet>`,
	})
}

// longHTML returns an HTML document of n paragraphs at 40 points, in
// columns of the count given, none for 1.
func longHTML(n, columns int) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, `<!doctype html><title>Long</title><style>p { font-size: 40pt; margin: 0 }</style><div style="column-count: %d">`, columns)
	for i := range n {
		fmt.Fprintf(&b, "<p>Line %d</p>", i+1)
	}
	b.WriteString("</div>")
	return []byte(b.String())
}

// longPageTexts returns the text of every page of data.
func longPageTexts(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close() //nolint:errcheck // test reader
	n, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	texts := make([]string, n)
	for p := range n {
		if texts[p], err = r.ExtractText(ctx, p+1); err != nil {
			t.Fatalf("ExtractText(%d): %v", p+1, err)
		}
	}
	return texts
}

// TestConvert_LongDocumentsReachPDFA1 converts documents whose flow runs
// past 32767 points, the PDF/A-1 real limit (ISO 19005-1, 6.1.12), to
// PDF/A-1b: an HTML document, a three-column one, a DOCX and a workbook of
// many pages. Each conforms with nothing left, litho's validator finds
// nothing, and every page shows the text the plain conversion shows.
func TestConvert_LongDocumentsReachPDFA1(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	for _, tc := range []struct {
		name     string
		format   testFormat
		data     []byte
		minPages int
	}{
		// A page of an A4 flow is about 780 points: past page 42 the flow
		// passes 32767.
		{"html", formatHTML, longHTML(780, 1), 45},
		{"html in columns", formatHTML, longHTML(2100, 3), 45},
		{"docx", formatDOCX, longDOCX(t, 780), 45},
		{"xlsx", formatXLSX, longXLSX(t, 2600), 45},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			conversion := func(opts ...ConvertOption) *Conversion {
				return fromFormat(tc.format, bytes.NewReader(tc.data), append([]ConvertOption{WithConvertFontDirs(dir), WithDefaultLanguage("en")}, opts...)...)
			}
			var out bytes.Buffer
			if _, err := conversion().WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			plain := out.Bytes()
			a1b, rep, err := convertAndRemediate(ctx, conversion(WithConvertVersion("1.4")), "1.4", models.PDFA1b, "", "en")
			if err != nil {
				t.Fatalf("converting to PDF/A-1b: %v", err)
			}
			if len(rep.Residual) > 0 {
				t.Fatalf("Residual = %+v, want nothing left", rep.Residual)
			}
			r, err := Open(ctx, bytes.NewReader(a1b), int64(len(a1b)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close() //nolint:errcheck // test reader
			if violations, err := r.ValidatePDFA(ctx, models.PDFA1b); err != nil || len(violations) > 0 {
				t.Errorf("ValidatePDFA = %v, %v", violations, err)
			}
			want, got := longPageTexts(t, plain), longPageTexts(t, a1b)
			if len(want) < tc.minPages {
				t.Fatalf("%d pages, want at least %d: the flow no longer passes the limit", len(want), tc.minPages)
			}
			if len(got) != len(want) {
				t.Fatalf("%d pages at PDF/A-1b, %d plain", len(got), len(want))
			}
			for p := range want {
				if got[p] != want[p] {
					t.Errorf("page %d shows %q at PDF/A-1b, %q plain", p+1, got[p], want[p])
				}
			}
		})
	}
}
