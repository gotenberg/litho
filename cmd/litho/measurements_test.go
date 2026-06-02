package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestMeasurements_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runMeasurements(context.Background(), []string{path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
}

func TestMeasurements_JSON(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runMeasurements(context.Background(), []string{"-f", "json", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"file"`) {
		t.Errorf("expected file field in JSON, got: %s", stdout.String())
	}
}
