package remediation

import "testing"

// FuzzCmapHasForbiddenDestination asserts the ToUnicode forbidden-destination
// scan never panics on crafted CMap bytes.
func FuzzCmapHasForbiddenDestination(f *testing.F) {
	f.Add([]byte("beginbfchar <0001> <0041> endbfchar"))
	f.Add([]byte("beginbfchar")) // Unterminated block.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = cmapHasForbiddenDestination(data)
	})
}

// FuzzCmapOperatorBlocks asserts the begin/end block extractor never panics.
func FuzzCmapOperatorBlocks(f *testing.F) {
	f.Add([]byte("beginbfrange <0000> <00FF> <0041> endbfrange"))
	f.Add([]byte("beginbfrange <00")) // Unterminated block.
	f.Fuzz(func(_ *testing.T, data []byte) {
		_ = cmapOperatorBlocks(data, "bfchar")
		_ = cmapOperatorBlocks(data, "bfrange")
	})
}
