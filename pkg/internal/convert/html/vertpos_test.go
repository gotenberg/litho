package html

import (
	"strings"
	"testing"
)

// verticalBoxes are absolutely positioned boxes of a vertical container,
// one colour each: physical insets (r, g, m), flow-relative ones (y) and
// percentages of the containing block on both axes (k).
const verticalBoxes = `<div id=a class=p></div><div id=b class=p></div><div id=c class=p></div><div id=d class=p></div><div id=g class=p></div>`

// verticalBoxCSS styles [verticalBoxes]; POS is their position.
const verticalBoxCSS = `@page { size: 600px 400px; margin: 0 } html, body { margin: 0 }
	.p { position: POS; width: 40px; height: 60px }
	#a { left: 200px; top: 100px; background: red }
	#b { right: 10px; top: 10px; background: lime }
	#c { bottom: 20px; left: 30px; background: fuchsia }
	#d { inset-block-start: 15px; inset-inline-end: 25px; background: yellow }
	#g { left: 10%; top: 20%; width: 10%; height: 15%; background: aqua }`

// TestVertical_PositionedInTheirContainingBlock locks the place of an
// absolutely positioned box of an orthogonal flow: its containing block is
// the one CSS 2.1, 10.1 names, a positioned vertical container, a
// positioned or transformed horizontal ancestor through a static vertical
// container, or the initial containing block, and for a fixed box the page
// area; the physical insets measure from its sides (CSS Writing Modes 4,
// 6), the flow-relative ones map by the box's own writing mode, and
// percentages resolve against its size on both axes.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) prints each
//     box where the table below has it, to the pixel at 96 dpi, in
//     vertical-rl, vertical-lr and sideways-rl, but the aqua box inside a
//     horizontal ancestor, 57.6 pixels wide, which it fills 57 pixels of.
func TestVertical_PositionedInTheirContainingBlock(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, css, body, pos string
		// want holds the boxes, the yellow one by writing mode.
		want             map[string]string
		yellowRL, yellow string
	}{
		{
			"positioned vertical container",
			`.v { position: relative; width: 420px; height: 300px; margin: 20px 30px 20px 120px; border: 2px solid gray }`,
			`<div class=v>` + verticalBoxes + `</div>`, "absolute",
			map[string]string{"r": "322,122+40x60", "g": "492,32+40x60", "m": "152,242+40x60", "k": "164,82+42x45"},
			"487,237+40x60", "137,237+40x60",
		},
		{
			"positioned horizontal ancestor",
			`.h { position: relative; margin: 10px; padding: 15px; height: 330px; border: 2px solid gray } .v { width: 300px; height: 250px; margin-left: 150px; border: 2px solid gray }`,
			`<div class=h><div class=v>` + verticalBoxes + `</div></div>`, "absolute",
			map[string]string{"r": "212,112+40x60", "g": "538,22+40x60", "m": "42,292+40x60", "k": "70,84+58x54"},
			"533,287+40x60", "27,287+40x60",
		},
		{
			"transformed horizontal ancestor",
			`.h { transform: translate(7px, 9px); margin: 10px; padding: 15px; height: 330px; border: 2px solid gray } .v { width: 300px; height: 250px; margin-left: 150px; border: 2px solid gray }`,
			`<div class=h><div class=v>` + verticalBoxes + `</div></div>`, "absolute",
			map[string]string{"r": "219,121+40x60", "g": "545,31+40x60", "m": "49,301+40x60", "k": "77,93+58x54"},
			"540,296+40x60", "34,296+40x60",
		},
		{
			"initial containing block",
			`.v { width: 300px; height: 250px; margin: 40px 0 0 200px; border: 2px solid gray }`,
			`<div class=v>` + verticalBoxes + `</div>`, "absolute",
			map[string]string{"r": "200,100+40x60", "g": "550,10+40x60", "m": "30,320+40x60", "k": "60,80+60x60"},
			"545,315+40x60", "15,315+40x60",
		},
		{
			"page area",
			`.v { width: 300px; height: 250px; margin: 40px 0 0 200px; border: 2px solid gray }`,
			`<div class=v>` + verticalBoxes + `</div>`, "fixed",
			map[string]string{"r": "200,100+40x60", "g": "550,10+40x60", "m": "30,320+40x60", "k": "60,80+60x60"},
			"545,315+40x60", "15,315+40x60",
		},
	}
	for _, c := range cases {
		for _, mode := range []string{"vertical-rl", "vertical-lr", "sideways-rl"} {
			css := strings.ReplaceAll(verticalBoxCSS, "POS", c.pos) + ` .v { writing-mode: ` + mode + ` }` + c.css
			doc := convertDoc(t, `<style>`+css+`</style>`+c.body, Options{PreferCSSPageSize: true})
			if len(doc.Pages) != 1 {
				t.Errorf("%s, %s: %d pages, want 1", c.name, mode, len(doc.Pages))
				continue
			}
			got := pxBoxes(doc)[0]
			want := map[string]string{"y": c.yellowRL}
			if mode == "vertical-lr" {
				want["y"] = c.yellow
			}
			for k, w := range c.want {
				want[k] = w
			}
			for k, w := range want {
				if got[k] != w {
					t.Errorf("%s, %s, box %s: %q, want %q", c.name, mode, k, got[k], w)
				}
			}
		}
	}
}

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
