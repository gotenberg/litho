package document

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// plainObjStm builds an unfiltered object stream (ISO 32000-1, 7.5.7) whose
// decoded bytes are header followed by body, with /First at the end of header.
func plainObjStm(n int, header, body string) ast.PdfStream {
	return ast.NewStream(ast.PdfDict{
		"Type":  ast.PdfName("ObjStm"),
		"N":     ast.PdfInt(n),
		"First": ast.PdfInt(len(header)),
	}, []byte(header+body))
}

// decodeNums decodes stream and returns the objects it holds keyed by number.
func decodeNums(t *testing.T, stream ast.PdfStream) map[int]ast.PdfObject {
	t.Helper()
	doc := &Document{Logger: NopLogger()}
	objs, err := doc.DecodeObjectStream(context.Background(), 8, stream)
	if err != nil {
		t.Fatalf("DecodeObjectStream: %v", err)
	}
	got := make(map[int]ast.PdfObject, len(objs))
	for _, o := range objs {
		got[o.Num] = o.Object
	}
	return got
}

func TestDecodeObjectStream_ReadsEveryObject(t *testing.T) {
	t.Parallel()

	got := decodeNums(t, plainObjStm(3, "3 0 6 11 7 22 ", "<< /A 1 >> << /B 2 >> [/C 3]"))
	want := map[int]ast.PdfObject{
		3: ast.PdfDict{"A": ast.PdfInt(1)},
		6: ast.PdfDict{"B": ast.PdfInt(2)},
		7: ast.PdfArray{ast.PdfName("C"), ast.PdfInt(3)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestDecodeObjectStream_HeaderOutOfOrder covers a header that breaks 7.5.7's
// "the offsets shall be in increasing order": each object is still found, and
// still read within its own bytes.
func TestDecodeObjectStream_HeaderOutOfOrder(t *testing.T) {
	t.Parallel()

	got := decodeNums(t, plainObjStm(3, "7 22 3 0 6 11 ", "<< /A 1 >> << /B 2 >> [/C 3]"))
	if len(got) != 3 || !reflect.DeepEqual(got[6], ast.PdfDict{"B": ast.PdfInt(2)}) {
		t.Errorf("got %v, want objects 3, 6 and 7 with 6 = << /B 2 >>", got)
	}
}

// TestDecodeObjectStream_ObjectEndsAtNextOffset pins that an object is parsed
// only up to the next offset in the header. Object 1 is declared to span
// "[1 [", so the array the unbounded bytes would give it is not what it holds.
func TestDecodeObjectStream_ObjectEndsAtNextOffset(t *testing.T) {
	t.Parallel()

	got := decodeNums(t, plainObjStm(2, "1 0 2 4 ", "[1 [2] 3]"))
	if reflect.DeepEqual(got[1], ast.PdfArray{ast.PdfInt(1), ast.PdfArray{ast.PdfInt(2)}, ast.PdfInt(3)}) {
		t.Errorf("object 1 = %v: it was read past the offset where object 2 begins", got[1])
	}
	if got[2] != ast.PdfInt(2) {
		t.Errorf("object 2 = %v, want 2", got[2])
	}
}

// TestDecodeObjectStream_SharedOffset covers a header naming one offset for
// several objects: each is reported with the value stored there.
func TestDecodeObjectStream_SharedOffset(t *testing.T) {
	t.Parallel()

	header := strings.Repeat("5 0 ", 1000) + "9 0 "
	got := decodeNums(t, plainObjStm(1001, header, "<< /Big true >>"))
	want := ast.PdfDict{"Big": ast.PdfBool(true)}
	if !reflect.DeepEqual(got[5], want) || !reflect.DeepEqual(got[9], want) {
		t.Errorf("got %v, want objects 5 and 9 both %v", got, want)
	}
}

// TestDecodeObjectStream_UntrustedOffsets covers /First and header offsets
// that name bytes the stream does not hold: /First is refused, and an offset
// outside the objects is skipped without costing its neighbours.
func TestDecodeObjectStream_UntrustedOffsets(t *testing.T) {
	t.Parallel()

	doc := &Document{Logger: NopLogger()}
	ctx := context.Background()
	for _, first := range []int{-1, 1 << 30} {
		stream := ast.NewStream(ast.PdfDict{
			"Type": ast.PdfName("ObjStm"), "N": ast.PdfInt(1), "First": ast.PdfInt(first),
		}, []byte("1 0 null"))
		if _, err := doc.DecodeObjectStream(ctx, 8, stream); err == nil {
			t.Errorf("/First %d: want an error", first)
		}
	}

	got := decodeNums(t, plainObjStm(3, "1 999999 2 -4 3 0 ", "(kept)"))
	if len(got) != 1 || got[3] != ast.PdfString("kept") {
		t.Errorf("got %v, want only object 3 = (kept)", got)
	}
}
