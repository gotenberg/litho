package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/plugins"
)

// TestDecoder_LoweredBudgetRefusesAFlateBomb pins that a Decoder's stream
// budget, not the default, bounds FlateDecode: 3 MiB of zeros decode under the
// default and fail under a 1 MiB budget.
func TestDecoder_LoweredBudgetRefusesAFlateBomb(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(make([]byte, 3*lowBudget)); err != nil {
		t.Fatalf("building the stream: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, buf.Bytes())

	if out, err := DecodeStream(context.Background(), stream); err != nil || len(out) != 3*lowBudget {
		t.Fatalf("default budget: %d bytes, %v; want %d bytes", len(out), err, 3*lowBudget)
	}
	d := NewDecoder(nil, limits.Limits{MaxStreamBytes: lowBudget})
	if _, err := d.DecodeStream(context.Background(), stream); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("1 MiB budget: err = %v, want ErrDecompressionBomb", err)
	}
	if _, _, err := d.DecodeStreamUpTo(context.Background(), stream, "JPXDecode"); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("1 MiB budget, DecodeStreamUpTo: err = %v, want ErrDecompressionBomb", err)
	}
}

// TestDecoder_LoweredBudgetRefusesARunLengthBomb pins the same bound through a
// Decoder, which is the route every document decode takes.
func TestDecoder_LoweredBudgetRefusesARunLengthBomb(t *testing.T) {
	t.Parallel()

	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("RunLengthDecode")}, runLengthBomb(3*lowBudget/128))
	if out, err := DecodeStream(context.Background(), stream); err != nil || len(out) != 3*lowBudget {
		t.Fatalf("default budget: %d bytes, %v; want %d bytes", len(out), err, 3*lowBudget)
	}
	d := NewDecoder(nil, limits.Limits{MaxStreamBytes: lowBudget})
	if _, err := d.DecodeStream(context.Background(), stream); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("1 MiB budget: err = %v, want ErrDecompressionBomb", err)
	}
	if rc, err := d.NewStreamReader(context.Background(), stream); !errors.Is(err, ErrDecompressionBomb) {
		if rc != nil {
			rc.Close()
		}
		t.Errorf("1 MiB budget, NewStreamReader: err = %v, want ErrDecompressionBomb", err)
	}
}

// TestDecoder_LoweredBudgetBoundsPluginsAndCCITT pins the two outputs that
// never pass through limitedCopy: a plugin's slice and CCITTFaxDecode's rows.
func TestDecoder_LoweredBudgetBoundsPluginsAndCCITT(t *testing.T) {
	t.Parallel()

	p := &mockPlugin{name: "VendorDecode", result: make([]byte, lowBudget+1)}
	d := NewDecoder([]plugins.FilterDecoder{p}, limits.Limits{MaxStreamBytes: lowBudget})
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("VendorDecode")}, []byte("raw"))
	if _, err := d.DecodeStream(context.Background(), stream); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("plugin output past a 1 MiB budget: err = %v, want ErrDecompressionBomb", err)
	}

	parms := ast.PdfDict{"Columns": ast.PdfInt(8), "Rows": ast.PdfInt(lowBudget + 1)}
	if _, err := decodeCCITTFax(context.Background(), nil, parms, lowBudget); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("CCITTFaxDecode rows past a 1 MiB budget: err = %v, want ErrDecompressionBomb", err)
	}
}
