package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestDisplayContents pins the computed value of display: contents (CSS
// Display 3, 2.5): parsed without a warning, never blockified by a float
// or an absolute position (2.7), block on the root (2.8), none on the
// replaced elements and form controls of Appendix B, and kept on the
// elements Appendix B leaves to it.
func TestDisplayContents(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=d>a</div><span id=f>b</span><span id=a>c</span>
<img id=img src=x.png><br id=br><input id=in><svg id=svg></svg><math id=m><mi id=mi>x</mi></math>
<button id=b>x</button><fieldset id=fs><legend id=lg>l</legend></fieldset><slot id=sl></slot>`,
		`#d, #f, #a, #img, #br, #in, #svg, #m, #mi, #b, #fs, #lg { display: contents }
#f { float: left } #a { position: absolute }`)
	for _, id := range []string{"d", "f", "a", "b", "fs", "lg", "sl"} {
		if s := c.of(id); s.Display != Contents {
			t.Errorf("#%s display = %v, want contents", id, s.Display)
		}
	}
	for _, id := range []string{"img", "br", "in", "svg", "m", "mi"} {
		if s := c.of(id); s.Display != DisplayNone {
			t.Errorf("#%s display = %v, want none", id, s.Display)
		}
	}
	if hasWarning(c.c.Warnings(), warn.CSSUnsupported, "contents") {
		t.Errorf("warnings = %v, want none for display: contents", c.c.Warnings())
	}
	r := compute(t, `<p>x</p>`, `html { display: contents }`)
	if s := r.c.Style(byTag(r.doc, "html")); s.Display != Block {
		t.Errorf("root display = %v, want block", s.Display)
	}
}

// TestDisplayContentsHandsItemsToFlex pins that the children and the
// ::before of a display: contents child of a flex or grid container are
// its items, blockified (CSS Display 3, 2.5 and 2.7), while those of a
// display: contents child of a block keep their display.
func TestDisplayContentsHandsItemsToFlex(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div style="display:flex"><div class=c id=c1><div class=c><span id=s1>a</span></div></div></div>
<div style="display:grid"><div class=c><span id=s2>b</span></div></div>
<div><div class=c><span id=s3>c</span></div></div>`,
		`.c { display: contents } .c::before { content: "x" }`)
	for _, id := range []string{"s1", "s2"} {
		if s := c.of(id); s.Display != Block {
			t.Errorf("#%s display = %v, want block: an item of its container", id, s.Display)
		}
	}
	if s := c.of("s3"); s.Display != Inline {
		t.Errorf("#s3 display = %v, want inline", s.Display)
	}
	if b := c.c.Pseudo(byID(c.doc, "c1"), PseudoBefore); b == nil || b.Display != Block {
		t.Errorf("::before of a contents child of a flex container = %+v, want a block", b)
	}
}
