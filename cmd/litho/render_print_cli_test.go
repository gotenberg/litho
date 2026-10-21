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

// TestRender_Print pins --print: the annotation hidden on screen (/F 36,
// Print and NoView) draws only when the page is drawn for print.
func TestRender_Print(t *testing.T) {
	const data = "1 0 0 rg 0 0 20 20 re f"
	src := writeTempPDF(t, renderPrintPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 40 40] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Square /Rect [10 10 30 30] /F 36 /AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 20 20] /Length %d >>\nstream\n%s\nendstream", len(data), data),
	}))
	for _, tc := range []struct {
		flags []string
		red   bool
	}{
		{nil, false},
		{[]string{"--print"}, true},
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
		_, g, _, _ := img.At(20, 20).RGBA()
		if red := g < 0x8000; red != tc.red {
			t.Errorf("%q: annotation drawn %v, want %v", args, red, tc.red)
		}
	}
}

// renderPrintPDF serialises objs as objects 1 to n with a classic xref table.
func renderPrintPDF(objs []string) []byte {
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}
