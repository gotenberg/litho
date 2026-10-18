package cff

import "testing"

// FuzzCharStringsReachable feeds arbitrary bytes to [CharStringsReachable],
// which reads an embedded font program compliance judges. The contract is
// crash-freedom.
func FuzzCharStringsReachable(f *testing.F) {
	valid := testCFF()
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		CharStringsReachable(data)
	})
}
