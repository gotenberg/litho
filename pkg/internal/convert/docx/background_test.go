package docx

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

func TestPageBackground(t *testing.T) {
	t.Parallel()
	d := build(t, `<w:p/>`, nil).d
	parse := func(x string) *ooxml.Element {
		t.Helper()
		e, err := ooxml.Parse(context.Background(), []byte(`<w:document `+nsDecl+`>`+x+`</w:document>`), nil)
		if err != nil {
			t.Fatal(err)
		}
		return e.Child(w, "background")
	}
	green := css.Color{R: 0x92 / 255.0, G: 0xD0 / 255.0, B: 0x50 / 255.0, A: 1}
	if st := d.pageBackground(parse(`<w:background w:color="92D050"/>`)); st == nil || st.BackgroundColor != green || st.BackgroundGradient != nil {
		t.Errorf("solid background %+v", st)
	}
	for _, x := range []string{``, `<w:background w:color="FFFFFF"/>`, `<w:background w:color="auto"/>`} {
		if st := d.pageBackground(parse(x)); st != nil {
			t.Errorf("%q paints a background", x)
		}
	}
	st := d.pageBackground(parse(`<w:background w:color="FFF2CC"><v:background fillcolor="#fff2cc"><v:fill color2="#deeaf6" type="gradient"/></v:background></w:background>`))
	if st == nil || st.BackgroundGradient == nil || len(st.BackgroundGradient.Stops) != 3 || st.BackgroundSize.Height != css.Pts(gradientTile) || st.BackgroundAttachment != css.AttachFixed {
		t.Fatalf("gradient background %+v", st)
	}
	if c := st.BackgroundGradient.Stops[1].Color; c.B < c.R {
		t.Errorf("the gradient's middle stop is %+v, want color2", c)
	}
	files := pkgFiles(`<w:p><w:r><w:t>x</w:t></w:r></w:p>`, nil)
	files["word/document.xml"] = strings.Replace(files["word/document.xml"], `<w:body>`, `<w:background w:color="92D050"/><w:body>`, 1)
	doc := convertBytes(t, zipFiles(t, files), office.Options{})
	pg := doc.Pages[0]
	p, ok := pg.Content[0].(*compose.Path)
	if !ok || p.Fill == nil {
		t.Fatalf("the page's first fragment is %T, want the background", pg.Content[0])
	}
}
