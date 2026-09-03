package compliance

import "testing"

// FuzzExtractCMapStreamWMode asserts WMode extraction never panics.
func FuzzExtractCMapStreamWMode(f *testing.F) {
	f.Add([]byte("/WMode 1 def"))
	f.Add([]byte("/WMode")) // No value follows.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = ExtractCMapStreamWMode(data)
	})
}

// FuzzExtractUseCMap asserts usecmap-name extraction never panics.
func FuzzExtractUseCMap(f *testing.F) {
	f.Add([]byte("/Identity-H usecmap"))
	f.Add([]byte("usecmap")) // No preceding name.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = extractUseCMap(data)
	})
}
