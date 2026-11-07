package html

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// FuzzPageDecorations fuzzes the header and footer templates and the
// @page rules that name pages and fill page-margin boxes.
func FuzzPageDecorations(f *testing.F) {
	f.Add(`<span class="date"></span> <span class="title text"></span>`,
		`<style>body{font-size:12px;margin:auto 20px}</style><p><span class="pageNumber"></span> of <span class="totalPages"></span></p>`,
		`@page{margin:2cm;@top-center{content:"T " counter(page) "/" counter(pages, upper-roman)}@bottom-left{content:attr(x, "f");border:1px solid}}`+
			`@page wide{size:landscape;@left-middle{content:"L";height:20%}}@page :blank{@top-left-corner{content:"B"}}.w{page:wide}h2{break-before:left}`)
	f.Add(`<div class="text left">L</div><div class="text center">C</div><div class="text right">R</div>`, ``,
		`@page{@top-left{content:"aaaaaaaaaa";width:auto;margin:auto}@top-center{content:"";width:50%}@top-right{content:"b";min-width:5px}}`)
	f.Add(`<html><head><style>#header{display:none}</style></head><body><div class="text">x`, `</div></body></html>`, `@page{margin:0}`)
	f.Fuzz(func(t *testing.T, header, footer, sheet string) {
		src := `<html><head><title>T</title><style>` + sheet + `</style></head><body><p>a</p><div class="w"><p>b</p></div><h2>c</h2><p>d</p></body></html>`
		o := Options{
			HeaderTemplate: header, FooterTemplate: footer, MarginTop: 40, MarginBottom: 40,
			Date: time.Date(2026, 11, 6, 10, 30, 0, 0, time.UTC), BaseURL: "https://example.com/",
			MaxInputBytes: 1 << 14, MaxPages: 16,
		}
		doc, err := Convert(context.Background(), strings.NewReader(src), testFonts(t), o)
		if errors.Is(err, ErrInputTooLarge) || errors.Is(err, ErrTooManyPages) || errors.Is(err, ErrTooDeep) ||
			errors.Is(err, ErrTooManyElements) || errors.Is(err, css.ErrTooComplex) || errors.Is(err, layout.ErrTooManyBoxes) {
			return
		}
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		var b bytes.Buffer
		if err := compose.Write(context.Background(), doc, &b, compose.Options{}); err != nil {
			t.Fatalf("compose.Write: %v", err)
		}
	})
}
