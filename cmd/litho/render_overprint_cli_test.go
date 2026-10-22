package main

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// TestRender_Overprint pins --overprint: magenta painted under overprint mode
// 1 over cyan leaves the cyan, which shows as blue, and knocks it out without
// the flag.
func TestRender_Overprint(t *testing.T) {
	const data = "1 0 0 0 k 0 0 40 40 re f /OP gs 0 1 0 0 k 0 0 40 40 re f"
	src := writeTempPDF(t, renderPrintPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 40 40] /Contents 4 0 R /Resources << /ExtGState << /OP 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(data), data),
		"<< /Type /ExtGState /OP true /OPM 1 >>",
	}))
	for _, tc := range []struct {
		flags []string
		cyan  bool
	}{
		{nil, false},
		{[]string{"--overprint"}, true},
	} {
		prefix := filepath.Join(t.TempDir(), "page")
		args := append(append([]string{"render", "--dpi", "72"}, tc.flags...), "-o", prefix, src)
		var stdout, stderr bytes.Buffer
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%q: exit %d, stderr %q", args, rc, stderr.String())
		}
		f, err := os.Open(prefix + "-1.png")
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		r, _, _, _ := img.At(20, 20).RGBA()
		if cyan := r < 0x8000; cyan != tc.cyan {
			t.Errorf("%q: cyan kept %v, want %v", args, cyan, tc.cyan)
		}
	}
}
