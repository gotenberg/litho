package cmap

import "testing"

// FuzzParseBCMapBFMappings feeds arbitrary bytes to the binary CMap bfchar and
// bfrange decoder and asserts crash-freedom. The decoder reads only the CMaps
// litho bundles, whose names a document chooses.
func FuzzParseBCMapBFMappings(f *testing.F) {
	valid, err := bcmapFS.ReadFile("bcmaps/Adobe-Korea1-UCS2.bcmap")
	if err != nil {
		f.Fatalf("read bundled bcmap: %v", err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _, _ = parseBCMapBFMappings(data)
	})
}
