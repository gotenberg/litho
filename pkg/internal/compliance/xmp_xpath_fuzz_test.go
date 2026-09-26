package compliance

import "testing"

// FuzzXPathCompiles feeds arbitrary text to [xpathCompiles], which reads an
// XPath-typed XMP value straight out of a document's metadata stream, and
// asserts crash-freedom.
func FuzzXPathCompiles(f *testing.F) {
	f.Add("/a/b[1][@c='d'] | x:f($v, 1.5)")
	f.Add("concat('a', substring(")
	f.Add("")

	f.Fuzz(func(_ *testing.T, expr string) {
		if len(expr) > 1<<16 {
			return
		}
		_ = xpathCompiles(expr)
	})
}
