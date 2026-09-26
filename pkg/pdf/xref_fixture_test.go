package pdf

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFixturesWriteTwentyByteXrefEntries pins that no test fixture builder in
// the module writes a cross-reference entry of 21 bytes.
//
// ISO 32000-1, 7.5.4 makes each entry exactly 20 bytes, its end-of-line marker
// included: two bytes, space and CR, space and LF, or CR and LF. A space
// followed by CR and LF makes 21. litho reads such a table, so the fixtures
// passed, but qpdf, one of the independent checks a signed or incrementally
// updated output is verified with, reports the table an update builds on as
// damaged ("invalid xref entry") and reconstructs it. The builders wrote the
// space and the CR LF both, and each new fixture copied the pattern from the
// last.
func TestFixturesWriteTwentyByteXrefEntries(t *testing.T) {
	t.Parallel()

	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locating the test source")
	}
	root := filepath.Join(filepath.Dir(self), "..", "..")
	// Built from parts so this file does not match itself.
	crlf := `\r` + `\n`
	patterns := [][]byte{[]byte(" n " + crlf), []byte(" f " + crlf)}

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".claude", "node_modules", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") && !strings.HasSuffix(path, ".py") {
			return nil
		}
		data, err := os.ReadFile(path) //nolint:gosec // Path comes from walking the module's own tree.
		if err != nil {
			return err
		}
		for _, p := range patterns {
			if bytes.Contains(data, p) {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
	if len(offenders) > 0 {
		t.Errorf("%d files write 21-byte cross-reference entries: %s", len(offenders), strings.Join(offenders, ", "))
	}
}
