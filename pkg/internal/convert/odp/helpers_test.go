package odp

import (
	"archive/zip"
	"bytes"
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
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
	`xmlns:number="urn:oasis:names:tc:opendocument:xmlns:datastyle:1.0" ` +
	`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" ` +
	`xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0" ` +
	`xmlns:anim="urn:oasis:names:tc:opendocument:xmlns:animation:1.0" ` +
	`xmlns:dr3d="urn:oasis:names:tc:opendocument:xmlns:dr3d:1.0" ` +
	`xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0"`

// flat is a flat presentation's parts: the office:styles content, the
// automatic styles, the master styles and the office:presentation body.
type flat struct {
	fonts, styles, auto, master, body, meta string
}

// bytes returns the flat document; a master page "Default" of a 28 cm by
// 15.75 cm page layout stands in when master is empty.
func (f flat) bytes() []byte {
	master, auto := f.master, f.auto
	if master == "" {
		master = `<style:master-page style:name="Default" style:page-layout-name="pm1"/>`
		auto += `<style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="28cm" fo:page-height="15.75cm"/></style:page-layout>`
	}
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><office:document ` + nsDecl +
		` office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.presentation">` +
		`<office:meta>` + f.meta + `</office:meta>` +
		`<office:font-face-decls>` + f.fonts + `</office:font-face-decls>` +
		`<office:styles>` + f.styles + `</office:styles>` +
		`<office:automatic-styles>` + auto + `</office:automatic-styles>` +
		`<office:master-styles>` + master + `</office:master-styles>` +
		`<office:body><office:presentation>` + f.body + `</office:presentation></office:body></office:document>`)
}

// page wraps shapes in a draw:page of the default master.
func page(shapes string) string {
	return `<draw:page draw:name="page1" draw:master-page-name="Default">` + shapes + `</draw:page>`
}

// zipOf returns a zip of the entries, name then body, the first stored.
func zipOf(t testing.TB, entries ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i+1 < len(entries); i += 2 {
		m := zip.Deflate
		if i == 0 {
			m = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entries[i], Method: m})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entries[i+1])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// read reads data, failing the test on an error.
func read(t testing.TB, data []byte) (*slides.Document, []compose.Warning) {
	t.Helper()
	doc, w, err := Read(context.Background(), data, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return doc, w
}

// firstShape reads a one-page document of shapes and returns its first
// shape that is not a master's.
func firstShape(t testing.TB, f flat) *slides.Shape {
	t.Helper()
	doc, _ := read(t, f.bytes())
	if len(doc.Slides) == 0 || len(doc.Slides[0].Shapes) == 0 {
		t.Fatal("no shape read")
	}
	return doc.Slides[0].Shapes[0]
}

// cm converts centimetres to points.
func cm(v float64) float64 { return v * 72 / 2.54 }

// near reports a and b within a hundredth of a point.
func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

// rect returns the rectangle of corner x, y and size w by h in
// centimetres, in points.
func rect(x, y, w, h float64) compose.Rect {
	return compose.Rect{X: cm(x), Y: cm(y), Width: cm(w), Height: cm(h)}
}

// nearRect reports a and b within a hundredth of a point on every side.
func nearRect(a, b compose.Rect) bool {
	return near(a.X, b.X) && near(a.Y, b.Y) && near(a.Width, b.Width) && near(a.Height, b.Height)
}

// nearAll reports a and b of the same length, each within a hundredth of
// a point.
func nearAll(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !near(a[i], b[i]) {
			return false
		}
	}
	return true
}

// hasWarning reports a warning of code among w.
func hasWarning(w []compose.Warning, code string) bool {
	for _, x := range w {
		if x.Code == code {
			return true
		}
	}
	return false
}

// texts returns the text of each paragraph.
func texts(ps []slides.Paragraph) []string {
	var out []string
	for _, p := range ps {
		var b strings.Builder
		for _, r := range p.Runs {
			if r.Break {
				b.WriteString("\n")
			}
			b.WriteString(r.Text)
		}
		out = append(out, b.String())
	}
	return out
}
