package odt

import (
	"archive/zip"
	"bytes"
	"context"
	"hash/crc32"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// nsDecl declares the namespaces the test documents use.
const nsDecl = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
	`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
	`xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0" ` +
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
		var w io.Writer
		var err error
		if store {
			// A STORED entry carries its sizes and CRC up front, no data
			// descriptor, as LibreOffice requires of the mimetype.
			w, err = zw.CreateRaw(&zip.FileHeader{Name: name, Method: zip.Store, CRC32: crc32.ChecksumIEEE([]byte(body)),
				CompressedSize64: uint64(len(body)), UncompressedSize64: uint64(len(body))})
		} else {
			w, err = zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		}
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	names := make([]string, 0, len(d.pictures))
	for n := range d.pictures {
		names = append(names, n)
	}
	sort.Strings(names)
	// LibreOffice refuses a package whose manifest leaves an entry out.
	manifest := `<?xml version="1.0" encoding="UTF-8"?><manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">` +
		`<manifest:file-entry manifest:full-path="/" manifest:version="1.3" manifest:media-type="application/vnd.oasis.opendocument.text"/>` +
		`<manifest:file-entry manifest:full-path="content.xml" manifest:media-type="text/xml"/>` +
		`<manifest:file-entry manifest:full-path="styles.xml" manifest:media-type="text/xml"/>` +
		`<manifest:file-entry manifest:full-path="meta.xml" manifest:media-type="text/xml"/>`
	for _, n := range names {
		manifest += `<manifest:file-entry manifest:full-path="` + n + `" manifest:media-type=""/>`
	}
	add("mimetype", "application/vnd.oasis.opendocument.text", true)
	add("META-INF/manifest.xml", manifest+`</manifest:manifest>`, false)
	add("content.xml", d.contentXML(), false)
	add("styles.xml", d.stylesXML(), false)
	meta := d.meta
	add("meta.xml", `<?xml version="1.0" encoding="UTF-8"?><office:document-meta `+nsDecl+`><office:meta>`+meta+`</office:meta></office:document-meta>`, false)
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

// run is one text run of a page in page coordinates.
type run struct {
	x, y, size float64
	text       string
}

// runs returns the text runs of a page, groups' translations applied.
func runs(pg *compose.Page) []run {
	var out []run
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
				out = append(out, run{x: x, y: v.Origin.Y + dy, size: v.Size, text: sb.String()})
			case *compose.Group:
				walk(v.Content, dx+v.Transform.E, dy+v.Transform.F)
			}
		}
	}
	walk(pg.Content, 0, 0)
	return out
}

// lines returns the text of a page grouped by baseline, top to bottom,
// each line's runs in x order.
func lines(pg *compose.Page) []textLine {
	runs := runs(pg)
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].y < runs[j].y })
	var out []textLine
	for _, r := range runs {
		if n := len(out); n > 0 && math.Abs(out[n-1].y-r.y) < 1.5 {
			l := &out[n-1]
			if r.x < l.x {
				l.text = r.text + l.text
				l.x = r.x
			} else {
				l.text += r.text
			}
			continue
		}
		out = append(out, textLine{y: r.y, x: r.x, size: r.size, text: r.text})
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

// tinyPNG returns a 4 by 2 red PNG.
func tinyPNG(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for i := range img.Pix {
		img.Pix[i] = 0xff
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newTestBuilder opens d and returns a builder of its layout tree, with
// the fixture fonts.
func newTestBuilder(t testing.TB, d doc) *builder {
	t.Helper()
	pkg, err := odf.Open(context.Background(), d.pack(t), odf.Options{})
	if err != nil {
		t.Fatal(err)
	}
	b := &builder{
		ctx: context.Background(), pkg: pkg, styles: odf.NewStyles(pkg), fonts: fixtureFonts(t),
		metricsCache: map[string][2]float64{}, warned: map[string]bool{}, pics: map[string]picResult{},
		meta: map[*layout.Node]paraMeta{}, tabsRelative: true,
	}
	b.pages = newPageFlow(b, office.Options{})
	return b
}

// odfBody returns the office:text of the builder's document.
func odfBody(b *builder) *odf.Element {
	return odf.Section(b.pkg.Content, "body").Child(odf.NSOffice, "text")
}

// walkNodes calls f on n and its descendants in document order.
func walkNodes(n *layout.Node, f func(*layout.Node)) {
	if n == nil {
		return
	}
	f(n)
	for _, c := range n.Children {
		walkNodes(c, f)
	}
}
