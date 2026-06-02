package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempCII(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "cii.xml")
	const cii = `<?xml version="1.0" encoding="UTF-8"?>` +
		`<rsm:CrossIndustryInvoice xmlns:rsm="urn:test"/>`
	if err := os.WriteFile(path, []byte(cii), 0o644); err != nil {
		t.Fatalf("write temp CII: %v", err)
	}
	return path
}

func TestValidate_FacturX_NonInvoice(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"validate", "--profile", "Factur-X", path}, &stdout, &stderr)
	if rc != exitValidationFail {
		t.Fatalf("exit code: got %d, want %d. stderr: %s", rc, exitValidationFail, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "violation(s)") || !strings.Contains(out, "[fatal]") {
		t.Errorf("expected fatal violations for a non-invoice PDF, got: %s", out)
	}
}

func TestValidate_ZUGFeRD21_NonInvoice(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"validate", "--profile", "ZUGFeRD-2.1", path}, &stdout, &stderr)
	if rc != exitValidationFail {
		t.Fatalf("exit code: got %d, want %d. stderr: %s", rc, exitValidationFail, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ZUGFeRD-2.1:") {
		t.Errorf("expected ZUGFeRD-2.1 result line, got: %s", stdout.String())
	}
}

func TestValidate_UnknownProfile_ListsFacturX(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"validate", "--profile", "NOPE", path}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit code: got %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "Factur-X") {
		t.Errorf("expected Factur-X in the valid-profiles list, got: %s", stderr.String())
	}
}

func TestRemediate_FacturX_MissingInvoice(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{"remediate", "--profile", "Factur-X", "-o", out, path}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit code: got %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "--invoice") {
		t.Errorf("expected usage mentioning --invoice, got: %s", stderr.String())
	}
}

func TestRemediate_FacturX_BadLevel(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{
		"remediate", "--profile", "Factur-X",
		"--invoice", writeTempCII(t), "--level", "BANANA",
		"-o", out, path,
	}, &stdout, &stderr)
	if rc != exitError {
		t.Fatalf("exit code: got %d, want %d", rc, exitError)
	}
	if !strings.Contains(stderr.String(), "invalid Factur-X level") {
		t.Errorf("expected invalid-level error, got: %s", stderr.String())
	}
}

func TestRemediate_FacturX_Success(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	rc := run(context.Background(), []string{
		"remediate", "--profile", "Factur-X",
		"--invoice", writeTempCII(t), "--level", "EN 16931",
		"-o", out, path,
	}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit code: got %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected a non-empty remediated output")
	}
}
