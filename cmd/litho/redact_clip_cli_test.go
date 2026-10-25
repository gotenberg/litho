package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedact_ClipAnnotationsByDefault pins the annotation clipping: a
// borderless link reaching past the region is cut to the part outside and
// reported in clippedAnnotations, by default and with the deprecated
// --clip-annotations, and refused with --no-clip-annotations.
func TestRedact_ClipAnnotationsByDefault(t *testing.T) {
	src := redactableCLIBodyPDF("BT /F1 12 Tf 100 700 Td (SECRET) Tj ET")
	src = bytes.Replace(src, []byte("/Contents 6 0 R >>"), []byte("/Contents 6 0 R /Annots [7 0 R] >>"), 1)
	src = bytes.Replace(src, []byte("trailer"), []byte("7 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 400 720] /Border [0 0 0] >>\nendobj\ntrailer"), 1)
	path := writeTempPDF(t, src)
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--no-clip-annotations", "-o", out, path}, &stdout, &stderr); rc == exitOK {
		t.Fatal("a link reaching past the region was accepted with --no-clip-annotations")
	}
	if !strings.Contains(stderr.String(), "annotation reaches outside the region") {
		t.Errorf("stderr = %q, want the annotation refusal", stderr.String())
	}

	for _, flags := range [][]string{nil, {"--clip-annotations"}} {
		stdout.Reset()
		stderr.Reset()
		args := append(append([]string{"redact", "-region", "1:90,690,210,40"}, flags...), "-f", "json", "-o", out, path)
		if rc := run(context.Background(), args, &stdout, &stderr); rc != exitOK {
			t.Fatalf("%v: exit %d, stderr: %s", flags, rc, stderr.String())
		}
		var report struct {
			ClippedAnnotations []int `json:"clippedAnnotations"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
			t.Fatalf("%v: decoding the report: %v\n%s", flags, err, stdout.String())
		}
		if len(report.ClippedAnnotations) != 1 || report.ClippedAnnotations[0] != 7 {
			t.Errorf("%v: clippedAnnotations = %v, want [7]", flags, report.ClippedAnnotations)
		}
	}
}
