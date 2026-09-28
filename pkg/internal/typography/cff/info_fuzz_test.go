package cff

import "testing"

// FuzzReadInfo feeds arbitrary bytes to [ReadInfo], which reads a font program
// a remediation caller supplies. The contract is crash-freedom.
func FuzzReadInfo(f *testing.F) {
	valid := testCFF()
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		_, _ = ReadInfo(data)
	})
}
