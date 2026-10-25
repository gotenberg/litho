package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedact_ClipAnnotationsReachesTheOption pins --clip-annotations: a
// borderless link reaching past the region is refused without it, and with it
// the link is cut to the part outside and reported in clippedAnnotations.
func TestRedact_ClipAnnotationsReachesTheOption(t *testing.T) {
	src := redactableCLIBodyPDF("BT /F1 12 Tf 100 700 Td (SECRET) Tj ET")
	src = bytes.Replace(src, []byte("/Contents 6 0 R >>"), []byte("/Contents 6 0 R /Annots [7 0 R] >>"), 1)
	src = bytes.Replace(src, []byte("trailer"), []byte("7 0 obj\n<< /Type /Annot /Subtype /Link /Rect [100 695 400 720] /Border [0 0 0] >>\nendobj\ntrailer"), 1)
	path := writeTempPDF(t, src)
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "-o", out, path}, &stdout, &stderr); rc == exitOK {
		t.Fatal("a link reaching past the region was accepted without --clip-annotations")
	}
	if !strings.Contains(stderr.String(), "annotation reaches outside the region") {
		t.Errorf("stderr = %q, want the annotation refusal", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--clip-annotations", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	var report struct {
		ClippedAnnotations []int `json:"clippedAnnotations"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decoding the report: %v\n%s", err, stdout.String())
	}
	if len(report.ClippedAnnotations) != 1 || report.ClippedAnnotations[0] != 7 {
		t.Errorf("clippedAnnotations = %v, want [7]", report.ClippedAnnotations)
	}
}
