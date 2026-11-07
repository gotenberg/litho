package xlsx

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// nsDecl declares the namespaces the test parts use.
const nsDecl = `xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"`

// testSheet is one sheet of a test workbook: its name, its state and the
// content of its worksheet element; rels, when set, is the worksheet
// part's relationships content.
type testSheet struct {
	name, state, xml, rels string
}

// minimalStyles is a stylesheet of one font, the two fills Excel always
// writes, one border and one cell format.
const minimalStyles = `<fonts><font><sz val="11"/><name val="Test Sans"/></font></fonts>` +
	`<fills><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills>` +
	`<borders><border><left/><right/><top/><bottom/><diagonal/></border></borders>` +
	`<cellStyleXfs><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` +
	`<cellXfs><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/></cellXfs>` +
	`<cellStyles><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>`

// workbookFiles returns the parts of a workbook of sheets, with the
// styles content styles ("" for none), the workbook element's content
// before and after its sheets, and the extra parts: xl/sharedStrings.xml
// and xl/theme/theme1.xml are related from the workbook, any other part
// is only stored.
func workbookFiles(sheets []testSheet, styles, before, after string, extra map[string]string) map[string]string {
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/></Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` +
			`<Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>`,
	}
	var wb, rels strings.Builder
	wb.WriteString(`<workbook ` + nsDecl + `>` + before + `<sheets>`)
	rels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	for i, s := range sheets {
		state := s.state
		if state == "" {
			state = "visible"
		}
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" state="%s" r:id="rIdS%d"/>`, s.name, i+1, state, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rIdS%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)
		files[fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1)] = `<worksheet ` + nsDecl + `>` + s.xml + `</worksheet>`
		if s.rels != "" {
			files[fmt.Sprintf("xl/worksheets/_rels/sheet%d.xml.rels", i+1)] = `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + s.rels + `</Relationships>`
		}
	}
	wb.WriteString(`</sheets>` + after + `</workbook>`)
	if styles != "" {
		files["xl/styles.xml"] = `<styleSheet ` + nsDecl + `>` + styles + `</styleSheet>`
		rels.WriteString(`<Relationship Id="rIdStyles" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`)
	}
	related := map[string]string{
		"xl/sharedStrings.xml": "http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings",
		"xl/theme/theme1.xml":  "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme",
	}
	names := make([]string, 0, len(extra))
	for n := range extra {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		files[n] = extra[n]
		if typ, ok := related[n]; ok {
			fmt.Fprintf(&rels, `<Relationship Id="rId%s" Type="%s" Target="%s"/>`, filepath.Base(n), typ, strings.TrimPrefix(n, "xl/"))
		}
	}
	rels.WriteString(`</Relationships>`)
	files["xl/workbook.xml"] = wb.String()
	files["xl/_rels/workbook.xml.rels"] = rels.String()
	return files
}

// zipFiles returns a zip archive of files, in name order, stored without
// compression so the fuzz targets spend their time in the reader.
func zipFiles(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Store})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// oneSheet returns a workbook of one sheet "S" of content xml, with the
// minimal styles.
func oneSheet(t testing.TB, xml string) []byte {
	t.Helper()
	return zipFiles(t, workbookFiles([]testSheet{{name: "S", xml: xml}}, minimalStyles, "", "", nil))
}

// readBytes reads a workbook into the model with the test fonts.
func readBytes(t testing.TB, data []byte) (*sheet.Document, []compose.Warning) {
	t.Helper()
	doc, warnings, err := load(context.Background(), bytes.NewReader(data), testFonts(t), office.Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return doc, warnings
}

// cellAt returns the cell at row and col of sh, false when absent.
func cellAt(sh *sheet.Sheet, row, col int) (sheet.Cell, bool) {
	for _, r := range sh.Rows {
		if r.Index != row {
			continue
		}
		for _, c := range r.Cells {
			if c.Column == col {
				return c, true
			}
		}
	}
	return sheet.Cell{}, false
}

// testSans builds a "Test Sans" face covering printable ASCII, every glyph
// half an em wide.
func testSans() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 500})
	}
	return fontbuild.TrueType{
		PostScriptName: "TestSans-Regular",
		Family:         "Test Sans",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs:         glyphs,
	}.Build()
}

var (
	sharedFontsOnce sync.Once
	sharedFonts     *text.Fonts
	sharedFontsErr  error
)

// testFonts returns an index of the test face, built once for the
// package.
func testFonts(t testing.TB) *text.Fonts {
	t.Helper()
	sharedFontsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "litho-xlsx-fonts")
		if err != nil {
			sharedFontsErr = err
			return
		}
		if err := os.WriteFile(filepath.Join(dir, "test_sans.ttf"), testSans(), 0o600); err != nil {
			sharedFontsErr = err
			return
		}
		sharedFonts, sharedFontsErr = text.NewFonts(context.Background(), nil, []string{dir})
	})
	if sharedFontsErr != nil {
		t.Fatalf("indexing the test font: %v", sharedFontsErr)
	}
	return sharedFonts
}

var (
	fixtureFontsOnce sync.Once
	fixtureFonts     *text.Fonts
)

// pinnedFonts returns an index of the pinned fixture fonts, skipping the
// test when they are not fetched.
func pinnedFonts(t testing.TB) *text.Fonts {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts")
	fixtureFontsOnce.Do(func() {
		if _, err := os.Stat(dir); err != nil {
			return
		}
		fixtureFonts, _ = text.NewFonts(context.Background(), nil, []string{dir})
	})
	if fixtureFonts == nil || fixtureFonts.Len() == 0 {
		t.Skip("fixture fonts not fetched (make html-fixtures)")
	}
	return fixtureFonts
}
