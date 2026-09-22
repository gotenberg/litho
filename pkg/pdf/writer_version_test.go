package pdf

import (
	"bytes"
	"context"
	"testing"
)

// Object streams and cross-reference streams are PDF 1.5 constructs, so an
// output carrying them declares at least that version. The rewrite engine's
// own tests pin this, but nothing at the PUBLIC surface did: every pkg/pdf
// test checks that a packed file parses and reopens, never what it says about
// itself. A refactor could drop the floor and this package would stay green,
// which is the same blind spot that let the original defect ship.

// pdfHeader returns the leading version token, for assertions that want to
// report what a file actually declared.
func pdfHeader(b []byte) string {
	return string(b[:min(len(b), 8)])
}

func TestWriter_WithObjectStreams_DeclaresPDF15(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	// Without this the whole test is vacuous: a fixture already declaring 1.5
	// or later would satisfy the assertion below without the floor ever
	// running.
	if !bytes.HasPrefix(data, []byte("%PDF-1.4")) {
		t.Fatalf("fixture declares %q, want %%PDF-1.4; a raise to 1.5 would not be observable",
			pdfHeader(data))
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(ctx, &out, WithFullRewrite(), WithObjectStreams()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	// The second half of the precondition: assert packing HAPPENED, or the
	// version check below would pass on a file that simply never packed.
	if !bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Fatal("nothing was packed, so this proves nothing about the declared version")
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-1.5")) {
		t.Errorf("packed output declares %q, want %%PDF-1.5: it carries PDF 1.5 constructs",
			pdfHeader(out.Bytes()))
	}
}

// TestWriter_WithoutObjectStreams_KeepsTheSourceVersion is the control that
// separates a floor from a blanket bump. A full rewrite that packs nothing
// needs nothing from 1.5 and must leave the declaration alone.
func TestWriter_WithoutObjectStreams_KeepsTheSourceVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(ctx, &out, WithFullRewrite()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if bytes.Contains(out.Bytes(), []byte(objStmMarker)) {
		t.Fatal("the unpacked control packed after all; it no longer controls for anything")
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-1.4")) {
		t.Errorf("a rewrite that packed nothing declares %q, want the source's %%PDF-1.4",
			pdfHeader(out.Bytes()))
	}
}

// TestWriter_WithXRefStream_DeclaresPDF15 covers the half that predates
// compaction. [WithXRefStream] documented "PDF 1.5+" long before anything
// enforced it, and a caller selecting one directly gets the same construct.
func TestWriter_WithXRefStream_DeclaresPDF15(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var out bytes.Buffer
	if err := NewWriter(r).WriteTo(ctx, &out, WithFullRewrite(), WithXRefStream()); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	if !bytes.Contains(out.Bytes(), []byte("/Type /XRef")) {
		t.Fatal("no cross-reference stream in the output, so this proves nothing")
	}
	if !bytes.HasPrefix(out.Bytes(), []byte("%PDF-1.5")) {
		t.Errorf("cross-reference-stream output declares %q, want %%PDF-1.5",
			pdfHeader(out.Bytes()))
	}
}
