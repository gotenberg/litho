package compliance

import "testing"

// FuzzBERToDER asserts berToDER never panics on arbitrary input.
func FuzzBERToDER(f *testing.F) {
	f.Add([]byte{0x30, 0x80, 0x02, 0x01, 0x05, 0x00, 0x00})
	f.Add([]byte{0x30, 0x80, 0x02, 0x01})
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = berToDER(data)
	})
}
