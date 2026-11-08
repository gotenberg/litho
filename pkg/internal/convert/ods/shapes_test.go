package ods

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/odf"

	"github.com/gotenberg/litho/pkg/internal/compose"
)

func TestParseTransform(t *testing.T) {
	tests := []struct {
		in   string
		x, y float64
		ok   bool
	}{
		{"translate (1in 2in)", 72 + 10, 144, true},
		{"rotate (1.5707963267949) translate (1in 0in)", 72, -10, true},
		{"skewX (0.785398163397448)", 10, 0, true},
		{"scale (2) translate(1pt, 1pt)", 21, 1, true},
		{"matrix (1 0 0 1 1pt 2pt)", 11, 2, true},
		{"rotate", 10, 0, false},
		{"turn (1)", 10, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			m, ok := parseTransform(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if !ok {
				return
			}
			x, y := apply(m, 10, 0)
			if math.Abs(x-tc.x) > 1e-6 || math.Abs(y-tc.y) > 1e-6 {
				t.Errorf("(10, 0) maps to (%v, %v), want (%v, %v)", x, y, tc.x, tc.y)
			}
		})
	}
}

func FuzzParseTransform(f *testing.F) {
	f.Add("rotate (0.5) translate (1cm 2cm)")
	f.Add("skewX (0.1) rotate (")
	f.Fuzz(func(t *testing.T, s string) {
		parseTransform(s)
	})
}

