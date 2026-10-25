package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestConvertColors_CompensatesByDefault pins the CLI's black point
// compensation: on by default, --bpc still accepted and changing nothing,
// --no-bpc turning it off. The litho fixture usebpc_4's /Default page paints
// in profiles whose black reads L* 42, which compensation moves.
func TestConvertColors_CompensatesByDefault(t *testing.T) {
	t.Parallel()
	src := "../../test/integration/corpus/litho/pdfs/usebpc_4.pdf"
	dir := t.TempDir()
	convert := func(name string, flags ...string) []byte {
		t.Helper()
		out := filepath.Join(dir, name+".pdf")
		args := append(append([]string{"convert-colors", "--target", "rgb"}, flags...), "-o", out, src)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%s: exit %d, stderr %s", name, rc, stderr.String())
		}
		data, err := os.ReadFile(out) //nolint:gosec // the test's own temp file
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	def, opted, off := convert("default"), convert("bpc", "--bpc"), convert("nobpc", "--no-bpc")
	if !bytes.Equal(def, opted) {
		t.Error("--bpc changed the output, though compensation is the default")
	}
	if bytes.Equal(def, off) {
		t.Error("--no-bpc wrote the default output")
	}
}
