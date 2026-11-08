package odp

import (
	"context"
	"testing"
)

func FuzzConvert(f *testing.F) {
	full := flat{
		styles: presStyles + `<draw:gradient draw:name="g" draw:style="radial"/><draw:stroke-dash draw:name="d" draw:dots1="1"/>`,
		auto:   `<style:style style:name="s" style:family="graphic"><style:graphic-properties draw:fill="gradient" draw:fill-gradient-name="g" draw:stroke="dash" draw:stroke-dash="d"/></style:style>`,
		body: page(`<draw:frame presentation:class="title" svg:width="5cm" svg:height="1cm"><draw:text-box><text:p>T<text:s text:c="2"/></text:p></draw:text-box></draw:frame>` +
			`<draw:custom-shape draw:style-name="s" draw:transform="rotate (1) translate (1cm 1cm)" svg:width="2cm" svg:height="1cm"><text:list><text:list-item><text:p>a</text:p></text:list-item></text:list><draw:enhanced-geometry draw:type="star5" draw:modifiers="1 2"/></draw:custom-shape>` +
			`<draw:g><draw:line svg:x1="0cm" svg:y1="0cm" svg:x2="1cm" svg:y2="1cm"/></draw:g>` +
			`<draw:frame svg:width="4cm" svg:height="2cm"><table:table><table:table-row><table:table-cell><text:p>c</text:p></table:table-cell></table:table-row></table:table></draw:frame>`),
	}.bytes()
	f.Add(full)
	f.Add(full[:len(full)/2])
	f.Add(zipOf(f, "mimetype", "application/vnd.oasis.opendocument.presentation",
		"content.xml", `<office:document-content `+nsDecl+`><office:body><office:presentation><draw:page/></office:presentation></office:body></office:document-content>`))
	f.Fuzz(func(t *testing.T, data []byte) {
		doc, _, err := Read(context.Background(), data, nil, "")
		if err != nil {
			return
		}
		if doc.Width <= 0 || doc.Height <= 0 {
			t.Fatalf("slide size %v x %v", doc.Width, doc.Height)
		}
	})
}

func FuzzParseTransform(f *testing.F) {
	f.Add("rotate (0.5235987756) translate (10cm 5cm)")
	f.Add("skewX (-3.72417297208683E-017) rotate (-0.349065850398866) translate (5.111cm 2.326cm)")
	f.Add("matrix (1 0 0 1 1cm 2cm) scale (2)")
	f.Add("rotate (")
	f.Fuzz(func(t *testing.T, s string) {
		m, _, ok := parseTransform(s)
		if ok {
			_ = m.place(10, 10)
		}
	})
}
