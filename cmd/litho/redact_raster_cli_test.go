package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRedact_RasterFallbackLiftsAShadingRefusal pins --raster-fallback: a
// region a shading paint reaches is refused without it, and with it the
// shading is rasterised, reported in rasterisedUnits, and the output written.
func TestRedact_RasterFallbackLiftsAShadingRefusal(t *testing.T) {
	path := writeTempPDF(t, redactableCLIBodyPDF("q 0 0 612 792 re W n /Sh0 sh Q BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"))
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "-o", out, path}, &stdout, &stderr); rc == exitOK {
		t.Fatal("a region a shading reaches was accepted without --raster-fallback")
	}
	if !strings.Contains(stderr.String(), "shading") {
		t.Errorf("stderr = %q, want the shading refusal", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--raster-fallback", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	var report struct {
		RemovedText     []string `json:"removedText"`
		RasterisedUnits int      `json:"rasterisedUnits"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decoding the report: %v\n%s", err, stdout.String())
	}
	if report.RasterisedUnits != 1 {
		t.Errorf("rasterisedUnits = %d, want 1", report.RasterisedUnits)
	}
	written, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	if bytes.Contains(written, []byte("SECRET")) {
		t.Error("the redacted word is still in the written file")
	}
}

// TestRedact_JPEGQualityReachesTheOption pins --jpeg-quality: a quality out
// of range is refused before any output is written, and one in range runs.
func TestRedact_JPEGQualityReachesTheOption(t *testing.T) {
	path := writeTempPDF(t, redactableCLIBodyPDF("q 0 0 612 792 re W n /Sh0 sh Q BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"))
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	if rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--raster-fallback", "--jpeg-quality", "101", "-o", out, path}, &stdout, &stderr); rc == exitOK {
		t.Fatal("--jpeg-quality 101 was accepted")
	}
	if !strings.Contains(stderr.String(), "JPEG quality 101") {
		t.Errorf("stderr = %q, want the quality named", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("an output was written for a refused quality: %v", err)
	}

	stdout.Reset()
	stderr.Reset()
	if rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--raster-fallback", "--jpeg-quality", "80", "-o", out, path}, &stdout, &stderr); rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
}
