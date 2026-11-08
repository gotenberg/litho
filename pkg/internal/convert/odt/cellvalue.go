package odt

import (
	"encoding/xml"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/numfmt"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// This file shows the values of table cells as Writer does: a cell with
// an office:value-type whose cell style names a data style and whose
// paragraphs hold no text shows its value in that format, and a string
// cell its office:string-value, in place of its paragraphs' text.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/fdo60842.odt prints its date, currency and string
//     cells by their values ("18/02/2012", "500 000,00", "USD") where the
//     string cell's paragraph says "This text should not appear".

// maxStyleChain bounds the parent styles followed for a cell's data
// style. Anti-DoS guard, not spec-mandated.
const maxStyleChain = 32

// cellDataStyle returns the data style name of the cell style name,
// following its parent styles (style:data-style-name, ODF 1.3 Part 3,
// 19.479), "" for none.
func (b *builder) cellDataStyle(name string) string {
	for range maxStyleChain {
		if name == "" {
			return ""
		}
		s := b.styles.Style(b.scope, odf.FamilyCell, name)
		if s == nil || s.El == nil {
			return ""
		}
		if ds, ok := s.El.Lookup(odf.NSStyle, "data-style-name"); ok {
			return ds
		}
		name = s.Parent
	}
	return ""
}

// cellValueText returns the text Writer shows for the table cell c in
// place of its paragraphs' text, false when it shows the paragraphs.
func (b *builder) cellValueText(c *odf.Element) (string, bool) {
	vt := c.A(odf.NSOffice, "value-type")
	if vt == "string" {
		return c.Lookup(odf.NSOffice, "string-value")
	}
	if strings.TrimSpace(c.TextContent()) != "" {
		// A cell whose paragraphs hold text shows it, whatever its value
		// (tdf131025_noZerosInTable: value 0 beside "Banana").
		return "", false
	}
	var v numfmt.Value
	switch vt {
	case "float", "percentage", "currency":
		n, err := strconv.ParseFloat(strings.TrimSpace(c.A(odf.NSOffice, "value")), 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return "", false
		}
		v = numfmt.Value{Kind: numfmt.Number, Number: n}
	case "date":
		n, ok := dateSerial(c.A(odf.NSOffice, "date-value"))
		if !ok {
			return "", false
		}
		v = numfmt.Value{Kind: numfmt.Number, Number: n}
	default:
		return "", false
	}
	name := b.cellDataStyle(c.A(odf.NSTable, "style-name"))
	if name == "" {
		return "", false
	}
	if b.data == nil {
		b.data = odf.NewDataStyles(b.pkg)
	}
	code := b.data.Code(b.scope, name)
	el := b.data.Element(b.scope, name)
	if el != nil && el.Is(odf.NSNumber, "date-style") && el.A(odf.NSNumber, "format-source") == "language" {
		if c, ok := languageDates[el.A(odf.NSNumber, "language")]; ok {
			code = c
		}
	}
	if code == "" {
		return "", false
	}
	f, err := numfmt.Parse(code)
	if err != nil {
		return "", false
	}
	if el != nil {
		lang := el.A(odf.NSNumber, "language")
		if cc := el.A(odf.NSNumber, "country"); lang != "" && cc != "" {
			lang += "-" + cc
		}
		if lang != "" {
			f = f.WithLocale(numfmt.LocaleOf(lang))
		}
	}
	return f.Apply(v).Text, true
}

// languageDates holds the short date format of a language, which a date
// style of number:format-source language prints in whatever its parts
// say (ODF 1.3 Part 3, 19.351): LibreOffice's locale data. Only the
// languages the corpus shows are listed.
//
// # Corpus evidence
//
//   - libreoffice/odt: odfimport/fdo60842.odt, fr LU: 2012-02-18 prints 18/02/2012.
var languageDates = map[string]string{"fr": "DD/MM/YYYY"}

// nullDate is day 0 of the serial numbers data styles format dates by,
// 1899-12-30 (ODF 1.3 Part 3, 9.4.2 table:null-date's default).
var nullDate = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

// dateSerial parses an office:date-value (19.385, an XML Schema date or
// dateTime) to days since [nullDate].
func dateSerial(s string) (float64, bool) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "Z")
	for _, l := range []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.Sub(nullDate).Hours() / 24, true
		}
	}
	return 0, false
}

// valueParagraph returns the cell c with its paragraphs replaced by one
// holding text, styled as its first paragraph.
func valueParagraph(c *odf.Element, text string) *odf.Element {
	p := &odf.Element{Name: xml.Name{Space: odf.NSText, Local: "p"}}
	for _, k := range c.Children {
		if k.Is(odf.NSText, "p") || k.Is(odf.NSText, "h") {
			p.Attr = k.Attr
			break
		}
	}
	if text != "" {
		p.Children = []*odf.Element{{Text: text}}
	}
	out := *c
	out.Children = []*odf.Element{p}
	return &out
}
