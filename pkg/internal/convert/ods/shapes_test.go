package ods

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
	frags := box.Picture.Vector(context.Background(), typeset.Rect{Width: box.Width, Height: box.Height})
	fill := firstFill(frags)
	if fill == nil || !near(fill.Color.R, 0x72/255.0) || !near(fill.Color.B, 0xcf/255.0) {
		t.Errorf("box fill = %+v", fill)
	}
}

// firstFill returns the first fill of frags, nil when none.
func firstFill(frags []typeset.Fragment) *typeset.Fill {
	for _, f := range frags {
		switch f := f.(type) {
		case *typeset.Group:
			if fl := firstFill(f.Content); fl != nil {
				return fl
			}
		case *typeset.Path:
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

// TestRead_ShapeGroupDepth locks the bound on draw:g nesting: a shape
// inside [maxShapeGroupDepth] groups is drawn, one inside a group more is
// left out with a warning.
func TestRead_ShapeGroupDepth(t *testing.T) {
	nested := func(groups int, name string) string {
		return strings.Repeat(`<draw:g>`, groups) +
			`<draw:rect draw:name="` + name + `" svg:x="0in" svg:y="0in" svg:width="1in" svg:height="1in"/>` +
			strings.Repeat(`</draw:g>`, groups)
	}
	body := `<table:table table:name="S"><table:shapes>` + nested(maxShapeGroupDepth, "kept") + nested(maxShapeGroupDepth+1, "deep") + `</table:shapes></table:table>`
	doc, w := read(t, flat{body: body}.bytes())
	ds := doc.Sheets[0].Drawings
	if len(ds) != 1 || ds[0].Name != "kept" {
		t.Errorf("drawings %+v, want the shape kept alone", ds)
	}
	if !hasWarning(w, warn.LimitReached) {
		t.Errorf("no group-depth warning in %+v", w)
	}
}

// TestSpacedText_Cap locks the bound on a shape's text: a paragraph of
// many wide text:s stops one write past [maxFieldText].
func TestSpacedText_Cap(t *testing.T) {
	root, err := odf.ParseXML(context.Background(), []byte(`<text:p `+nsDecl+`>`+strings.Repeat(`<text:s text:c="1024"/>`, 1000)+`</text:p>`))
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	spacedText(root, &sb, "", 0)
	if n := sb.Len(); n <= maxFieldText || n > 2*maxFieldText {
		t.Errorf("%d bytes of text, want past %d by one write at most", n, maxFieldText)
	}
}

// TestRead_RowsUnderDrawingsKeepTheirHeights locks the heights of the
// rows above and under a cell-anchored shape that no cell of the sheet
// holds a value in: they place the shape, as Calc places it.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf137044_CoverHiddenRows.ods prints its
//     rectangle, anchored 5 mm into the fourth row under three rows of
//     10 mm, 35 mm below the sheet's origin; sc/LargeAnchorOffset.ods and
//     sc/tdf137082_LTR_arrow_image.ods their shapes below rows of their
//     own height.
func TestRead_RowsUnderDrawingsKeepTheirHeights(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="ro1" style:family="table-row"><style:table-row-properties style:row-height="10mm" style:use-optimal-row-height="false"/></style:style>` +
			`<style:style style:name="ro2" style:family="table-row"><style:table-row-properties style:row-height="30mm" style:use-optimal-row-height="false"/></style:style>`,
		body: `<table:table table:name="S">` +
			`<table:table-row table:style-name="ro1" table:number-rows-repeated="3"><table:table-cell/></table:table-row>` +
			`<table:table-row table:style-name="ro2"><table:table-cell>` +
			`<draw:custom-shape draw:name="box" svg:x="5mm" svg:y="5mm" svg:width="15mm" svg:height="110mm" table:end-cell-address="S.A7" table:end-x="20mm" table:end-y="25mm">` +
			`<draw:enhanced-geometry svg:viewBox="0 0 21600 21600" draw:type="rectangle" draw:enhanced-path="M 0 0 L 21600 0 21600 21600 0 21600 Z N"/></draw:custom-shape>` +
			`</table:table-cell></table:table-row>` +
			`<table:table-row table:style-name="ro2" table:number-rows-repeated="1048572"><table:table-cell/></table:table-row>` +
			`</table:table>`,
	}
	doc, _ := read(t, f.bytes())
	sh := doc.Sheets[0]
	if len(sh.Drawings) != 1 {
		t.Fatalf("drawings %d, want 1", len(sh.Drawings))
	}
	heights := map[int]float64{}
	for _, r := range sh.Rows {
		heights[r.Index] = r.Height
	}
	for i := range 3 {
		if h, ok := heights[i]; !ok || !near(h, 28.35) {
			t.Errorf("row %d height %v (listed %v), want 28.35", i, h, ok)
		}
	}
	if !near(sh.DefaultRowHeight, 85.05) {
		t.Errorf("default row height %v, want 85.05", sh.DefaultRowHeight)
	}
}

// TestRead_StandardRowsUnderATallDefault locks a row of Calc's standard
// height above a drawing on a sheet whose last row element, and so its
// default row, is taller: the row keeps the standard height.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/tdf115655_HideDetail.ods prints its picture,
//     anchored in a second row of 74.86 mm, below a first row of the
//     standard height.
func TestRead_StandardRowsUnderATallDefault(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="ro2" style:family="table-row"><style:table-row-properties style:row-height="74.86mm" style:use-optimal-row-height="false"/></style:style>`,
		body: `<table:table table:name="S">` +
			`<table:table-row><table:table-cell/></table:table-row>` +
			`<table:table-row table:style-name="ro2"><table:table-cell>` +
			`<draw:custom-shape draw:name="box" svg:x="0mm" svg:y="0mm" svg:width="15mm" svg:height="15mm">` +
			`<draw:enhanced-geometry svg:viewBox="0 0 21600 21600" draw:type="rectangle" draw:enhanced-path="M 0 0 L 21600 0 21600 21600 0 21600 Z N"/></draw:custom-shape>` +
			`</table:table-cell></table:table-row></table:table>`,
	}
	doc, _ := read(t, f.bytes())
	sh := doc.Sheets[0]
	if len(sh.Rows) == 0 || sh.Rows[0].Index != 0 || !near(sh.Rows[0].Height, defaultRowHeight) {
		t.Errorf("rows %+v, want row 0 at %v", sh.Rows, defaultRowHeight)
	}
}

// TestRead_ShapeBoxIsItsOutline locks the box a shape takes on the sheet:
// that of its outline's points, not of the turned box they are fitted to
// (see [outlinePoints]). A polyline of three points turned by -2.4939
// radians covers 44.9 mm across, its box 51.74 mm.
func TestRead_ShapeBoxIsItsOutline(t *testing.T) {
	f := flat{
		body: `<table:table table:name="S"><table:shapes>` +
			`<draw:polyline draw:name="P" svg:width="50.42mm" svg:height="19.11mm" draw:transform="rotate (-2.4939009681747) translate (145mm 25mm)" svg:viewBox="0 0 5043 1912" draw:points="0,0 5043,0 4184,1912"/>` +
			`</table:shapes><table:table-row><table:table-cell/></table:table-row></table:table>`,
	}
	doc, _ := read(t, f.bytes())
	d := doc.Sheets[0].Drawings
	if len(d) != 1 {
		t.Fatalf("%d drawings, want 1", len(d))
	}
	// Within a point: the box grows by half the line's width.
	mm := 72 / 25.4
	if math.Abs(d[0].Width-44.9*mm) > 1 || math.Abs(d[0].X-100.1*mm) > 1 {
		t.Errorf("box from x %.2f, %.2f wide; want %.2f, %.2f", d[0].X, d[0].Width, 100.1*mm, 44.9*mm)
	}
}

// TestMeasureGuides locks the guides of a dimension line (see
// [measureArt]): each from its point's guide length less the guide
// distance past the point to the guide overhang past the dimension line.
func TestMeasureGuides(t *testing.T) {
	f := flat{
		auto: `<style:style style:name="gr1" style:family="graphic"><style:graphic-properties draw:stroke="solid" draw:line-distance="1.82mm" draw:start-guide="5.95mm" draw:end-guide="7.31mm"/></style:style>`,
		body: `<table:table table:name="S"><table:shapes><draw:measure draw:style-name="gr1" svg:x1="10mm" svg:y1="50mm" svg:x2="110mm" svg:y2="50mm"/></table:shapes>` +
			`<table:table-row><table:table-cell/></table:table-row></table:table>`,
	}
	doc, _ := read(t, f.bytes())
	ds := doc.Sheets[0].Drawings
	if len(ds) != 1 || ds[0].Picture == nil {
		t.Fatalf("drawings %+v", ds)
	}
	var lengths []float64
	for _, fr := range ds[0].Picture.Vector(t.Context(), typeset.Rect{Width: ds[0].Width, Height: ds[0].Height}) {
		var walk func(typeset.Fragment)
		walk = func(fr typeset.Fragment) {
			switch v := fr.(type) {
			case *typeset.Group:
				for _, c := range v.Content {
					walk(c)
				}
			case *typeset.Path:
				if v.Stroke != nil && len(v.Ops) == 2 && v.Ops[0].P[0].X == v.Ops[1].P[0].X {
					lengths = append(lengths, math.Abs(v.Ops[1].P[0].Y-v.Ops[0].P[0].Y))
				}
			}
		}
		walk(fr)
	}
	mm := 72 / 25.4
	want := []float64{(1.82 + 2 + 5.95 - 1) * mm, (1.82 + 2 + 7.31 - 1) * mm}
	if len(lengths) != 2 || math.Abs(lengths[0]-want[0]) > 0.01 || math.Abs(lengths[1]-want[1]) > 0.01 {
		t.Errorf("guides %v points long, want %v", lengths, want)
	}
}
