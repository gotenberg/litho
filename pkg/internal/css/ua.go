package css

import (
	"context"
	_ "embed"
	"sync"
)

//go:embed ua.css
var uaCSS []byte

// HTMLUserAgent returns litho's user agent stylesheet for HTML, after the
// HTML Standard, 15 (Rendering): the display, margins and fonts of the
// elements a printed page shows. It is parsed once and shared.
func HTMLUserAgent() *Stylesheet { return uaSheet() }

var uaSheet = sync.OnceValue(func() *Stylesheet {
	// The embedded sheet is far below every cap and fetches nothing, so
	// Parse cannot fail on it; TestHTMLUserAgent_ParsesClean checks it.
	s, _ := Parse(context.Background(), uaCSS, ParseOptions{userAgent: true})
	return s
})

//go:embed ua-mathml.css
var uaMathCSS []byte

// MathMLUserAgent returns litho's user agent stylesheet for MathML, the
// user agent stylesheet of MathML Core, Appendix A, which a cascade holds
// after [HTMLUserAgent] for a document with MathML. It is parsed once and
// shared.
func MathMLUserAgent() *Stylesheet { return uaMathSheet() }

var uaMathSheet = sync.OnceValue(func() *Stylesheet {
	// The embedded sheet is far below every cap and fetches nothing, so
	// Parse cannot fail on it; TestMathMLUserAgent_ParsesClean checks it.
	s, _ := Parse(context.Background(), uaMathCSS, ParseOptions{userAgent: true})
	return s
})

//go:embed ua-quirks.css
var uaQuirksCSS []byte

// QuirksUserAgent returns litho's user agent stylesheet for an HTML
// document in quirks mode, which a cascade holds after [HTMLUserAgent]:
// the rules the HTML Standard's Rendering section expects in quirks mode
// alone. It is parsed once and shared.
func QuirksUserAgent() *Stylesheet { return uaQuirksSheet() }

var uaQuirksSheet = sync.OnceValue(func() *Stylesheet {
	// The embedded sheet is far below every cap and fetches nothing, so
	// Parse cannot fail on it; TestQuirksUserAgent_ParsesClean checks it.
	s, _ := Parse(context.Background(), uaQuirksCSS, ParseOptions{userAgent: true})
	return s
})
