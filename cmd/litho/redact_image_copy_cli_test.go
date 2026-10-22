package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// sharedImageCLIPDF builds a one-page document painting one 4x4 DeviceGray
// image twice, as a 100pt square at x=100 and at x=300, y=500.
func sharedImageCLIPDF() []byte {
	body := "q 100 0 0 100 100 500 cm /Im1 Do Q q 100 0 0 100 300 500 cm /Im1 Do Q"
	samples := string(bytes.Repeat([]byte{0x80}, 16))
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray " +
		"/BitsPerComponent 8 /Length 16 >>\nstream\n" + samples + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
}

func TestRedact_ReportsAnImageBlankedInACopy(t *testing.T) {
	path := writeTempPDF(t, sharedImageCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,490,120,120", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), "warning: page 1 blanked image(s) [Im1] in a copy") {
		t.Errorf("stderr = %q, want a warning naming the copied image", stderr.String())
	}
	var result redactResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if got := result.ImagesBlankedInCopy[1]; len(got) != 1 || got[0] != "Im1" {
		t.Errorf("imagesBlankedInCopy = %v, want Im1 on page 1", result.ImagesBlankedInCopy)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no output written: %v", err)
	}
}

// sharedFormImageCLIPDF builds a one-page document drawing one form twice,
// at x=100 and at x=300, y=500; the form paints a 4x4 DeviceGray image over
// its 100pt square.
func sharedFormImageCLIPDF() []byte {
	body := "q 1 0 0 1 100 500 cm /Fm1 Do Q q 1 0 0 1 300 500 cm /Fm1 Do Q"
	form := "q 100 0 0 100 0 0 cm /Im1 Do Q"
	samples := string(bytes.Repeat([]byte{0x80}, 16))
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /XObject << /Fm1 6 0 R >> >> /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image /Width 4 /Height 4 /ColorSpace /DeviceGray " +
		"/BitsPerComponent 8 /Length 16 >>\nstream\n" + samples + "\nendstream\nendobj\n" +
		"6 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Im1 5 0 R >> >> " +
		"/Length " + strconv.Itoa(len(form)) + " >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

func TestRedact_ReportsAFormCopiedForAnImage(t *testing.T) {
	path := writeTempPDF(t, sharedFormImageCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,490,120,120", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	var result redactResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if got := result.ImagesBlankedInCopy[1]; len(got) != 1 || got[0] != "Im1" {
		t.Errorf("imagesBlankedInCopy = %v, want Im1 on page 1", result.ImagesBlankedInCopy)
	}
	if got := result.FormsCopied[1]; got != 1 {
		t.Errorf("formsCopied = %v, want one form on page 1", result.FormsCopied)
	}

	stdout.Reset()
	stderr.Reset()
	rc = run(context.Background(),
		[]string{"redact", "-region", "1:90,490,120,120", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Page 1 draws 1 copied form(s) holding images blanked in a copy") {
		t.Errorf("stdout = %q, want the copied form counted", stdout.String())
	}
}

func TestRedact_StrictRefusesASharedImage(t *testing.T) {
	path := writeTempPDF(t, sharedImageCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,490,120,120", "--strict", "-o", out, path}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), "blanked image is also painted") {
		t.Errorf("stderr = %q, want the shared image named", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused redaction left %s behind (stat err %v)", out, err)
	}
}
