package truetype

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// FuzzCollection feeds arbitrary bytes to [Faces], [ReadInfoAt] and
// [ExtractFace], which read the font files of the directories a remediation
// searches. The contract is crash-freedom, and an extracted face reads as a
// program.
func FuzzCollection(f *testing.F) {
	valid := fontbuild.Collection(testProgram(0), collectionFace("TestSans", "Regular", 600))
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add(minimalSfnt())
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		r, size := bytes.NewReader(data), int64(len(data))
		dirs, err := Faces(r, size)
		if err != nil {
			return
		}
		for _, dir := range dirs {
			if info, err := ReadInfoAt(r, size, dir); err == nil {
				_ = info.EmbeddingRestricted()
			}
			if program, err := ExtractFace(r, size, dir, 1<<20); err == nil && !IsSfnt(program) {
				t.Errorf("extracted face at %d is no sfnt program", dir)
			}
		}
	})
}
