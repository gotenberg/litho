package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestRenderOne_RecordsTheSettingOfAFileItCannotDraw checks that the index
// of a PDF pdfinfo refuses still says how its corpus draws: the diff cell
// refuses a corpus whose indexes disagree on it, and an index without the
// setting took a whole corpus out of the matrix.
//
// # Corpus evidence
//
//   - poi/ppt slideshow/empty.ppt: Microsoft's PDF of an empty deck holds no
//     page, pdfinfo refuses it, and its index read hide_annotations false in
//     a corpus drawn with true.
func TestRenderOne_RecordsTheSettingOfAFileItCannotDraw(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo is not on PATH")
	}
	dir := t.TempDir()
	pdf := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(pdf, []byte("not a PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := renderOne(t.Context(), pdf, dir, 3, true)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Error == "" {
		t.Fatal("renderOne drew a file that is not a PDF")
	}
	if !idx.HideAnnotations {
		t.Error("HideAnnotations = false on an index pdfinfo refused, want the corpus's setting")
	}
}
