package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// transparentCLIPDF is one page with a half-transparent square over a
// backdrop and text over both.
func transparentCLIPDF() []byte {
	content := "0 0 1 rg 0 0 200 100 re f q /Half gs 1 0 0 rg 20 40 80 80 re f Q BT /F1 14 Tf 0 g 20 150 Td (Kept as text) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> /ExtGState << /Half 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /ExtGState /ca 0.5 >>",
	}
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

func TestFlattenTransparency_CompositesAndKeepsText(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, transparentCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"flatten-transparency", "--dpi", "150", "-f", "json", "-o", out, src}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d: %s", rc, stderr.String())
	}
	var report flattenTransparencyResult
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("report %q: %v", stdout.String(), err)
	}
	if len(report.Pages) != 1 || report.Pages[0].Regions != 1 || report.Pages[0].DPI != 150 || report.Pages[0].Rasterized != 1 {
		t.Errorf("report %+v", report)
	}

	stdout.Reset()
	if rc := run(context.Background(), []string{"text", out}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("text exit %d: %s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Kept as text") {
		t.Errorf("text %q lost the page's text", stdout.String())
	}
}

func TestFlattenTransparency_RefusesABadJPEGQuality(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, transparentCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"flatten-transparency", "--jpeg-quality", "101", "-o", out, src}, &stdout, &stderr); rc != exitError {
		t.Fatalf("exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "JPEG quality 101") {
		t.Errorf("stderr %q", stderr.String())
	}
}

func TestFlattenTransparency_RefusesABadResolution(t *testing.T) {
	t.Parallel()
	src := writeTempPDF(t, transparentCLIPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(), []string{"flatten-transparency", "--dpi", "0", "-o", out, src}, &stdout, &stderr); rc != exitError {
		t.Fatalf("exit %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "invalid transparency flattening options") {
		t.Errorf("stderr %q", stderr.String())
	}
}

// TestFlattenTransparency_PrintedAnnotations pins --printed-annotations: an
// opaque printed annotation, a square with no appearance that litho
// synthesises from its colour (ISO 32000-1, 12.5.6.8), is drawn into the page
// only with the flag.
func TestFlattenTransparency_PrintedAnnotations(t *testing.T) {
	t.Parallel()
	data := bytes.Replace(transparentCLIPDF(), []byte("/Contents 4 0 R "),
		[]byte("/Contents 4 0 R /Annots [<< /Type /Annot /Subtype /Square /Rect [150 150 190 190] /F 4 /C [1 0 0] >>] "), 1)
	src := writeTempPDF(t, data)
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"--printed-annotations"}, true},
	} {
		out := filepath.Join(t.TempDir(), "out.pdf")
		var stdout, stderr bytes.Buffer
		args := append(append([]string{"flatten-transparency"}, tc.args...), "-o", out, src)
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%v: exit %d: %s", tc.args, rc, stderr.String())
		}
		if got := strings.Contains(stdout.String(), "Drew 1 annotation(s) into the page content"); got != tc.want {
			t.Errorf("%v: report %q, drew an annotation %v, want %v", tc.args, stdout.String(), got, tc.want)
		}
	}
}
