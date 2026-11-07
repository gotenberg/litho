package docx

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// fuzzSeed is a document body exercising most of the converter.
const fuzzSeed = `<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr><w:tabs><w:tab w:val="right" w:pos="4000"/></w:tabs></w:pPr>` +
	`<w:r><w:rPr><w:b/><w:sz w:val="30"/></w:rPr><w:t>a</w:t><w:tab/><w:br w:type="page"/></w:r><w:hyperlink w:anchor="x"><w:r><w:t>b</w:t></w:r></w:hyperlink>` +
	`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
	`<w:tbl><w:tblGrid><w:gridCol w:w="100"/><w:gridCol w:w="100"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p/></w:tc></w:tr>` +
	`<w:tr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr><w:tr><w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>` +
	`<w:p><w:pPr><w:sectPr><w:pgSz w:w="5000" w:h="5000"/><w:cols w:num="2"/></w:sectPr></w:pPr></w:p><w:sectPr><w:pgNumType w:start="3"/></w:sectPr>`

// fuzzNumbering is the numbering part of the fuzz seed.
const fuzzNumbering = `<w:numbering ` + nsDecl + `><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="upperRoman"/><w:lvlText w:val="%1.%2"/></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`

func FuzzConvert(f *testing.F) {
	f.Add([]byte(fuzzSeed))
	f.Add([]byte(fuzzSeed[:len(fuzzSeed)/2]))
	fonts := testFonts(f)
	f.Fuzz(func(t *testing.T, body []byte) {
		files := pkgFiles("", map[string]string{"word/numbering.xml": fuzzNumbering})
		files["word/document.xml"] = `<w:document ` + nsDecl + `><w:body>` + string(body) + `</w:body></w:document>`
		data := zipFiles(t, files)
		doc, err := Convert(context.Background(), bytes.NewReader(data), fonts, office.Options{MaxPages: 50})
		if err == nil && len(doc.Pages) == 0 {
			t.Fatal("no page without an error")
		}
	})
}
