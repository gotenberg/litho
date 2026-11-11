package docx

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
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// nsDecl declares the namespaces the test parts use.
const nsDecl = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" ` +
	`xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" ` +
	`xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" ` +
	`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" ` +
	`xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture" ` +
	`xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" ` +
	`xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" ` +
	`xmlns:m="http://schemas.openxmlformats.org/officeDocument/2006/math" ` +
	`xmlns:v="urn:schemas-microsoft-com:vml" xmlns:o="urn:schemas-microsoft-com:office:office" xmlns:w10="urn:schemas-microsoft-com:office:word"`

// relTypes maps the extra parts a test package may hold to their
// relationship types from the main part.
var relTypes = map[string]string{
	"word/styles.xml":        "http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles",
	"word/numbering.xml":     "http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering",
	"word/settings.xml":      "http://schemas.openxmlformats.org/officeDocument/2006/relationships/settings",
	"word/footnotes.xml":     "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes",
	"word/endnotes.xml":      "http://schemas.openxmlformats.org/officeDocument/2006/relationships/endnotes",
	"word/theme/theme1.xml":  "http://schemas.openxmlformats.org/officeDocument/2006/relationships/theme",
	"word/header1.xml":       "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header",
	"word/header2.xml":       "http://schemas.openxmlformats.org/officeDocument/2006/relationships/header",
	"word/footer1.xml":       "http://schemas.openxmlformats.org/officeDocument/2006/relationships/footer",
	"word/media/a.png":       "http://schemas.openxmlformats.org/officeDocument/2006/relationships/image",
	"word/charts/chart1.xml": "http://schemas.openxmlformats.org/officeDocument/2006/relationships/chart",
}

// pkgFiles returns the files of a DOCX package with body as the content of
// its w:body and the extra parts, related from the main part with the IDs
// rId<part's base name> (rIdheader1, rIdfooter1, rIda...).
func pkgFiles(body string, extra map[string]string) map[string]string {
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
			`<Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>`,
		"_rels/.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
			`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>` +
			`<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/></Relationships>`,
		"word/document.xml": `<w:document ` + nsDecl + `><w:body>` + body + `</w:body></w:document>`,
	}
	var rels strings.Builder
	rels.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	rels.WriteString(`<Relationship Id="rIdlink" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://example.com/" TargetMode="External"/>`)
	names := make([]string, 0, len(extra))
	for name := range extra {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		files[name] = extra[name]
		if typ, ok := relTypes[name]; ok {
			base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
			fmt.Fprintf(&rels, `<Relationship Id="rId%s" Type="%s" Target="%s"/>`, base, typ, strings.TrimPrefix(name, "word/"))
		}
	}
	rels.WriteString(`</Relationships>`)
	files["word/_rels/document.xml.rels"] = rels.String()
	return files
}

// zipFiles returns a zip archive of files.
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
		fw, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(files[n])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// docx returns a DOCX package of body and extra parts.
func docx(t testing.TB, body string, extra map[string]string) []byte {
	t.Helper()
	return zipFiles(t, pkgFiles(body, extra))
}

// testSans builds a "Test Sans" face covering printable ASCII, every glyph
// half an em wide.
func testSans() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 500, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: 500})
	}
	for _, r := range []rune{'•', '​', ' ', '‑'} {
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
	// sharedFontsDir is the directory testFonts made, "" when it made none.
	sharedFontsDir string
)

// TestMain removes the directory of the test face once the tests end: the
// index reads the face's file on first use, so no single test owns it.
func TestMain(m *testing.M) {
	code := m.Run()
	// Running the Once first orders this read after the write of any test.
	sharedFontsOnce.Do(func() {})
	if sharedFontsDir != "" {
		os.RemoveAll(sharedFontsDir)
	}
	os.Exit(code)
}

// testFonts returns an index of the test face, built once for the
// package.
func testFonts(t testing.TB) *text.Fonts {
	t.Helper()
	sharedFontsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "litho-docx-fonts")
		if err != nil {
			sharedFontsErr = err
			return
		}
		sharedFontsDir = dir
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
		t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
	}
	return fixtureFonts
}