// TestRead_Shapes locks how drawing shapes are read: placed from their
// anchor cell's corner, with LibreOffice's pool colours where no style
// sets one, under their transform; not drawn when their anchor row is
// hidden or when they lie wholly left of the sheet.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/hideColsShow.ods prints its custom shape 15 mm right of its
//     anchor column C, as its svg:x says; sc/LargeAnchorOffset.ods its
//     dimension line from its anchor column's left edge;
//     sc/legacycellanchoredrotatedhiddenshape.ods no shape, its anchor row
//     hidden; sc/formula-across-sheets.ods no line, both left of the
//     sheet's origin.
func TestRead_Shapes(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="co1" style:family="table-column"><style:table-column-properties style:column-width="1in"/></style:style>` +
			`<style:style style:name="ro1" style:family="table-row"><style:table-row-properties style:row-height="0.5in"/></style:style>` +
			`<style:style style:name="gr1" style:family="graphic"><style:graphic-properties draw:stroke="none"/></style:style>`,
		body: `<table:table table:name="S"><table:shapes>` +
			`<draw:rect draw:name="turned" svg:x="1in" svg:y="1in" svg:width="1in" svg:height="2in" draw:transform="rotate (1.5707963267949) translate (1in 3in)"/>` +
			`</table:shapes><table:table-column table:style-name="co1" table:number-columns-repeated="8"/>` +
			`<table:table-row table:style-name="ro1"><table:table-cell/><table:table-cell>` +
			`<draw:custom-shape draw:name="box" draw:style-name="gr1" svg:x="0.25in" svg:y="0.1in" svg:width="1in" svg:height="0.5in" table:end-cell-address="S.C2" table:end-x="0.25in" table:end-y="0.1in">` +
			`<draw:enhanced-geometry svg:viewBox="0 0 21600 21600" draw:type="rectangle" draw:enhanced-path="M 0 0 L 21600 0 21600 21600 0 21600 Z N"/></draw:custom-shape>` +
			`<draw:measure draw:name="dim" svg:x1="0in" svg:y1="0in" svg:x2="1in" svg:y2="0in"/>` +
			`<draw:line draw:name="away" svg:x1="-5in" svg:y1="-5in" svg:x2="-3in" svg:y2="-5in"/>` +
			`</table:table-cell></table:table-row>` +
			`<table:table-row table:style-name="ro1" table:visibility="collapse"><table:table-cell><draw:ellipse draw:name="hidden" svg:x="0in" svg:y="0in" svg:width="1in" svg:height="1in"/></table:table-cell></table:table-row>` +
			`</table:table>`,
	}
	doc, _ := read(t, f.bytes())
	byName := map[string]int{}
	ds := doc.Sheets[0].Drawings
	for i, d := range ds {
		byName[d.Name] = i
		if !d.Unbounded || d.Picture == nil || d.Picture.Vector == nil {
			t.Errorf("%s = %+v", d.Name, d)
		}
	}
	if len(ds) != 3 {
		t.Fatalf("drawings %v, want turned, box and dim", byName)
	}
	// A quarter turn counterclockwise of a 1 by 2 inch box at (1in, 1in)
	// moved by (1in, 3in) covers x 2 to 4 inches, y 1 to 2, grown by half
	// the hairline.
	if d := ds[byName["turned"]]; !d.Absolute || !near(d.X, 143.75) || !near(d.Y, 71.75) || !near(d.Width, 144.5) || !near(d.Height, 72.5) {
		t.Errorf("turned = %+v", d)
	}
	if d := ds[byName["box"]]; d.Absolute || d.From.Row != 0 || d.From.Column != 1 || !near(d.From.DX, 18) || !near(d.From.DY, 7.2) ||
		d.To == nil || d.To.Column != 2 || d.To.Row != 1 {
		t.Errorf("box = %+v to %+v", d, d.To)
	}
	// The dimension line lies 0.8 cm above its points, its guides 2 mm
	// past it: a box an inch wide and 1 cm tall, grown by the hairline.
	if d := ds[byName["dim"]]; d.Absolute || d.From.Column != 1 || !near(d.Width, 73) || !near(d.Height, 29.35) {
		t.Errorf("dim = %+v", d)
	}
	box := ds[byName["box"]]
	frags := box.Picture.Vector(context.Background(), compose.Rect{Width: box.Width, Height: box.Height})
	fill := firstFill(frags)
	if fill == nil || !near(fill.Color.R, 0x72/255.0) || !near(fill.Color.B, 0xcf/255.0) {
		t.Errorf("box fill = %+v", fill)
	}
}

// firstFill returns the first fill of frags, nil when none.
func firstFill(frags []compose.Fragment) *compose.Fill {
	for _, f := range frags {
		switch f := f.(type) {
		case *compose.Group:
			if fl := firstFill(f.Content); fl != nil {
				return fl
			}
		case *compose.Path:
			if f.Fill != nil {
				return f.Fill
			}
		}
	}
	return nil
}

func TestMeasureValue(t *testing.T) {
	for pt, want := range map[float64]string{37 * 72 / 25.4: "37", 79.99 * 72 / 25.4: "79.99", 12.5 * 72 / 25.4: "12.5", 0: "0"} {
		if got := measureValue(pt); got != want {
			t.Errorf("measureValue(%v) = %q, want %q", pt, got, want)
		}
	}
}

func TestSpacedText(t *testing.T) {
	f := flat{body: `<table:table table:name="S"><table:table-row><table:table-cell><draw:measure svg:x1="0in" svg:y1="0in" svg:x2="1in" svg:y2="0in">` +
		`<text:p><text:measure text:kind="gap"/><text:s/><text:measure text:kind="value">25,4</text:measure><text:s text:c="3"/><text:measure text:kind="unit">mm</text:measure></text:p>` +
		`</draw:measure></table:table-cell></table:table-row></table:table>`}
	root, err := odf.ParseXML(context.Background(), f.bytes())
	if err != nil {
		t.Fatal(err)
	}
	var find func(e *odf.Element) *odf.Element
	find = func(e *odf.Element) *odf.Element {
		if e.Is(odf.NSText, "p") {
			return e
		}
		for _, c := range e.Children {
			if p := find(c); p != nil {
				return p
			}
		}
		return nil
	}
	var sb strings.Builder
	spacedText(find(root), &sb, "25.4", 0)
	if sb.String() != " 25.4   mm" {
		t.Errorf("text %q", sb.String())
	}
}
