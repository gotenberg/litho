package css

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/warn"
)

// TestInterpolateSize_Silent reads interpolate-size (CSS Sizing 4, 7.3)
// without a warning: it decides only how a sizing keyword interpolates
// with a length, and a printed animation stands at its start or its end.
func TestInterpolateSize_Silent(t *testing.T) {
	t.Parallel()
	c := compute(t, `<body><div id=t></div></body>`, `#t { interpolate-size: allow-keywords; width: 10px }`)
	if ws := c.c.Warnings(); hasWarning(ws, warn.CSSUnsupported, "interpolate-size") {
		t.Errorf("interpolate-size warns: %v", ws)
	}
}
