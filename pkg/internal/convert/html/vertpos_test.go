package html

import "testing"

// TestVertical_OrthogonalRootIsMonolithic locks the fragmentation of the
// root of an orthogonal flow: it moves whole to the next page, or column,
// when it straddles a cut, and its content, laid out across the cut's
// axis, paints on the page the root lands on, whatever its length along
// the line (CSS Writing Modes 4, 7.3; CSS Fragmentation 3, 4.1).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) prints each
//     box where the table below has it, to the pixel at 96 dpi, in
//     vertical-rl, vertical-lr and sideways-rl.
func TestVertical_OrthogonalRootIsMonolithic(t *testing.T) {
	t.Parallel()
	const head = `<style>@page { size: 600px 400px; margin: 0 } html, body { margin: 0 } .v { border: 2px solid lime }`
	for _, c := range []struct {
		name, css, body string
		want            map[string]string
	}{
		{
			"straddling a page", `.pre { height: 300px } .v { width: 300px; height: 200px; margin-left: 200px }`,
			`<div class=pre></div><div class=v></div>`,
			map[string]string{"1:g": "", "2:g": "200,0+304x204"},
		},
		{
			"longer than the page's rest", `.pre { height: 350px } .v { width: 300px; height: 40px; margin-left: 100px } .f { width: 150px } .c { width: 100px; background: red }`,
			`<div class=pre></div><div class=v><div class=f></div><div class=c></div></div>`,
			map[string]string{"1:g": "100,350+304x44", "1:r": "152,352+100x40", "2:r": ""},
		},
		{
			"straddling a column", `.mc { columns: 2; column-gap: 20px; height: 360px; margin: 10px } .pre { height: 100px } .v { width: 200px; height: 200px }`,
			`<div class=mc><div class=pre></div><div class=v></div></div>`,
			map[string]string{"1:g": "310,10+204x204"},
		},
	} {
		for _, mode := range []string{"vertical-rl", "vertical-lr", "sideways-rl"} {
			doc := convertDoc(t, head+` .v { writing-mode: `+mode+` }`+c.css+`</style>`+c.body, Options{PreferCSSPageSize: true})
			got := pxBoxes(doc)
			for k, w := range c.want {
				page := int(k[0] - '1')
				g := ""
				if page < len(got) {
					g = got[page][k[2:]]
				}
				if mode == "vertical-lr" && k == "1:r" {
					w = "252,352+100x40"
				}
				if g != w {
					t.Errorf("%s, %s, %s: %q, want %q", c.name, mode, k, g, w)
				}
			}
		}
	}
}
