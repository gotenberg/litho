package compliance

import "testing"

// FuzzVeraPDFHeaderVersion asserts that reading the version number of a header
// line never panics. The bytes are attacker-controlled: they are the start of
// an untrusted document.
func FuzzVeraPDFHeaderVersion(f *testing.F) {
	f.Add([]byte("%PDF-2.0"))
	f.Add([]byte("%PDF-2"))
	f.Add([]byte("junk %PDF-2.0junk"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		veraPDFHeaderVersion(data)
		javaParseFloat(string(data))
	})
}
