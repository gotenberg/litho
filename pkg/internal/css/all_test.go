package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestAll checks the all shorthand (CSS Cascade 5, 3.2): it resets every
// property but direction and unicode-bidi to the CSS-wide keyword it is
// given, keeps custom properties, and refuses any other value.
func TestAll(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body style="direction: rtl; color: blue; font-size: 20px; --x: 4px">
<div id=i style="all: initial"><div id=v style="width: var(--x)"></div></div>
<div id=h style="all: inherit"></div>
<div id=u style="all: unset; unicode-bidi: isolate"></div>
<p id=bad style="all: block"></p></body>`, ``)
	i := c.of("i")
	if i.Display != Inline || !colorEq(i.Color, rgb(0, 0, 0)) || !near(i.FontSize, 12) {
		t.Errorf("all: initial: display %v, color %v, font-size %v", i.Display, i.Color, i.FontSize)
	}
	if i.Direction != RTL {
		t.Errorf("all: initial reset direction: %v", i.Direction)
	}
	if v := c.of("v"); !dimEq(v.Width, pts(3)) {
		t.Errorf("all: initial reset a custom property: width %v", v.Width)
	}
	if h := c.of("h"); h.Display != Block || !colorEq(h.Color, rgb(0, 0, 255)) {
		t.Errorf("all: inherit: display %v, color %v", h.Display, h.Color)
	}
	if u := c.of("u"); u.Display != Inline || !colorEq(u.Color, rgb(0, 0, 255)) || u.UnicodeBidi != Isolate {
		t.Errorf("all: unset: display %v, color %v, unicode-bidi %v", u.Display, u.Color, u.UnicodeBidi)
	}
	if b := c.of("bad"); b.Display != Block {
		t.Errorf("all: block applied: display %v", b.Display)
	}
	ws := c.c.Warnings()
	if !hasWarning(ws, warn.CSSInvalid, `"all"`) {
		t.Errorf("no warning for all: block: %v", ws)
	}
	if hasWarning(ws, warn.CSSUnsupported, `"all"`) {
		t.Errorf("all warned as unsupported: %v", ws)
	}
}
