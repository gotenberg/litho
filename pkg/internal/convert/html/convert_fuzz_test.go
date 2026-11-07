package html

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

func FuzzConvert(f *testing.F) {
	valid := `<!doctype html><html lang=en><head><title>T</title><meta charset=utf-8>
<link rel=stylesheet href=s.css media=print><style>@page{size:A5;margin:1cm} p{color:red}</style></head>
<body><h1 id=a>Head</h1><p>Text<br>more <a href="#a">link</a> <img src=i.png alt=i><wbr>end</p>
<ol reversed start=3><li>x<li value=9>y</ol><table><tr><td colspan=2>c</table><input value=v></body>`
	f.Add([]byte(valid))
	f.Add([]byte(valid[:len(valid)/2]))
	f.Add([]byte("\xff\xfe<\x00p\x00>\x00"))
	f.Add([]byte(`<html dir=rtl><style>@page{margin:1cm}@page :first{size:200pt 100pt}@page :left{margin-left:3cm}` +
		`@page :right{size:landscape}h1{break-before:right}p{break-after:left}</style><h1>a</h1><p>b<p>c<h1>d</h1>`))
	f.Add([]byte(`<style>@page a{size:100pt 50pt;@top-left{content:counter(page)}}@page :blank{margin:1pt}.a{page:a}` +
		`p{break-before:right}</style><div class=a><p>x</div><p>y<div style="page:b"><p>z</div>`))
	fsys := fstest.MapFS{
		"s.css": {Data: []byte(`@import "s.css"; body { background: url(i.png) }`)},
		"i.png": {Data: pngBytes(f, 3, 2)},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		o := Options{Resources: fsys, MaxInputBytes: 1 << 14, MaxPages: 16}
		doc, err := Convert(context.Background(), bytes.NewReader(data), testFonts(t), o)
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
