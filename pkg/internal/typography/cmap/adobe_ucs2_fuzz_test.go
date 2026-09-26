package cmap

import "testing"

// FuzzParseAdobeUCS2 feeds arbitrary bytes to the reader of Adobe's text
// PDF-to-Unicode resources and asserts crash-freedom. The reader reads only
// the resources litho embeds, whose names a document chooses.
func FuzzParseAdobeUCS2(f *testing.F) {
	valid, _, err := adobeUCS2Source("Adobe-Korea1-UCS2")
	if err != nil {
		f.Fatalf("read embedded resource: %v", err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxAdobeUCS2Bytes {
			return
		}
		_, _, _ = parseAdobeUCS2(data)
	})
}
