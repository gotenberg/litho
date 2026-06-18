package compliance

import "testing"

// FuzzParseICCHeader asserts the ICC header parser never panics on crafted input.
func FuzzParseICCHeader(f *testing.F) {
	f.Add(make([]byte, 128)) // Minimal ICC profile header.
	f.Add([]byte("icc"))     // Shorter than the header.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_, _ = parseICCHeader(data)
	})
}

// FuzzNormaliseICCProfileID asserts profile-ID normalisation never panics.
func FuzzNormaliseICCProfileID(f *testing.F) {
	f.Add(make([]byte, 128)) // Full-length profile.
	f.Add([]byte("short"))   // Below the profile-ID offset.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = normaliseICCProfileID(data)
	})
}
