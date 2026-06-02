package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetadata_XMP_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runMetadata(context.Background(), []string{"--xmp", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	// The minimal PDF carries an Info dict but no XMP packet.
	if !strings.Contains(stdout.String(), "(no XMP packet)") {
		t.Errorf("expected \"(no XMP packet)\", got: %s", stdout.String())
	}
}

func TestInfo_Permissions_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runInfo(context.Background(), []string{"--permissions", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	out := stdout.String()
	// An unencrypted document grants every permission.
	if !strings.Contains(out, "Permissions:") || !strings.Contains(out, "Print:              true") {
		t.Errorf("expected granted permissions, got: %s", out)
	}
}

func TestBookmarks_Generate_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	var stdout, stderr bytes.Buffer
	rc := runBookmarks(context.Background(), []string{"--generate", path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
}

func TestSetBookmarks_Generate_MinimalPDF(t *testing.T) {
	path := writeTempPDF(t, minimalPDF())
	out := filepath.Join(t.TempDir(), "out.pdf")
	var stdout, stderr bytes.Buffer
	rc := runSetBookmarks(context.Background(), []string{"--generate", "-o", out, path}, &stdout, &stderr)
	if rc != exitOK {
		t.Fatalf("exit = %d, want %d. stderr: %s", rc, exitOK, stderr.String())
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Size() == 0 {
		t.Error("expected a non-empty output")
	}
}