// convertBytes converts a DOCX package with the test fonts.
func convertBytes(t testing.TB, data []byte, opts office.Options) *compose.Document {
	t.Helper()
	doc, err := Convert(context.Background(), bytes.NewReader(data), testFonts(t), opts)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return doc
}

// pageText returns the text of a page's fragments in paint order, runs
// joined by a space, lines by a newline when they start lower.
func pageText(pg *compose.Page) string {
	var b strings.Builder
	lastY := -1.0
	var walk func([]compose.Fragment)
	walk = func(frags []compose.Fragment) {
		for _, f := range frags {
			switch v := f.(type) {
			case *compose.Text:
				var s strings.Builder
				for _, g := range v.Glyphs {
					s.WriteString(g.Text)
				}
				if lastY >= 0 && v.Origin.Y > lastY+1 {
					b.WriteString("\n")
				} else if b.Len() > 0 {
					b.WriteString(" ")
				}
				b.WriteString(s.String())
				lastY = v.Origin.Y
			case *compose.Group:
				walk(v.Content)
			}
		}
	}
	walk(pg.Content)
	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.Join(lines, "\n")
}

// docText returns the text of every page, pages separated by a form feed.
func docText(doc *compose.Document) string {
	parts := make([]string, len(doc.Pages))
	for i, pg := range doc.Pages {
		parts[i] = pageText(pg)
	}
	return strings.Join(parts, "\f")
}

// built is a document built into its layout tree, not laid out.
type built struct {
	d    *document
	secs []*section
	root *layout.Node
}

// build loads a DOCX package of body and extra parts and builds its
// layout tree.
func build(t testing.TB, body string, extra map[string]string) *built {
	t.Helper()
	pkg, err := ooxml.Open(docx(t, body, extra))
	if err != nil {
		t.Fatalf("ooxml.Open: %v", err)
	}
	d, err := load(context.Background(), pkg, testFonts(t), office.Options{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	root, err := d.part(d.main)
	if err != nil {
		t.Fatal(err)
	}
	b := &built{d: d, secs: d.body(root)}
	b.root = &layout.Node{Kind: layout.Element, Style: d.blockStyle()}
	for _, s := range b.secs {
		b.root.Children = append(b.root.Children, d.sectionNode(s, nil))
	}
	return b
}

// find returns the nodes of the tree under n that match, in document
// order.
func find(n *layout.Node, match func(*layout.Node) bool) []*layout.Node {
	var out []*layout.Node
	var walk func(*layout.Node)
	walk = func(x *layout.Node) {
		if match(x) {
			out = append(out, x)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	return out
}

// role returns the nodes of role r.
func role(n *layout.Node, r string) []*layout.Node {
	return find(n, func(x *layout.Node) bool { return x.Role == r })
}

// textOf returns the text of the Text nodes under n.
func textOf(n *layout.Node) string {
	var b strings.Builder
	for _, x := range find(n, func(x *layout.Node) bool { return x.Kind == layout.Text }) {
		b.WriteString(x.Text)
	}
	return b.String()
}

// runWith returns the first inline run element under n whose text
// contains s.
func runWith(t testing.TB, n *layout.Node, s string) *layout.Node {
	t.Helper()
	for _, x := range find(n, func(x *layout.Node) bool {
		return x.Kind == layout.Element && len(x.Children) > 0 && x.Children[0].Kind == layout.Text
	}) {
		if strings.Contains(textOf(x), s) {
			return x
		}
	}
	t.Fatalf("no run with %q", s)
	return nil
}

// stylesPart returns a styles part with the given style definitions after
// document defaults of a 10-point Test Sans.
func stylesPart(defs string) string {
	return `<w:styles ` + nsDecl + `><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Test Sans" w:hAnsi="Test Sans"/><w:sz w:val="20"/></w:rPr></w:rPrDefault></w:docDefaults>` +
		`<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>` + defs + `</w:styles>`
}
