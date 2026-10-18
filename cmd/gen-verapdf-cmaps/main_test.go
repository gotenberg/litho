package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"go/parser"
	"go/token"
	"io"
	"strings"
	"testing"
)

func TestVerify(t *testing.T) {
	t.Parallel()

	data := []byte("1 begincidrange\n<0000> <00ff> 0\nendcidrange\n")
	sum := sha256.Sum256(data)
	r := resource{"Test-CMap", hex.EncodeToString(sum[:])}
	if err := verify(data, r); err != nil {
		t.Errorf("verify on the pinned bytes: %v", err)
	}
	if err := verify(append(data, ' '), r); !errors.Is(err, errDigestMismatch) {
		t.Errorf("verify on changed bytes = %v, want errDigestMismatch", err)
	}
}

// TestCompress pins that the gzip copy round-trips and depends on its input
// alone, so regenerating from the pinned commit rewrites identical files.
func TestCompress(t *testing.T) {
	t.Parallel()

	data := []byte(strings.Repeat("<0000> <00ff> 0\n", 100))
	a, err := compress(data)
	if err != nil {
		t.Fatal(err)
	}
	b, err := compress(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two compressions of the same input differ")
	}
	zr, err := gzip.NewReader(bytes.NewReader(a))
	if err != nil {
		t.Fatal(err)
	}
	back, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(back, data) {
		t.Error("the gzip copy does not decompress to its input")
	}
}

func TestGenerate(t *testing.T) {
	t.Parallel()

	code, err := generate()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if _, err := parser.ParseFile(token.NewFileSet(), "gen.go", code, 0); err != nil {
		t.Fatalf("generated code does not parse: %v", err)
	}
	for _, r := range resources {
		if !bytes.Contains(code, []byte(r.SHA256)) {
			t.Errorf("generated code misses the digest of %s", r.Name)
		}
	}
	if !bytes.Contains(code, []byte(veraPDFCommit)) {
		t.Error("generated code does not name the pinned commit")
	}
}
