package writer

import (
	"bytes"
	"context"
	"math/rand/v2"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// plainContent is a content stream long enough for FlateDecode to shrink it.
var plainContent = bytes.Repeat([]byte("BT /F1 12 Tf 72 700 Td (A line of text) Tj ET\n"), 40)

func TestCompressStreams_CompressesWhatCarriesNoFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	alloc := NewObjAllocator(0)
	content := alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(plainContent))}, plainContent))
	nullFilter := alloc.Alloc(ast.NewStream(ast.PdfDict{"Filter": ast.PdfNull{}}, plainContent))

	n, err := CompressStreams(ctx, alloc, StreamCompression{})
	if err != nil {
		t.Fatalf("CompressStreams: %v", err)
	}
	if n != 2 {
		t.Errorf("compressed %d streams, want 2", n)
	}
	for _, ref := range []ast.PdfIndirectRef{content, nullFilter} {
		s := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
		if f, _ := s.Dict["Filter"].(ast.PdfName); f != "FlateDecode" {
			t.Errorf("object %d: /Filter = %v, want FlateDecode", ref.ObjectNum, s.Dict["Filter"])
		}
		raw, err := s.Bytes(ctx)
		if err != nil {
			t.Fatalf("object %d: %v", ref.ObjectNum, err)
		}
		if l, _ := s.Dict["Length"].(ast.PdfInt); int(l) != len(raw) || len(raw) >= len(plainContent) {
			t.Errorf("object %d: /Length %v for %d bytes, want the smaller compressed size", ref.ObjectNum, s.Dict["Length"], len(raw))
		}
		decoded, err := filter.DecodeStream(ctx, s)
		if err != nil {
			t.Fatalf("object %d: decoding: %v", ref.ObjectNum, err)
		}
		if !bytes.Equal(decoded, plainContent) {
			t.Errorf("object %d decodes to other bytes than it held", ref.ObjectNum)
		}
	}
}

func TestCompressStreams_LeavesWhatItMustNotTouch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	compressed, ok, err := filter.CompressFlate(plainContent, -1)
	if err != nil || !ok {
		t.Fatalf("CompressFlate: %v", err)
	}
	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		data []byte
	}{
		{"XMP metadata", ast.PdfDict{"Type": ast.PdfName("Metadata"), "Subtype": ast.PdfName("XML")}, plainContent},
		{"an image", ast.PdfDict{"Subtype": ast.PdfName("Image"), "Width": ast.PdfInt(1), "Height": ast.PdfInt(1)}, plainContent},
		{"a filtered stream", ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, compressed},
		{"a filter array", ast.PdfDict{"Filter": ast.PdfArray{ast.PdfName("ASCIIHexDecode")}}, []byte("41>")},
		{"external data", ast.PdfDict{"F": ast.PdfString("data.bin")}, plainContent},
		{"decode parameters", ast.PdfDict{"DecodeParms": ast.PdfDict{}}, plainContent},
		{"a stream compression does not shrink", ast.PdfDict{}, []byte("q\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alloc := NewObjAllocator(0)
			ref := alloc.Alloc(ast.NewStream(tc.dict, tc.data))
			if n, err := CompressStreams(ctx, alloc, StreamCompression{}); err != nil || n != 0 {
				t.Fatalf("CompressStreams = %d, %v, want 0, nil", n, err)
			}
			s := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
			raw, err := s.Bytes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, tc.data) {
				t.Error("the stream's data changed")
			}
		})
	}
}

// TestCompressStreams_RecompressesPastTheBudget pins that a compression past
// the stream budget is redone from the source when the stream is serialized,
// to the same bytes.
func TestCompressStreams_RecompressesPastTheBudget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Half noise, half zeros: Flate keeps a little over half of it, so the
	// second stream's compression passes a budget of one stream's size.
	rng := rand.New(rand.NewPCG(1, 2))
	data := make([]byte, 4096)
	for i := range len(data) / 2 {
		data[i] = byte(rng.Uint32())
	}
	alloc := NewObjAllocator(0)
	first := alloc.Alloc(ast.NewStream(ast.PdfDict{}, data))
	second := alloc.Alloc(ast.NewStream(ast.PdfDict{}, data))
	if n, err := CompressStreams(ctx, alloc, StreamCompression{Limits: limits.Limits{MaxStreamBytes: int64(len(data))}}); err != nil || n != 2 {
		t.Fatalf("CompressStreams = %d, %v, want 2, nil", n, err)
	}
	a, err := alloc.Pending()[first.ObjectNum].(ast.PdfStream).Bytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b, err := alloc.Pending()[second.ObjectNum].(ast.PdfStream).Bytes(ctx)
	if err != nil {
		t.Fatalf("recompressing past the budget: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Error("the stream compressed again from its source differs from the one kept in memory")
	}
}
