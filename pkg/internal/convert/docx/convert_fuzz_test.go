package docx

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// fuzzSeeds are document bodies exercising most of the converter: a
// numbered paragraph with a tab, a page break, a link and a field; a table
// with spans and merges; sections with columns and a page number start.
// Each is short: the fuzzing engine minimizes every input that reaches new
// code before it goes on, and minimizing a long one takes the run's time.
var fuzzSeeds = []string{
	`<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr><w:tabs><w:tab w:val="right" w:pos="4000"/></w:tabs></w:pPr>` +
		`<w:r><w:rPr><w:b/><w:sz w:val="30"/></w:rPr><w:t>a</w:t><w:tab/><w:br w:type="page"/></w:r><w:hyperlink w:anchor="x"><w:r><w:t>b</w:t></w:r></w:hyperlink>` +
		`<w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>PAGE</w:instrText></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>`,
	`<w:tbl><w:tblGrid><w:gridCol w:w="100"/><w:gridCol w:w="100"/></w:tblGrid><w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p/></w:tc></w:tr>` +
		`<w:tr><w:tc><w:tcPr><w:vMerge w:val="restart"/></w:tcPr><w:p/></w:tc><w:tc><w:p/></w:tc></w:tr><w:tr><w:tc><w:tcPr><w:vMerge/></w:tcPr><w:p/></w:tc></w:tr></w:tbl>`,
	`<w:p><w:pPr><w:sectPr><w:pgSz w:w="5000" w:h="5000"/><w:cols w:num="2"/></w:sectPr></w:pPr></w:p><w:sectPr><w:pgNumType w:start="3"/></w:sectPr>`,
}

// fuzzNumbering is the numbering part of the fuzz seed.
const fuzzNumbering = `<w:numbering ` + nsDecl + `><w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:numFmt w:val="upperRoman"/><w:lvlText w:val="%1.%2"/></w:lvl></w:abstractNum><w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num></w:numbering>`

func FuzzConvert(f *testing.F) {
	for _, seed := range fuzzSeeds {
		f.Add([]byte(seed))
	}
	f.Add([]byte(fuzzSeeds[0][:len(fuzzSeeds[0])/2]))
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

// FuzzRevisions converts arbitrary bodies under each tracked-changes view:
// it must neither panic nor loop.
func FuzzRevisions(f *testing.F) {
	seed := `<w:p><w:pPr><w:rPr><w:ins w:id="1" w:author="A"/></w:rPr></w:pPr><w:ins w:id="2" w:author="A"><w:r><w:t>a</w:t></w:r></w:ins>` +
		`<w:del w:id="3" w:author="B"><w:r><w:delText>b</w:delText></w:r></w:del><w:moveFrom w:id="4" w:author="A"><w:r><w:t>c</w:t></w:r></w:moveFrom></w:p>` +
		`<w:tbl><w:tr><w:trPr><w:del w:id="5" w:author="A"/></w:trPr><w:tc><w:p/></w:tc></w:tr></w:tbl><w:p/>`
	f.Add([]byte(seed), uint8(2))
	f.Add([]byte(seed[:len(seed)/2]), uint8(1))
	fonts := testFonts(f)
	f.Fuzz(func(t *testing.T, body []byte, view uint8) {
		files := pkgFiles("", nil)
		files["word/document.xml"] = `<w:document ` + nsDecl + `><w:body>` + string(body) + `</w:body></w:document>`
		data := zipFiles(t, files)
		opts := office.Options{MaxPages: 50, TrackedChanges: office.TrackedChanges(view % 3)}
		doc, err := Convert(context.Background(), bytes.NewReader(data), fonts, opts)
		if err == nil && len(doc.Pages) == 0 {
			t.Fatal("no page without an error")
		}
	})
}
