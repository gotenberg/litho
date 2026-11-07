package odt

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

func TestConvert_TextAndStyles(t *testing.T) {
	cases := []struct {
		name string
		d    doc
		want string
	}{
		{"paragraphs", doc{body: `<text:p>One</text:p><text:p>Two</text:p>`}, "One\nTwo"},
		{"white space collapses", doc{body: `<text:p>  a   b
	c </text:p>`}, "a b c"},
		{"text:s keeps spaces", doc{body: `<text:p>a<text:s text:c="3"/>b</text:p>`}, "a   b"},
		{"line break", doc{body: `<text:p>a<text:line-break/>b</text:p>`}, "a\nb"},
		{"spans", doc{
			contentAuto: `<style:style style:name="T1" style:family="text"><style:text-properties fo:font-weight="bold"/></style:style>`,
			body:        `<text:p>x <text:span text:style-name="T1">bold</text:span> y</text:p>`,
		}, "x bold y"},
		{"hidden text", doc{
			contentAuto: `<style:style style:name="T1" style:family="text"><style:text-properties text:display="none"/></style:style>`,
			body:        `<text:p>a<text:span text:style-name="T1">HIDDEN</text:span>b</text:p>`,
		}, "ab"},
		{"fields show saved content", doc{body: `<text:p><text:date>2020-01-01</text:date> <text:author-name>Ann</text:author-name></text:p>`}, "2020-01-01 Ann"},
		{"soft page break ignored", doc{body: `<text:p>a</text:p><text:soft-page-break/><text:p>b</text:p>`}, "a\nb"},
		{"annotation left out", doc{body: `<text:p>a<office:annotation><dc:creator>x</dc:creator><text:p>NOTE</text:p></office:annotation>b</text:p>`}, "ab"},
		{"tracked deletion left out", doc{body: `<text:tracked-changes><text:changed-region text:id="c1"><text:deletion><text:p>GONE</text:p></text:deletion></text:changed-region></text:tracked-changes><text:p>kept<text:change text:change-id="c1"/></text:p>`}, "kept"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := convertDoc(t, tc.d.pack(t), office.Options{})
			if got := docText(d); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConvert_TextProperties(t *testing.T) {
	cases := []struct {
		name  string
		props string
		check func(t *testing.T, l textLine)
	}{
		{"size in points", `fo:font-size="20pt"`, func(t *testing.T, l textLine) {
			if !near(l.size, 20, 0.01) {
				t.Errorf("size = %v, want 20", l.size)
			}
		}},
		{"size in percent", `fo:font-size="50%"`, func(t *testing.T, l textLine) {
			if !near(l.size, 6, 0.01) {
				t.Errorf("size = %v, want 6", l.size)
			}
		}},
		{"superscript", `style:text-position="super 58%"`, func(t *testing.T, l textLine) {
			if !near(l.size, 12*0.58, 0.01) {
				t.Errorf("size = %v, want 6.96", l.size)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := doc{
				contentAuto: `<style:style style:name="T1" style:family="text"><style:text-properties ` + tc.props + `/></style:style>`,
				body:        `<text:p><text:span text:style-name="T1">X</text:span></text:p>`,
			}
			res := convertDoc(t, d.pack(t), office.Options{})
			ls := lines(res.Pages[0])
			if len(ls) != 1 {
				t.Fatalf("lines = %v", ls)
			}
			tc.check(t, ls[0])
		})
	}
}

func TestConvert_Errors(t *testing.T) {
	fonts := fixtureFonts(t)
	cases := []struct {
		name string
		data []byte
		opts office.Options
		want error
	}{
		{"not a package", []byte("hello"), office.Options{}, office.ErrMalformed},
		{"spreadsheet", []byte(`<office:document ` + nsDecl + ` office:mimetype="application/vnd.oasis.opendocument.spreadsheet"><office:body><office:spreadsheet/></office:body></office:document>`), office.Options{}, office.ErrMalformed},
		{"bad ranges", doc{body: `<text:p>a</text:p>`}.pack(t), office.Options{PageRanges: "3-1"}, office.ErrInvalidOptions},
		{"ranges keep nothing", doc{body: `<text:p>a</text:p>`}.pack(t), office.Options{PageRanges: "5"}, office.ErrInvalidOptions},
		{"too large", doc{body: `<text:p>a</text:p>`}.pack(t), office.Options{MaxInputBytes: 10}, office.ErrInputTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Convert(context.Background(), bytes.NewReader(tc.data), fonts, tc.opts)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestConvert_FlatDocument(t *testing.T) {
	src := `<?xml version="1.0" encoding="UTF-8"?><office:document ` + nsDecl + ` office:mimetype="application/vnd.oasis.opendocument.text">` +
		`<office:automatic-styles><style:page-layout style:name="pm1"><style:page-layout-properties fo:page-width="10cm" fo:page-height="10cm" fo:margin-top="1cm" fo:margin-bottom="1cm" fo:margin-left="1cm" fo:margin-right="1cm"/></style:page-layout></office:automatic-styles>` +
		`<office:master-styles><style:master-page style:name="Standard" style:page-layout-name="pm1"/></office:master-styles>` +
		`<office:body><office:text><text:p>Flat</text:p></office:text></office:body></office:document>`
	d := convertDoc(t, []byte(src), office.Options{})
	if len(d.Pages) != 1 || !near(d.Pages[0].Width, 72/2.54*10, 0.01) {
		t.Fatalf("pages = %d, width %v", len(d.Pages), d.Pages[0].Width)
	}
	if got := docText(d); got != "Flat" {
		t.Errorf("text = %q", got)
	}
}

func TestConvert_Metadata(t *testing.T) {
	d := doc{
		body: `<text:p>a</text:p>`,
		meta: `<dc:title>T</dc:title><dc:creator>C</dc:creator><dc:description>D</dc:description><meta:keyword>k1</meta:keyword><meta:keyword>k2</meta:keyword><meta:creation-date>2020-01-01T00:00:00</meta:creation-date>`,
	}
	res := convertDoc(t, d.pack(t), office.Options{})
	m := res.Metadata
	if m.Title != "T" || m.Author != "C" || m.Subject != "D" || m.Keywords != "k1, k2" || m.Lang != "en-US" {
		t.Errorf("metadata = %+v", m)
	}
}

func TestConvert_Outline(t *testing.T) {
	d := doc{body: `<text:h text:outline-level="1">A</text:h><text:h text:outline-level="2">B</text:h><text:h text:outline-level="1">C</text:h>`}
	res := convertDoc(t, d.pack(t), office.Options{})
	if len(res.Outline) != 2 || res.Outline[0].Title != "A" || len(res.Outline[0].Kids) != 1 || res.Outline[0].Kids[0].Title != "B" {
		t.Fatalf("outline = %+v", res.Outline)
	}
	res = convertDoc(t, d.pack(t), office.Options{OmitOutline: true})
	if len(res.Outline) != 0 {
		t.Errorf("outline kept with OmitOutline: %+v", res.Outline)
	}
	var roles []string
	for _, k := range res.Structure.Kids {
		roles = append(roles, k.Role)
	}
	if got := strings.Join(roles, ","); got != "H1,H2,H1" {
		t.Errorf("structure roles = %s", got)
	}
}
