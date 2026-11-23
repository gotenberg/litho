package css

import "testing"

// TestIf_Branches resolves if() (CSS Values 5, 12) as Chromium 156 printed
// the same rules (see [computeState.ifValue]): each element's width is
// the branch its conditions choose, auto where none is true.
func TestIf_Branches(t *testing.T) {
	t.Parallel()
	ids := []string{"a1", "a3", "a5", "a6", "a7", "a8", "a9", "a10", "a11", "a12", "a13", "a14", "k", "a15", "a17", "a19", "a20", "a22", "a23", "a24", "a27"}
	var doc string
	for _, id := range ids {
		if id == "k" {
			continue
		}
		doc += `<div id=` + id + `>`
		if id == "a14" {
			doc += `<div id=k></div>`
		}
		doc += `</div>`
	}
	c := compute(t, `<!DOCTYPE html>`+doc, `
#a1 { width: if(media(print): 50px; else: 20px) }
#a3 { width: if(media(width > 100px): 60px; else: 20px) }
#a5 { width: if(supports(display: grid): 80px; else: 20px) }
#a6 { width: if(supports(display: foo): 80px; else: 20px) }
#a7 { width: if(supports(selector(:has(a))): 90px; else: 20px) }
#a8 { --x: 1; width: if(style(--x: 1): 100px; else: 20px) }
#a9 { --x: 2; width: if(style(--x: 1): 100px; else: 20px) }
#a10 { width: if(style(--y): 110px; else: 20px) }
#a11 { --y: a; width: if(style(--y): 110px; else: 20px) }
#a12 { width: 33px; width: if(media(screen): 50px) }
#a13 { width: calc(if(media(print): 10px; else: 0px) + 110px) }
#a14 { --x: 1; --w: if(style(--x: 1): 130px; else: 5px) } #k { --x: 2; width: var(--w) }
#a15 { width: if(not media(screen): 140px; else: 20px) }
#a17 { width: 44px; width: if(media(print): ; else: 10px) }
#a19 { width: if(else: 170px) }
#a20 { width: if(media(print): 180px;) }
#a22 { --x: 1; width: if(style(--x: 1) or style(--x: 2): 200px; else: 20px) }
#a23 { --x:  1 ; width: if(style(--x:1): 210px; else: 20px) }
#a24 { --x: 3px; width: if(style(--x: 3px): 220px; else: 20px) }
#a27 { width: if(media(print): if(style(--z): 10px; else: 250px); else: 20px) }`)
	want := map[string]float64{
		"a1": 20, "a3": 60, "a5": 80, "a6": 20, "a7": 90, "a8": 100, "a9": 20, "a10": 20, "a11": 110,
		"a12": -1, "a13": 110, "a14": -1, "k": 130, "a15": 20, "a17": 10, "a19": 170, "a20": -1,
		"a22": 200, "a23": 210, "a24": 220, "a27": 20,
	}
	for id, w := range want {
		got := c.of(id).Width
		if w < 0 {
			if got.Kind != Auto {
				t.Errorf("%s: width %v, want auto", id, got)
			}
			continue
		}
		if got != Pts(w*Pt) {
			t.Errorf("%s: width %v, want %vpx", id, got, w)
		}
	}
}
