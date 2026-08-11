package mutations

import "testing"

// FuzzValidateTTFForStamp feeds arbitrary bytes to the stamp font validator and
// asserts crash-freedom.
//
// The bytes come from a caller-supplied font file rather than from the PDF, but
// the rule is the same: this function's leading argument is a []byte it did not
// produce, and it walks an sfnt table directory to decide whether the font can
// be embedded. A truncated directory or a table offset past the end of the file
// is exactly what it exists to reject, so it must reject rather than panic.
func FuzzValidateTTFForStamp(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte("OTTO\x00\x01\x00\x00"))
	f.Add([]byte("ttcf\x00\x01\x00\x00\x00\x00\x00\x02"))
	f.Add(append([]byte("\x00\x01\x00\x00\x00\x01\x00\x00\x00\x00\x00\x00"),
		[]byte("glyf\x00\x00\x00\x00\xff\xff\xff\xff\xff\xff\xff\xff")...)) // offset past EOF

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_ = ValidateTTFForStamp(data)
	})
}
