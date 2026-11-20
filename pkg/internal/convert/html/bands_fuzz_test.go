package html

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
)

// FuzzPageDecorations fuzzes a band's HTML and CSS, drawn as a header on
// every page, isolated as a footer on the last, with room made for both,
// and the @page rules that name pages and fill page-margin boxes.
func FuzzPageDecorations(f *testing.F) {
	f.Add(`<span class="t"></span> <a href="#x">x</a>`,
		`.t::after{content:string(document-title) " " counter(page) "/" counter(pages, upper-roman)} body{font-size:30px}`,
		`@page{margin:2cm;@top-center{content:"T " counter(page) "/" counter(pages, upper-roman)}@bottom-left{content:attr(x, "f");border:1px solid}}`+
			`@page wide{size:landscape;@left-middle{content:"L";height:20%}}@page :blank{@top-left-corner{content:"B"}}.w{page:wide}h2{break-before:left}`)
	f.Add(`<div style="height:500pt">tall</div>`, `div{display:flex}`,
		`@page{@top-left{content:"aaaaaaaaaa";width:auto;margin:auto}@top-center{content:"";width:50%}@top-right{content:"b";min-width:5px}}`)
	f.Add(`</litho-band></body></html><style>p{color:red}</style><p>x`, `} p { color: red } {`, `@page{margin:0}`)
	f.Fuzz(func(t *testing.T, html, sheetOfBand, sheet string) {
		src := `<html><head><title>T</title><style>` + sheet + `</style></head><body><p>a</p><div class="w"><p>b</p></div><h2 id="x">c</h2><p>d</p></body></html>`
		o := Options{
			MarginTop: 40, MarginBottom: 40, MaxInputBytes: 1 << 14, MaxPages: 16,
			Bands: []Band{
				{HTML: html, CSS: sheetOfBand, Room: RoomContent},
				{Footer: true, HTML: html, CSS: sheetOfBand, Isolated: true, Pages: models.LastPage().Or(models.PagesFrom(2))},
			},
		}
		doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), o)
		if errors.Is(err, limits.ErrInputTooLarge) || errors.Is(err, limits.ErrTooManyPages) || errors.Is(err, ErrTooDeep) ||
			errors.Is(err, ErrTooManyElements) || errors.Is(err, css.ErrTooComplex) || errors.Is(err, layout.ErrTooManyBoxes) {
			return
		}
		if errors.Is(err, typeset.ErrInvalidOptions) && len(html)+len(sheetOfBand) > maxBandBytes {
			return
		}
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		var b bytes.Buffer
		if err := typeset.Write(context.Background(), doc, &b, typeset.Options{}); err != nil {
			t.Fatalf("typeset.Write: %v", err)
		}
	})
}
