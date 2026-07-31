package compliance

import "testing"

// FuzzRichTextContent asserts that extracting the text of an /RC payload never
// panics. The payload is attacker-controlled: it arrives verbatim from the
// annotation dictionary of an untrusted document.
func FuzzRichTextContent(f *testing.F) {
	f.Add([]byte("<?xml version='1.0'?><body><p>the plain text</p></body>"))
	f.Add([]byte("<body><p>truncated"))
	f.Add([]byte(""))

	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = richTextContent(data)
	})
}
