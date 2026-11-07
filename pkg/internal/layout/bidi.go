package layout

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// bidiControls returns the explicit formatting characters an inline box of
// style st opens before its content and closes after it, as CSS Writing
// Modes 4, 2.4.2 maps unicode-bidi onto UAX #9: embed opens an embedding,
// isolate an isolate, bidi-override an override, isolate-override an
// override inside an isolate, and plaintext an isolate whose direction its
// first strong character sets (FSI). normal opens nothing.
func bidiControls(st *css.Style) (open, closing []rune) {
	rtl := st.Direction == css.RTL
	pick := func(l, r rune) rune {
		if rtl {
			return r
		}
		return l
	}
	switch st.UnicodeBidi {
	case css.Embed:
		return []rune{pick(text.LRE, text.RLE)}, []rune{text.PDF}
	case css.Isolate:
		return []rune{pick(text.LRI, text.RLI)}, []rune{text.PDI}
	case css.BidiOverride:
		return []rune{pick(text.LRO, text.RLO)}, []rune{text.PDF}
	case css.IsolateOverride:
		return []rune{pick(text.LRI, text.RLI), pick(text.LRO, text.RLO)}, []rune{text.PDF, text.PDI}
	case css.Plaintext:
		return []rune{text.FSI}, []rune{text.PDI}
	case css.BidiNormal:
	}
	return nil, nil
}

// blockOverride returns the override a block container of style st puts
// on its inline content: bidi-override and isolate-override on a block
// container override the direction of its every character (CSS Writing
// Modes 4, 2.2).
func blockOverride(st *css.Style) []rune {
	if st.UnicodeBidi != css.BidiOverride && st.UnicodeBidi != css.IsolateOverride {
		return nil
	}
	if st.Direction == css.RTL {
		return []rune{text.RLO}
	}
	return []rune{text.LRO}
}

// bidiScope is an inline box whose explicit formatting characters are in
// force in the paragraph being collected.
type bidiScope struct {
	box           *box
	open, closing []rune
	// emitted reports whether the openers went on a run of the current
	// paragraph.
	emitted bool
}

// openBidi records the explicit formatting characters inline box k opens.
func (c *collector) openBidi(k *box) {
	open, closing := bidiControls(k.st)
	if len(open) == 0 {
		return
	}
	c.bidi = append(c.bidi, &bidiScope{box: k, open: open, closing: closing})
}

// closeBidi closes the explicit formatting characters inline box k opened
// on the last run of the paragraph, or drops them when it put no run in
// it.
func (c *collector) closeBidi(k *box) {
	n := len(c.bidi)
	if n == 0 || c.bidi[n-1].box != k {
		return
	}
	sc := c.bidi[n-1]
	c.bidi = c.bidi[:n-1]
	if sc.emitted && len(c.cur.runs) > 0 {
		r := &c.cur.runs[len(c.cur.runs)-1]
		r.Close = append(r.Close, sc.closing...)
	}
}

// addRun appends the run r of info to the paragraph, opening on it the
// explicit formatting characters of the scopes that have not opened in
// the paragraph yet, outermost first.
func (c *collector) addRun(r text.ParagraphRun, info runInfo) {
	if c.cur.runs == nil {
		r.Open = append(r.Open, blockOverride(c.root.st)...)
	}
	for _, sc := range c.bidi {
		if !sc.emitted {
			r.Open = append(r.Open, sc.open...)
			sc.emitted = true
		}
	}
	c.cur.runs = append(c.cur.runs, r)
	c.cur.infos = append(c.cur.infos, info)
}

// reopenBidi starts a paragraph after a forced break: the scopes in force
// open again on its first run, a paragraph ending every embedding (UAX
// #9, BD7 and X8).
func (c *collector) reopenBidi() {
	for _, sc := range c.bidi {
		sc.emitted = false
	}
}
