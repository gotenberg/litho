package mutations

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestEmbeddedFilesArrayEncodesKeys pins that a name-tree key reaches the file
// in an encoding a reader can decode.
//
// The key used to be the Go string cast straight to a PDF string, so a
// non-ASCII name landed as raw UTF-8: pikepdf showed an attachment named
// "Café СУВОРОВ.txt" as "CafÃ© Ð¡Ð£Ð™ÐžÐ€ÐžÐ™.txt". ISO 32000-1, 7.9.2.2 gives a
// text string two forms, PDFDocEncoding or UTF-16BE with a byte order mark, and
// raw UTF-8 is neither.
func TestEmbeddedFilesArrayEncodesKeys(t *testing.T) {
	t.Parallel()

	arr := embeddedFilesArray([]EFEntry{
		{Name: "Café.txt", Ref: ast.PdfIndirectRef{ObjectNum: 5}},
	})
	if len(arr) != 2 {
		t.Fatalf("array holds %d entries, want a key and a value", len(arr))
	}
	key, ok := arr[0].(ast.PdfString)
	if !ok {
		t.Fatalf("the key is %T, want a string", arr[0])
	}

	// PDFDocEncoding covers this name, so it stays one byte per character.
	want := string([]byte{'C', 'a', 'f', 0xE9, '.', 't', 'x', 't'})
	if string(key) != want {
		t.Errorf("key = % x, want % x", key, want)
	}
	if string(key) == "Café.txt" {
		t.Error("the key reached the file as raw UTF-8")
	}
}

// TestEmbeddedFilesArrayUsesUTF16WhenNeeded pins the other form: a name
// PDFDocEncoding cannot express is written as UTF-16BE with a BOM.
func TestEmbeddedFilesArrayUsesUTF16WhenNeeded(t *testing.T) {
	t.Parallel()

	arr := embeddedFilesArray([]EFEntry{
		{Name: "СУВОРОВ.txt", Ref: ast.PdfIndirectRef{ObjectNum: 5}},
	})
	key, _ := arr[0].(ast.PdfString)
	if len(key) < 2 || key[0] != 0xFE || key[1] != 0xFF {
		t.Errorf("key = % x, want it to start with the UTF-16BE byte order mark", key)
	}
}

// TestEmbeddedFilesArraySortsByEncodedBytes pins that the tree is ordered the
// way a reader searches it.
//
// ISO 32000-2, 7.9.6 orders name-tree keys by BYTE VALUE. Sorting the Go
// strings and encoding afterwards would write an order that does not match the
// bytes in the file the moment a key stops being ASCII, and a reader pruning on
// /Limits would then miss entries.
func TestEmbeddedFilesArraySortsByEncodedBytes(t *testing.T) {
	t.Parallel()

	arr := embeddedFilesArray([]EFEntry{
		{Name: "zebra.txt", Ref: ast.PdfIndirectRef{ObjectNum: 1}},
		{Name: "alpha.txt", Ref: ast.PdfIndirectRef{ObjectNum: 2}},
		{Name: "Ünicode.txt", Ref: ast.PdfIndirectRef{ObjectNum: 3}},
	})

	var keys []string
	for i := 0; i+1 < len(arr); i += 2 {
		k, ok := arr[i].(ast.PdfString)
		if !ok {
			t.Fatalf("entry %d has key type %T", i, arr[i])
		}
		keys = append(keys, string(k))
	}
	for i := 1; i < len(keys); i++ {
		if keys[i-1] > keys[i] {
			t.Errorf("keys are not ordered by byte value: %q before %q", keys[i-1], keys[i])
		}
	}
}

// TestEmbeddedFilesArrayKeepsASCIIUnchanged is the control: the common case
// must produce exactly the bytes it always did.
func TestEmbeddedFilesArrayKeepsASCIIUnchanged(t *testing.T) {
	t.Parallel()

	arr := embeddedFilesArray([]EFEntry{
		{Name: "plain.txt", Ref: ast.PdfIndirectRef{ObjectNum: 5}},
	})
	if key, _ := arr[0].(ast.PdfString); string(key) != "plain.txt" {
		t.Errorf("key = %q, want %q", key, "plain.txt")
	}
}
