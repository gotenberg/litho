package css

import "testing"

// TestQuirksUserAgent_ParsesClean parses the quirks mode user agent sheet
// without a warning.
func TestQuirksUserAgent_ParsesClean(t *testing.T) {
	ua := QuirksUserAgent()
	if ua != QuirksUserAgent() {
		t.Fatal("QuirksUserAgent parses again")
	}
	if w := ua.Warnings(); len(w) > 0 {
		t.Fatalf("warnings: %v", w)
	}
	if len(ua.rules) == 0 {
		t.Fatal("no rule")
	}
}
