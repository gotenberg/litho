package pdf

import (
	"bytes"
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"testing/fstest"
)

// markedSVG is an SVG picture whose path has a marker, which litho leaves
// out with a warning.
const markedSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><marker id="k"/><path d="M0 0L40 20" stroke="black" marker-end="url(#k)"/></svg>`

// markerWarning is the svg renderer's message for markedSVG.
const markerWarning = "markers are not drawn"

// TestConvert_SVGPictureWarningsReachTheReport converts, for each family of
// formats, a document holding markedSVG as a picture, and checks that the
// renderer's warning reaches the report once, named after the picture,
// with its code and effect.
func TestConvert_SVGPictureWarningsReachTheReport(t *testing.T) {
	t.Parallel()

	fonts := WithConvertFontDirs(convertFontDir(t))
	embedded := base64.StdEncoding.EncodeToString([]byte(markedSVG))
	image := `<draw:image><office:binary-data>` + embedded + `</office:binary-data></draw:image>`
	frame := `<draw:frame svg:x="1cm" svg:y="1cm" svg:width="2cm" svg:height="1cm">` + image + `</draw:frame>`
	for _, tc := range []struct {
		family, want string
		conv         func() *Conversion
	}{
		{"HTML image", "in the SVG image a.svg: ", func() *Conversion {
			return FromHTML(strings.NewReader(`<img src="a.svg" alt=""><img src="a.svg" alt="">`), fonts, WithResources(fstest.MapFS{"a.svg": {Data: []byte(markedSVG)}}))
		}},
		{"HTML inline", "in an inline SVG: ", func() *Conversion {
			return FromHTML(strings.NewReader(`<p>`+markedSVG+markedSVG), fonts)
		}},
		{"word processing, DOCX", "in the SVG picture word/media/a.svg: ", func() *Conversion {
			return FromDOCX(bytes.NewReader(svgPictureDOCX(t)), fonts)
		}},
		{"word processing, ODT", "in an embedded SVG picture: ", func() *Conversion {
			return FromODT(strings.NewReader(wordODT("", `<text:p><draw:frame text:anchor-type="as-char" svg:width="2cm" svg:height="1cm">`+image+`</draw:frame></text:p>`)), fonts)
		}},
		{"spreadsheet, ODS", "in an embedded SVG picture: ", func() *Conversion {
			return FromODS(strings.NewReader(flatODF("spreadsheet", "", "", `<office:spreadsheet><table:table table:name="S"><table:shapes>`+frame+`</table:shapes>`+
				`<table:table-row><table:table-cell office:value-type="float" office:value="1"><text:p>1</text:p></table:table-cell></table:table-row></table:table></office:spreadsheet>`)), fonts)
		}},
		{"presentation, ODP", "in an embedded SVG picture: ", func() *Conversion {
			return FromODP(strings.NewReader(flatODF("presentation", pageLayout("PM1", "28cm", "21cm", ""),
				`<style:master-page style:name="Default" style:page-layout-name="PM1"/>`,
				`<office:presentation><draw:page draw:name="S1" draw:master-page-name="Default">`+frame+`</draw:page></office:presentation>`)), fonts)
		}},
	} {
		rep, err := tc.conv().WriteTo(context.Background(), &bytes.Buffer{})
		if err != nil {
			t.Errorf("%s: %v", tc.family, err)
			continue
		}
		n := 0
		for _, w := range rep.Warnings {
			if w.Message != tc.want+markerWarning {
				continue
			}
			n++
			if w.Code != WarningContentUnsupported || w.Effect != EffectOmitted {
				t.Errorf("%s: the warning is %s, %s", tc.family, w.Code, w.Effect)
			}
		}
		if n != 1 {
			t.Errorf("%s: %d warnings %q, want 1, in %+v", tc.family, n, tc.want+markerWarning, rep.Warnings)
		}
	}
}

// svgPictureDOCX returns a DOCX of one paragraph holding markedSVG as an
// inline picture, the part word/media/a.svg.
func svgPictureDOCX(t *testing.T) []byte {
	t.Helper()
	ns := `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"` +
		` xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"` +
		` xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"`
	return zipParts(t, map[string]string{
		"[Content_Types].xml": `<Types ` + ctNS + `>` + ctXML + `<Default Extension="svg" ContentType="image/svg+xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels":                  `<Relationships ` + relsNS + `><Relationship Id="rId1" Type="` + relDoc + `" Target="word/document.xml"/></Relationships>`,
		"word/_rels/document.xml.rels": `<Relationships ` + relsNS + `><Relationship Id="rIda" Type="` + relR + `image" Target="media/a.svg"/></Relationships>`,
		"word/document.xml": `<w:document ` + ns + `><w:body><w:p><w:r><w:drawing><wp:inline><wp:extent cx="1270000" cy="635000"/><wp:docPr id="1" name="p"/>` +
			`<a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic>` +
			`<pic:nvPicPr><pic:cNvPr id="1" name="a.svg"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="rIda"/></pic:blipFill>` +
			`<pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="1270000" cy="635000"/></a:xfrm></pic:spPr></pic:pic></a:graphicData></a:graphic>` +
			`</wp:inline></w:drawing></w:r></w:p></w:body></w:document>`,
		"word/media/a.svg": markedSVG,
	})
}
