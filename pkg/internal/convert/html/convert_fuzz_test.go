package html

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"testing/fstest"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
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
	f.Add([]byte(`<style>@page{size:100pt 60pt;bleed:2pt;marks:crop cross;width:80pt;background:red fixed;` +
		`@footnote{max-height:20pt;border-top:1pt solid}@top-left{content:string(t,start) element(h,last) leader(dotted)}}` +
		`h1{string-set:t content() counter(c) content(first-letter)}.n{float:footnote;footnote-display:inline}` +
		`header{position:running(h)}p::after{content:leader('. ') counter(c)}a{&:hover{color:red}& b{x:y}}</style>` +
		`<header>H</header><h1>T</h1><p>a<span class=n>note</span> b<span class=n>two</span></p>`))
	controls := `<style>#m{offset:url(#p) 40%}#n{offset:url(#m) 10%}textarea{field-sizing:fixed}</style>` +
		`<svg width=0 height=0><path id=p d="M0 0 L9 9 Z"/></svg><div id=m>m</div><div id=n>n</div>` +
		`<input size=3><input size=-1 value=x><textarea cols=2 rows=1>a\nb\nc</textarea><select><option>a<optgroup label=g><option>bb</select>` +
		`<select multiple size=2><option selected>x<optgroup label=G><option selected>y<option>z</optgroup></select><select size=0 multiple></select>`
	f.Add([]byte(controls))
	f.Add([]byte(controls[:len(controls)/2]))
	widgets := `<style>.s{border:1px solid;accent-color:#ff0;color-scheme:dark;appearance:none;resize:none;height:40px}</style>` +
		`<input type=checkbox checked><input type=radio disabled checked><input type=range min=9 max=1 step=0 value=x>` +
		`<input type=color value=#12345><input type=file multiple><input type=date value=2026-02-30><input type=time value=25:61>` +
		`<input type=datetime-local value=2026-11-22T13:45:30.123><input type=month value=0001-13><input type=week value=99999-W00>` +
		`<progress value=-3 max=0></progress><meter min=5 max=1 low=9 high=-1 optimum=nan value=3></meter>` +
		`<fieldset disabled><legend><button>b</button></legend><button class=s><p>c<span>d</span></button><select class=s><option>e</select></fieldset>`
	f.Add([]byte(widgets))
	f.Add([]byte(widgets[:len(widgets)/2]))
	fsys := fstest.MapFS{
		"s.css": {Data: []byte(`@import "s.css"; body { background: url(i.png) }`)},
		"i.png": {Data: pngBytes(f, 3, 2)},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		o := Options{Resources: fsys, MaxInputBytes: 1 << 14, MaxPages: 16}
		doc, err := Convert(context.Background(), bytes.NewReader(data), testFonts(t), o)
		if errors.Is(err, limits.ErrInputTooLarge) || errors.Is(err, limits.ErrTooManyPages) || errors.Is(err, ErrTooDeep) ||
			errors.Is(err, ErrTooManyElements) || errors.Is(err, css.ErrTooComplex) || errors.Is(err, layout.ErrTooManyBoxes) {
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
