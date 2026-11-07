package ods

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/png"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
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
	`xmlns:calcext="urn:org:documentfoundation:names:experimental:calc:xmlns:calcext:1.0" ` +
	`xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0"`

// flat is a flat spreadsheet document's parts.
type flat struct {
	fonts, styles, auto, master, body, meta string
}

// bytes returns the flat document.
func (f flat) bytes() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?><office:document ` + nsDecl +
		` office:version="1.3" office:mimetype="application/vnd.oasis.opendocument.spreadsheet">` +
		`<office:meta>` + f.meta + `</office:meta>` +
		`<office:font-face-decls>` + f.fonts + `</office:font-face-decls>` +
		`<office:styles>` + f.styles + `</office:styles>` +
		`<office:automatic-styles>` + f.auto + `</office:automatic-styles>` +
		`<office:master-styles>` + f.master + `</office:master-styles>` +
		`<office:body><office:spreadsheet>` + f.body + `</office:spreadsheet></office:body></office:document>`)
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
func read(t testing.TB, data []byte) (*sheet.Document, []compose.Warning) {
	t.Helper()
	doc, w, err := Read(context.Background(), data, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	return doc, w
}

// table wraps rows in a table named name.
func table(name, inner string) string {
	return `<table:table table:name="` + name + `">` + inner + `</table:table>`
}

// cellAt returns the cell at row, column of sh, nil when none.
func cellAt(sh *sheet.Sheet, row, col int) *sheet.Cell {
	for i := range sh.Rows {
		if sh.Rows[i].Index != row {
			continue
		}
		for j := range sh.Rows[i].Cells {
			if sh.Rows[i].Cells[j].Column == col {
				return &sh.Rows[i].Cells[j]
			}
		}
	}
	return nil
}

// hasWarning reports the warning code among w.
func hasWarning(w []compose.Warning, code string) bool {
	for _, x := range w {
		if x.Code == code {
			return true
		}
	}
	return false
}

// tinyPNG returns a 2 by 1 PNG.
func tinyPNG(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
