package cff

import "testing"

// FuzzEmptyCharStrings feeds arbitrary bytes to [EmptyCharStrings], which
// reads an embedded font program compliance judges. The contract is
// crash-freedom.
func FuzzEmptyCharStrings(f *testing.F) {
	valid := testCFF()
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _, _ = EmptyCharStrings(data)
	})
}
