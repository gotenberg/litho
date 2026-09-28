package truetype

import "testing"

// FuzzReadInfo feeds arbitrary bytes to [ReadInfo], which reads a font program
// a remediation caller supplies. The contract is crash-freedom.
func FuzzReadInfo(f *testing.F) {
	valid := testProgram(0)
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add(minimalSfnt())
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		info, err := ReadInfo(data)
		if err == nil {
			_ = info.EmbeddingRestricted()
		}
	})
}
