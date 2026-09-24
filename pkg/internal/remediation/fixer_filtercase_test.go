package remediation

import (
	"bytes"
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// flateBody returns genuine zlib bytes, the shape a correctly spelled
// /FlateDecode stream carries.
func flateBody(t *testing.T, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write([]byte(payload)); err != nil {
		t.Fatalf("zlib write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zlib close: %v", err)
	}
	return buf.Bytes()
}

func pendingStream(t *testing.T, alloc *writer.ObjAllocator, objNum int) (ast.PdfStream, bool) {
	t.Helper()
	obj, ok := alloc.Pending()[objNum]
	if !ok {
		return ast.PdfStream{}, false
	}
	st, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("object %d: got %T, want ast.PdfStream", objNum, obj)
	}
	return st, true
}

// TestFixFilterNameCase_RenamesWhenTheBytesActuallyDecode covers the repair.
//
// A stream may carry `/Filter /Flatedecode` over a genuinely Flate-compressed
// body. PDF names are case-sensitive, so that is "a filter not listed in Table
// 6".
//
// # Corpus evidence
//
//   - verapdf 6-1-6-2-t01-fail-b: carries `/Filter /Flatedecode` on object 11
//     with a genuinely Flate-compressed body.
func TestFixFilterNameCase_RenamesWhenTheBytesActuallyDecode(t *testing.T) {
	body := flateBody(t, "hello optional content")
	dict := ast.PdfDict{
		"Filter": ast.PdfName("Flatedecode"),
		"Length": ast.PdfInt(len(body)),
	}
	alloc := writer.NewObjAllocator(20)

	desc := fixFilterNameCase(context.Background(), alloc, 11, ast.NewStream(dict, body), dict["Filter"])
	if desc == "" {
		t.Fatal("a case-wrong filter whose bytes decode must be repaired")
	}

	st, ok := pendingStream(t, alloc, 11)
	if !ok {
		t.Fatal("expected a pending stream for object 11")
	}
	if got := st.Dict["Filter"]; got != ast.PdfName("FlateDecode") {
		t.Errorf("/Filter = %v, want /FlateDecode", got)
	}
	// The bytes must carry across untouched: only the name was wrong, so
	// /Length stays correct.
	raw, err := st.Bytes(context.Background())
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !bytes.Equal(raw, body) {
		t.Errorf("raw bytes changed: got %d bytes, want the original %d", len(raw), len(body))
	}
	if got := st.Dict["Length"]; got != ast.PdfInt(len(body)) {
		t.Errorf("/Length = %v, want %d", got, len(body))
	}
}

// TestFixFilterNameCase_LeavesItAloneWhenTheBytesDoNotDecode is the branch the
// gate CANNOT EXERCISE.
//
// A name that merely looks like a Table 6 spelling proves nothing about the
// bytes: renaming on the name alone would declare a stream to be Flate that is
// not, and the writer would emit a file no reader can inflate.
//
// # Corpus evidence
//
//   - All corpora: exactly one misspelled-filter stream exists corpus-wide and
//     its bytes do decode, so the CORPUS CANNOT EXERCISE this branch and the
//     gate can only ever confirm the positive path.
func TestFixFilterNameCase_LeavesItAloneWhenTheBytesDoNotDecode(t *testing.T) {
	body := []byte("this is definitely not zlib data, not even close")
	dict := ast.PdfDict{
		"Filter": ast.PdfName("Flatedecode"),
		"Length": ast.PdfInt(len(body)),
	}
	alloc := writer.NewObjAllocator(20)

	desc := fixFilterNameCase(context.Background(), alloc, 11, ast.NewStream(dict, body), dict["Filter"])
	if desc != "" {
		t.Errorf("must not rename when the bytes do not decode: %q", desc)
	}
	if _, ok := pendingStream(t, alloc, 11); ok {
		t.Error("nothing may be written when the rename is refused")
	}
}

// TestCanonicalFilterSpelling pins the table's scope in both directions.
//
// A canonical name must yield false, which is what keeps this branch away from
// the LZWDecode streams fixLZW re-encodes instead: the two repairs answer the
// same clause and must not both claim one stream.
func TestCanonicalFilterSpelling(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in    ast.PdfName
		want  ast.PdfName
		found bool
	}{
		{"Flatedecode", "FlateDecode", true},
		{"FLATEDECODE", "FlateDecode", true},
		{"flatedecode", "FlateDecode", true},
		{"lzwdecode", "LZWDecode", true},
		{"FlateDecode", "", false}, // already canonical
		{"LZWDecode", "", false},   // already canonical: fixLZW's own path
		{"Crypt", "", false},       // already canonical
		{"JPXDecode", "", false},   // already canonical
		{"NotAFilter", "", false},  // not a Table 6 name in any casing
		{"Fl", "", false},          // inline-image abbreviation, not a stream name
	} {
		got, found := canonicalFilterSpelling(tc.in)
		if found != tc.found || got != tc.want {
			t.Errorf("canonicalFilterSpelling(%q) = (%q, %v), want (%q, %v)",
				tc.in, got, found, tc.want, tc.found)
		}
	}
}

// TestFixFilterNameCase_IgnoresArrayValuedFilter records a deliberate limit.
//
// An array /Filter would need every element verified in chain order, and the
// gate could not falsify that code.
//
// # Corpus evidence
//
//   - All corpora: no file carries an array-valued misspelled /Filter.
func TestFixFilterNameCase_IgnoresArrayValuedFilter(t *testing.T) {
	body := flateBody(t, "payload")
	dict := ast.PdfDict{
		"Filter": ast.PdfArray{ast.PdfName("Flatedecode")},
		"Length": ast.PdfInt(len(body)),
	}
	alloc := writer.NewObjAllocator(20)

	if desc := fixFilterNameCase(context.Background(), alloc, 11, ast.NewStream(dict, body), dict["Filter"]); desc != "" {
		t.Errorf("an array-valued /Filter is out of scope, got %q", desc)
	}
	if _, ok := pendingStream(t, alloc, 11); ok {
		t.Error("nothing may be written for an array-valued /Filter")
	}
}
