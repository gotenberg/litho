package writer

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestSpoolReadsBackAndRemovesItsFile(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	s := NewSpool(dir)
	if entries, _ := os.ReadDir(dir); len(entries) != 0 { //nolint:errcheck // an unreadable dir fails the next check
		t.Fatalf("a new spool made %d files", len(entries))
	}
	a, b := []byte("first stream"), bytes.Repeat([]byte{7}, 1<<16)
	sa, err := s.Stream(ast.PdfDict{"K": ast.PdfInt(1)}, a)
	if err != nil {
		t.Fatal(err)
	}
	sb, err := s.Stream(ast.PdfDict{}, b)
	if err != nil {
		t.Fatal(err)
	}
	if s.Size() != int64(len(a)+len(b)) {
		t.Errorf("size %d, want %d", s.Size(), len(a)+len(b))
	}
	for _, c := range []struct {
		s    ast.PdfStream
		want []byte
	}{{sb, b}, {sa, a}, {sa, a}} {
		got, err := c.s.Bytes(ctx)
		if err != nil || !bytes.Equal(got, c.want) {
			t.Errorf("read back %d bytes (%v), want %d", len(got), err, len(c.want))
		}
	}
	if sa.Dict["K"] != ast.PdfInt(1) {
		t.Error("the dictionary is not kept")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 { //nolint:errcheck // an unreadable dir fails the check
		t.Fatalf("%d spool files, want 1", len(entries))
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 { //nolint:errcheck // an unreadable dir fails the check
		t.Errorf("%d files left after Close", len(entries))
	}
	if _, err := sa.Bytes(ctx); err == nil {
		t.Error("a stream reads after Close")
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestSpoolFailsWithoutItsDirectory(t *testing.T) {
	t.Parallel()
	s := NewSpool(t.TempDir() + "/missing")
	if _, err := s.Stream(ast.PdfDict{}, []byte("x")); err == nil {
		t.Error("a spool in a missing directory accepts a stream")
	}
	if err := s.Close(); err != nil {
		t.Errorf("Close of a spool without a file: %v", err)
	}
}
