package annotation

import (
	"bytes"
	"compress/zlib"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// xfaTestDoc returns a document carrying only a logger. Nothing on the assembly
// path resolves an indirect reference, so the unexported fields stay zero.
func xfaTestDoc() *document.Document {
	return &document.Document{Logger: document.NopLogger()}
}

// xfaFlateStream returns a stream whose FlateDecode payload expands to n zero
// bytes, so a large decoded size costs almost nothing in the fixture. That
// asymmetry is the hazard under test, not a convenience: each entry decodes
// under its own cap, so the fixture buys decoded megabytes with input kilobytes
// exactly as a crafted file would.
func xfaFlateStream(t *testing.T, n int) ast.PdfStream {
	t.Helper()

	var comp bytes.Buffer
	zw := zlib.NewWriter(&comp)
	if _, err := zw.Write(make([]byte, n)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}

	return ast.NewStream(ast.PdfDict{
		"Length": ast.PdfInt(comp.Len()),
		"Filter": ast.PdfName("FlateDecode"),
	}, comp.Bytes())
}

// TestAssembleXFAArray_BoundsAccumulation pins maxXFADataSize.
//
// Nothing in the repository built an /XFA array before this file, so the
// accumulation was reached only through whole documents and its size was never
// asserted. Without the bound this call assembles every entry: measured at 20
// entries, 328 KB of input produced 160 MB, and the only limit on that is how
// many entries a file can name.
func TestAssembleXFAArray_BoundsAccumulation(t *testing.T) {
	const packets = 3
	const each = 8 << 20

	var arr ast.PdfArray
	for range packets {
		arr = append(arr, ast.PdfName("datasets"), xfaFlateStream(t, each))
	}

	got, err := assembleXFAArray(context.Background(), xfaTestDoc(), arr)
	if err == nil {
		t.Fatalf("assembled %d bytes with no error: %d packets of %d bytes must exceed the %d byte bound",
			len(got), packets, each, maxXFADataSize)
	}
	if got != nil {
		t.Errorf("returned %d bytes alongside the error: a refused assembly returns nothing, "+
			"so a caller cannot mistake a truncated form for a whole one", len(got))
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want it to name the limit it hit", err)
	}
}

// TestAssembleXFAArray_ConcatenatesPacketsInOrder is the control for the bound
// test above, which would pass just as well on a build that assembled nothing
// at all.
//
// It also pins the separator, or rather its absence. The page-content loop this
// one otherwise mirrors joins its streams with a newline; the XFA packets
// concatenate into a single XML document, so inserting one here would split a
// tag across the join.
func TestAssembleXFAArray_ConcatenatesPacketsInOrder(t *testing.T) {
	arr := ast.PdfArray{
		ast.PdfName("preamble"), ast.NewStream(ast.PdfDict{}, []byte("<xdp>")),
		ast.PdfName("datasets"), ast.NewStream(ast.PdfDict{}, []byte("<data/>")),
		ast.PdfName("postamble"), ast.NewStream(ast.PdfDict{}, []byte("</xdp>")),
	}

	got, err := assembleXFAArray(context.Background(), xfaTestDoc(), arr)
	if err != nil {
		t.Fatalf("assembleXFAArray: %v", err)
	}
	if want := "<xdp><data/></xdp>"; string(got) != want {
		t.Errorf("assembled %q, want %q", got, want)
	}
}

// TestAssembleXFAArray_SkipsUnusableEntries pins the tolerance the bound sits
// next to: an entry that does not resolve to a stream is skipped and the rest
// of the array still assembles.
func TestAssembleXFAArray_SkipsUnusableEntries(t *testing.T) {
	arr := ast.PdfArray{
		ast.PdfName("preamble"), ast.PdfName("not a stream"),
		ast.PdfName("datasets"), ast.NewStream(ast.PdfDict{}, []byte("<data/>")),
	}

	got, err := assembleXFAArray(context.Background(), xfaTestDoc(), arr)
	if err != nil {
		t.Fatalf("assembleXFAArray: %v", err)
	}
	if want := "<data/>"; string(got) != want {
		t.Errorf("assembled %q, want %q: an unusable entry is skipped, not fatal", got, want)
	}
}

// TestAssembleXFAArray_OddLengthArray pins the pairing guard. A trailing name
// with no stream after it must not index past the end.
func TestAssembleXFAArray_OddLengthArray(t *testing.T) {
	arr := ast.PdfArray{
		ast.PdfName("datasets"), ast.NewStream(ast.PdfDict{}, []byte("<data/>")),
		ast.PdfName("template"),
	}

	got, err := assembleXFAArray(context.Background(), xfaTestDoc(), arr)
	if err != nil {
		t.Fatalf("assembleXFAArray: %v", err)
	}
	if want := "<data/>"; string(got) != want {
		t.Errorf("assembled %q, want %q", got, want)
	}
}

// TestAssembleXFAXML_BoundsBothShapes pins that the bound is on the VALUE, not
// on how the file spells it.
//
// /XFA is either one stream or an array of them carrying the same payload, so a
// limit applied to only one shape is not a limit: a file that wanted more would
// write the other spelling. The single-stream case would otherwise fall back to
// the 256 MB per-stream decompression cap, sixteen times this bound.
func TestAssembleXFAXML_BoundsBothShapes(t *testing.T) {
	const oversized = 24 << 20

	for _, tc := range []struct {
		name string
		xfa  ast.PdfObject
	}{
		{"single stream", xfaFlateStream(t, oversized)},
		{"array of three", ast.PdfArray{
			ast.PdfName("preamble"), xfaFlateStream(t, oversized/3),
			ast.PdfName("datasets"), xfaFlateStream(t, oversized/3),
			ast.PdfName("template"), xfaFlateStream(t, oversized/3),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := assembleXFAXML(context.Background(), xfaTestDoc(), tc.xfa)
			if err == nil {
				t.Fatalf("assembled %d bytes with no error, want a refusal at %d bytes",
					len(got), maxXFADataSize)
			}
			if got != nil {
				t.Errorf("returned %d bytes alongside the error", len(got))
			}
		})
	}
}

// TestAssembleXFAXML_AcceptsARealisticForm is the control for the bound tests:
// they would all pass on a build that refused every XFA form outright.
//
// 3 MB is above the largest real form, so a bound that rejected this would be
// rejecting real documents.
//
// # Corpus evidence
//
//   - All corpora: the largest form assembles 2.34 MB.
func TestAssembleXFAXML_AcceptsARealisticForm(t *testing.T) {
	const realistic = 3 << 20

	got, err := assembleXFAXML(context.Background(), xfaTestDoc(), ast.PdfArray{
		ast.PdfName("datasets"), xfaFlateStream(t, realistic),
	})
	if err != nil {
		t.Fatalf("assembleXFAXML refused a %d byte form: %v", realistic, err)
	}
	if len(got) != realistic {
		t.Errorf("assembled %d bytes, want %d", len(got), realistic)
	}
}
