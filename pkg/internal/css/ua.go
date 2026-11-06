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
	s, _ := Parse(context.Background(), uaCSS, ParseOptions{}) //nolint:errcheck // fixed input, checked by a test
	return s
})
