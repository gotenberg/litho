package odt

import (
	"archive/zip"
	"bytes"
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// nsDecl declares the namespaces the test documents use.
const nsDecl = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
	`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
	`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" ` +
	`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
	`xmlns:xlink="http://www.w3.org/1999/xlink" ` +
	`xmlns:dc="http://purl.org/dc/elements/1.1/" ` +
	`xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0" ` +
	`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" ` +
	`xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0" ` +
	`office:version="1.3"`

// testPageLayout is an A4 page with 2 cm margins and Liberation Serif
// 12pt text, the defaults of a new Writer document.
const testPageLayout = `<office:font-face-decls>` +
	`<style:font-face style:name="Liberation Serif" svg:font-family="'Liberation Serif'" style:font-family-generic="roman"/>` +
	`<style:font-face style:name="Liberation Sans" svg:font-family="'Liberation Sans'" style:font-family-generic="swiss"/>` +
	`</office:font-face-decls>`

// doc is a test document: the XML of its pieces, each without its
// wrapper element.
type doc struct {
	styles, autoStyles, masters, contentAuto, body, meta string
	pictures                                             map[string][]byte
}

// stylesXML returns the styles.xml of d.
func (d doc) stylesXML() string {
	defaults := `<style:default-style style:family="paragraph"><style:paragraph-properties style:tab-stop-distance="1.25cm"/>` +
		`<style:text-properties style:font-name="Liberation Serif" fo:font-size="12pt" fo:language="en" fo:country="US"/></style:default-style>` +
		`<style:style style:name="Standard" style:family="paragraph" style:class="text"/>`
	masters := d.masters
	auto := d.autoStyles
	if masters == "" {
		auto += `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="21cm" fo:page-height="29.7cm" fo:margin-top="2cm" fo:margin-bottom="2cm" fo:margin-left="2cm" fo:margin-right="2cm"/></style:page-layout>`
		masters = `<style:master-page style:name="Standard" style:page-layout-name="pm1"/>`
	}
	return `<?xml version="1.0" encoding="UTF-8"?><office:document-styles ` + nsDecl + `>` + testPageLayout +
		`<office:styles>` + defaults + d.styles + `</office:styles>` +
		`<office:automatic-styles>` + auto + `</office:automatic-styles>` +
		`<office:master-styles>` + masters + `</office:master-styles></office:document-styles>`
}

// contentXML returns the content.xml of d.
func (d doc) contentXML() string {
	return `<?xml version="1.0" encoding="UTF-8"?><office:document-content ` + nsDecl + `>` + testPageLayout +
		`<office:automatic-styles>` + d.contentAuto + `</office:automatic-styles>` +
		`<office:body><office:text>` + d.body + `</office:text></office:body></office:document-content>`
}

// pack returns d as an ODT package.
func (d doc) pack(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	add := func(name, body string, store bool) {
		method := zip.Deflate
		if store {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("mimetype", "application/vnd.oasis.opendocument.text", true)
	add("META-INF/manifest.xml", `<?xml version="1.0" encoding="UTF-8"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">`+
		`<manifest:file-entry manifest:full-path="/" manifest:media-type="application/vnd.oasis.opendocument.text"/>`+
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>`+
		`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/></manifest:manifest>`, false)
	add("content.xml", d.contentXML(), false)
	add("styles.xml", d.stylesXML(), false)
	meta := d.meta
	add("meta.xml", `<?xml version="1.0" encoding="UTF-8"?><office:document-meta `+nsDecl+`><office:meta>`+meta+`</office:meta></office:document-meta>`, false)
	names := make([]string, 0, len(d.pictures))
	for n := range d.pictures {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		add(n, string(d.pictures[n]), false)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

var (
	fontsOnce sync.Once
	fonts     *text.Fonts
	fontsErr  error
)

// fixtureFonts returns the index of the pinned fixture fonts LibreOffice
// prints with (Liberation, DejaVu, Carlito, Caladea, Noto), skipping the
// test when they are not fetched.
func fixtureFonts(t testing.TB) *text.Fonts {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts")
	if _, err := os.Stat(filepath.Join(dir, "liberation-2.1.5")); err != nil {
		t.Skip("the fixture fonts are not fetched (make html-fixtures)")
	}
	fontsOnce.Do(func() {
		fonts, fontsErr = text.NewFonts(context.Background(), nil, []string{
			filepath.Join(dir, "liberation-2.1.5"), filepath.Join(dir, "dejavu-2.37"),
		})
	})
	if fontsErr != nil {
		t.Fatalf("indexing the fixture fonts: %v", fontsErr)
	}
	return fonts
}

// convertDoc converts the package data with opts.
func convertDoc(t testing.TB, data []byte, opts office.Options) *compose.Document {
	t.Helper()
	d, err := Convert(context.Background(), bytes.NewReader(data), fixtureFonts(t), opts)
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	return d
}

// textLine is a line of drawn text: its baseline, left edge and text.
type textLine struct {
	y, x, size float64
	text       string
}

// lines returns the text of a page grouped by baseline, top to bottom,
// each line's runs in x order.
func lines(pg *compose.Page) []textLine {
	type run struct {
		x, y, size float64
		s          string
	}
	var runs []run
	var walk func(frags []compose.Fragment, dx, dy float64)
	walk = func(frags []compose.Fragment, dx, dy float64) {
		for _, f := range frags {
			switch v := f.(type) {
			case *compose.Text:
				var sb strings.Builder
				for _, g := range v.Glyphs {
					sb.WriteString(g.Text)
				}
				x := v.Origin.X + dx
				if len(v.Glyphs) > 0 {
					x += v.Glyphs[0].X
				}
				runs = append(runs, run{x: x, y: v.Origin.Y + dy, size: v.Size, s: sb.String()})
			case *compose.Group:
				m := v.Transform
				walk(v.Content, dx+m.E, dy+m.F)
			}
		}
	}
	walk(pg.Content, 0, 0)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].y < runs[j].y })
	var out []textLine
	for _, r := range runs {
		if n := len(out); n > 0 && math.Abs(out[n-1].y-r.y) < 1.5 {
			l := &out[n-1]
			if r.x < l.x {
				l.text = r.s + l.text
				l.x = r.x
			} else {
				l.text += r.s
			}
			continue
		}
		out = append(out, textLine{y: r.y, x: r.x, size: r.size, text: r.s})
	}
	return out
}

// pageText returns a page's lines joined by newlines, each trimmed.
func pageText(pg *compose.Page) string {
	var sb strings.Builder
	for i, l := range lines(pg) {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(strings.TrimSpace(l.text))
	}
	return sb.String()
}

// docText returns every page's text, pages separated by a form feed.
func docText(d *compose.Document) string {
	var parts []string
	for _, pg := range d.Pages {
		parts = append(parts, pageText(pg))
	}
	return strings.Join(parts, "\f")
}

// near reports a and b within tol.
func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }
