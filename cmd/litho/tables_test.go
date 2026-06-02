package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestTables_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runTables(context.Background(), []string{path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
}

func TestTables_JSON(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runTables(context.Background(), []string{"-f", "json", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"file"`) {
		t.Errorf("expected file field in JSON output, got: %s", stdout.String())
	}
}
