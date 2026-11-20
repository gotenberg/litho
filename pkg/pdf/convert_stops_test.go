package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// chartDOCX returns a DOCX of one paragraph and one inline chart whose
// part is chart.
func chartDOCX(t *testing.T, chart string) []byte {
	t.Helper()
	ns := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>` +
			`<Override PartName="/word/charts/chart1.xml" ContentType="application/vnd.openxmlformats-officedocument.drawingml.chart+xml"/></Types>`,
		"_rels/.rels":                  `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rId9" Type="` + relR + `chart" Target="charts/chart1.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` + ns + `><w:body><w:p><w:r><w:t>Before the chart</w:t></w:r></w:p>` +
			`<w:p><w:r><w:drawing><wp:inline><wp:extent cx="3810000" cy="2540000"/><wp:docPr id="1" name="c"/>` +
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/chart">` +
			`<c:chart xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart" r:id="rId9"/></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p>` +
			`</w:body></w:document>`,
		"word/charts/chart1.xml": chart,
	})
}

// TestConvert_ACapPassedInAPartStopsTheConversion locks the line between
// an error that stops a conversion and one that drops a construct: a chart
// part nested past the XML depth cap ends the conversion with
// [ErrConvertInputTooLarge], where a chart part that does not parse is
// left out with a warning.
func TestConvert_ACapPassedInAPartStopsTheConversion(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	cs := `xmlns:c="http://schemas.openxmlformats.org/drawingml/2006/chart"`
	deep := `<c:chartSpace ` + cs + `>` + strings.Repeat("<c:x>", 300) + strings.Repeat("</c:x>", 300) + `</c:chartSpace>`
	_, err := FromDOCX(bytes.NewReader(chartDOCX(t, deep)), WithConvertFontDirs(dir)).WriteTo(context.Background(), io.Discard)
	if !errors.Is(err, ErrConvertInputTooLarge) || !IsInvalidInput(err) {
		t.Fatalf("a chart part past the depth cap: %v, want ErrConvertInputTooLarge", err)
	}

	broken := `<c:chartSpace ` + cs + `><c:chart>`
	rep, err := FromDOCX(bytes.NewReader(chartDOCX(t, broken)), WithConvertFontDirs(dir)).WriteTo(context.Background(), io.Discard)
	if err != nil {
		t.Fatalf("a chart part that does not parse: %v, want a PDF", err)
	}
	found := false
	for _, w := range rep.Warnings {
		found = found || w.Code == models.WarningPartMalformed
	}
	if !found {
		t.Errorf("warnings %+v lack chart-malformed", rep.Warnings)
	}
}
