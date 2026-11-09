package build

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestBuildersMatchCommitted builds every registered fixture and compares it
// with the committed ../pdfs/<name>.pdf byte for byte. Run one fixture with
// go test -run 'TestBuildersMatchCommitted/annots_1b$'.
func TestBuildersMatchCommitted(t *testing.T) {
	t.Parallel()
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want, err := os.ReadFile(filepath.Join("..", "pdfs", name+".pdf"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := Build(name)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Equal(got, want) {
				return
			}
			i := 0
			for i < len(got) && i < len(want) && got[i] == want[i] {
				i++
			}
			lo := max(0, i-120)
			t.Errorf("%s differs at byte %d (got %d bytes, want %d)\ngot:  %q\nwant: %q",
				name, i, len(got), len(want), got[lo:min(len(got), i+120)], want[lo:min(len(want), i+120)])
			if dir := os.Getenv("PDFGEN_DUMP"); dir != "" {
				_ = os.WriteFile(filepath.Join(dir, name+".pdf"), got, 0o600) //nolint:errcheck // debugging aid only.
			}
		})
	}
}
