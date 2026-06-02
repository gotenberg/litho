package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestModel3d_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runModel3d(context.Background(), []string{path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
}

func TestModel3d_JSON(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runModel3d(context.Background(), []string{"-f", "json", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"file"`) {
		t.Errorf("expected \"file\" in JSON output, got: %s", stdout.String())
	}
}
