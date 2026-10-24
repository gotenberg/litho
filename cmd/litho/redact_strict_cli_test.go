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

// twoPageSecretCLIPDF builds a two-page document whose pages both draw
// "SECRET" at y=700, page 1 also "KEEP" at y=600, so a region on page 1 leaves
// a copy on page 2.
func twoPageSecretCLIPDF() []byte {
	one := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET"
	two := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R 7 0 R] /Count 2 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(one)) + " >>\nstream\n" + one + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources 5 0 R /Contents 8 0 R >>\nendobj\n" +
		"8 0 obj\n<< /Length " + strconv.Itoa(len(two)) + " >>\nstream\n" + two + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

func TestRedact_WarnsAboutACopyOnAnotherPage(t *testing.T) {
	path := writeTempPDF(t, twoPageSecretCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), `warning: a copy of "SECRET" remains on page(s) [2]`) {
		t.Errorf("stderr = %q, want a warning naming page 2", stderr.String())
	}
	var result redactResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if len(result.CopiesOnOtherPages) != 1 || result.CopiesOnOtherPages[0].Token != "SECRET" ||
		len(result.CopiesOnOtherPages[0].Pages) != 1 || result.CopiesOnOtherPages[0].Pages[0] != 2 {
		t.Errorf("copiesOnOtherPages = %+v, want SECRET on page 2", result.CopiesOnOtherPages)
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("no output written: %v", err)
	}
}

func TestRedact_StrictRefusesACopyOnAnotherPage(t *testing.T) {
	path := writeTempPDF(t, twoPageSecretCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--strict", "-o", out, path}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit %d, want %d; stderr: %s", rc, exitError, stderr.String())
	}
	if !strings.Contains(stderr.String(), `copy of "SECRET" survives in object 8`) {
		t.Errorf("stderr = %q, want the surviving copy named", stderr.String())
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Errorf("a refused redaction left %s behind (stat err %v)", out, err)
	}
}

// samePageSecretCLIPDF builds a one-page document drawing "SECRET" at y=700
// and again, visibly, at y=500, so a region over the first leaves the second.
func samePageSecretCLIPDF() []byte {
	one := "BT /F1 12 Tf 100 700 Td (SECRET) Tj ET BT /F1 12 Tf 100 500 Td (SECRET) Tj ET"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(one)) + " >>\nstream\n" + one + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
}

func TestRedact_WarnsAboutAVisibleCopyOnTheRedactedPage(t *testing.T) {
	path := writeTempPDF(t, samePageSecretCLIPDF())
	out := filepath.Join(t.TempDir(), "redacted.pdf")

	var stdout, stderr bytes.Buffer
	rc := run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "-f", "json", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit %d, stderr: %s", rc, stderr.String())
	}
	if !strings.Contains(stderr.String(), `warning: page 1 still shows "SECRET" outside the regions`) {
		t.Errorf("stderr = %q, want a warning naming page 1", stderr.String())
	}
	var result redactResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decoding the report: %v", err)
	}
	if len(result.CopiesOnRedactedPages) != 1 || result.CopiesOnRedactedPages[0].Token != "SECRET" ||
		result.CopiesOnRedactedPages[0].Page != 1 || len(result.CopiesOnRedactedPages[0].Boxes) != 1 {
		t.Errorf("copiesOnRedactedPages = %+v, want SECRET on page 1 with one box", result.CopiesOnRedactedPages)
	}

	strictOut := filepath.Join(t.TempDir(), "strict.pdf")
	rc = run(context.Background(),
		[]string{"redact", "-region", "1:90,690,210,40", "--strict", "-o", strictOut, path}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("--strict exit %d, want %d", rc, exitError)
	}
	if _, err := os.Stat(strictOut); !os.IsNotExist(err) {
		t.Errorf("a refused redaction left %s behind (stat err %v)", strictOut, err)
	}
}
