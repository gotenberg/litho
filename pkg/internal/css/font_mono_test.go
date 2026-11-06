package css

import (
	"math"
	"testing"
)

// TestFontSize_MonospaceQuirk locks the monospace size rule of
// [fixMonospaceSize] and the keyword table of [keywordSizes] against the
// sizes Chromium computes for the same markup (getComputedStyle in the
// pinned Gotenberg image), in pixels.
func TestFontSize_MonospaceQuirk(t *testing.T) {
	t.Parallel()
	doc := `<!doctype html><body>
<p><code id=c1>x</code></p>
<pre id=c2>x</pre>
<p><span id=c3 style="font-family: monospace, serif">x</span></p>
<p><span id=c4 style="font-family: monospace; font-size: 16px">x</span></p>
<p><code id=c5 style="font-size: 1em">x</code></p>
<p><code id=c6 style="font-size: 100%">x</code></p>
<p><code id=c7 style="font-size: 0.875rem">x</code></p>
<p><code id=c8 style="font-size: small">x</code></p>
<pre><span id=c9 style="font-family: serif">x</span></pre>
<div style="font-size: 20px"><code id=c10>x</code></div>
<div style="font-size: 1.25em"><code id=c11>x</code></div>
<pre><code id=c12>x</code></pre>
<p><code><span id=c13 style="font-family: sans-serif">x</span></code></p>
<p><span id=c14 style="font-family: 'Courier New', monospace">x</span></p>
<h1><code id=c16>x</code></h1>
<p><code id=c17 style="font-size: larger">x</code></p>
<p><span id=c18 style="font: 1em monospace">x</span></p>
<p><code id=c19 style="font-size: calc(1em + 0px)">x</code></p>
<p><span id=c20 style="font-family: monospace; font-size: medium">x</span></p>
<p><span id=c21 style="font-family: MONOSPACE">x</span></p>
<p><kbd id=c23>x</kbd></p>
<div style="font-family: monospace"><p id=c25>x</p></div>
<div style="font-family: monospace"><p id=c26 style="font-size: 2em">x</p></div>
<pre id=c27 style="font-family: serif">x</pre>
<div style="font-size: 120%"><code id=c28>x</code></div>
<div style="font-size: large"><code id=c29>x</code></div>
<div style="font-size: 10pt"><code id=c30>x</code></div>
<p><code id=c31 style="font-size: smaller">x</code></p>
<p><code id=c32 style="font-size: inherit">x</code></p>
<div style="font-family: monospace; font-size: 20px"><span id=c33 style="font-family: serif">x</span></div>
<div style="font-family: monospace; font-size: 1em"><span id=c34 style="font-family: serif">x</span></div>
<p><code id=c35 style="font-size: initial">x</code></p>
<div style="font-size: 2rem"><code id=c36>x</code></div>
<pre style="font-size: 2em"><span id=c38 style="font-family: serif">x</span></pre>
<p id=c39 style="font-size: xx-large">x</p>
</body>`
	c := compute(t, doc, "")
	for _, tt := range []struct {
		id string
		px float64
	}{
		{"c1", 13}, {"c2", 13}, {"c3", 16}, {"c4", 16}, {"c5", 13}, {"c6", 13},
		{"c7", 14}, {"c8", 12}, {"c9", 16}, {"c10", 20}, {"c11", 16.25}, {"c12", 13},
		{"c13", 16}, {"c14", 16}, {"c16", 26}, {"c17", 15.6}, {"c18", 13}, {"c19", 16},
		{"c20", 13}, {"c21", 13}, {"c23", 13}, {"c25", 13}, {"c26", 26}, {"c27", 16},
		{"c28", 15.6}, {"c29", 16}, {"c30", 13.3333}, {"c31", 10.8333}, {"c32", 13},
		{"c33", 20}, {"c34", 16}, {"c35", 13}, {"c36", 32}, {"c38", 32}, {"c39", 32},
	} {
		if got := c.of(tt.id).FontSize / Pt; math.Abs(got-tt.px) > 1e-3 {
			t.Errorf("%s: font-size = %.4fpx, want %vpx", tt.id, got, tt.px)
		}
	}
}
