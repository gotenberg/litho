package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestGeo_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runGeo(context.Background(), []string{path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
}

func TestGeo_JSON(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runGeo(context.Background(), []string{"-f", "json", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"file"`) {
		t.Fatalf("expected JSON file field, got: %s", stdout.String())
	}
}
