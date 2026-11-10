package css

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// BenchmarkCompute styles a 10 000-element document against the UA sheet
// and a framework-sized author sheet.
func BenchmarkCompute(b *testing.B) {
	var doc strings.Builder
	doc.WriteString("<!doctype html><html><body>")
	for i := range 2000 {
		fmt.Fprintf(&doc, `<div class="row c%d"><p class="lead">Text <a href="#">link</a> <span id="s%d">x</span></p><ul><li>a</li></ul></div>`, i%50, i)
	}
	var css strings.Builder
	for i := range 3000 {
		fmt.Fprintf(&css, ".c%d .lead > a:not(.x), #s%d, .c%d:nth-child(%dn+1) li { color: #%06x; margin: %dpx var(--g, 2px) }\n", i, i, i%50, i%7+1, i, i%9)
	}
	css.WriteString(":root { --g: 3px } * { box-sizing: border-box }")
	root := parseHTML(&testing.T{}, doc.String())
	sheet, err := Parse(context.Background(), []byte(css.String()), ParseOptions{})
	if err != nil {
		b.Fatal(err)
	}
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: sheet, Origin: Author}})
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints}); err != nil {
			b.Fatal(err)
		}
	}
}
