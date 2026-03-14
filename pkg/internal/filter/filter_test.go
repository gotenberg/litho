package filter

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func zlibCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

func lzwCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	_, _ = w.Write(data)
	_ = w.Close()
	return buf.Bytes()
}

func TestDecodeStream_FlateWithPredictor(t *testing.T) {
	// 3 columns, 1 color, 8 BPC, PNG predictor (Up).
	// Row 0 (None): [0, 10, 20, 30]
	// Row 1 (Up):   [2, 5, 10, 15]
	// Expected decoded: [10, 20, 30, 15, 30, 45]
	raw := []byte{
		0, 10, 20, 30,
		2, 5, 10, 15,
	}

	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfName("FlateDecode"),
			"DecodeParms": ast.PdfDict{
				"Predictor":        ast.PdfInt(12),
				"Columns":          ast.PdfInt(3),
				"Colors":           ast.PdfInt(1),
				"BitsPerComponent": ast.PdfInt(8),
			},
		},
		Data: zlibCompress(raw),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestDecodeStream_FlateNoPredictor(t *testing.T) {
	raw := []byte("hello world")
	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfName("FlateDecode"),
		},
		Data: zlibCompress(raw),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q", result, raw)
	}
}

func TestDecodeStream_LZW(t *testing.T) {
	raw := []byte("The quick brown fox jumps over the lazy dog")
	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfName("LZWDecode"),
		},
		Data: lzwCompress(raw),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q", result, raw)
	}
}

func TestDecodeStream_LZWWithPredictor(t *testing.T) {
	// Same predictor test as Flate but with LZW compression.
	raw := []byte{
		0, 10, 20, 30,
		2, 5, 10, 15,
	}

	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfName("LZWDecode"),
			"DecodeParms": ast.PdfDict{
				"Predictor": ast.PdfInt(12),
				"Columns":   ast.PdfInt(3),
			},
		},
		Data: lzwCompress(raw),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestDecodeStream_NoFilter(t *testing.T) {
	raw := []byte("unfiltered data")
	stream := ast.PdfStream{
		Dict: ast.PdfDict{},
		Data: raw,
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q", result, raw)
	}
}

func TestDecodeStream_FilterArray(t *testing.T) {
	// ASCIIHex → FlateDecode chain.
	raw := []byte("test data 123")
	compressed := zlibCompress(raw)
	hexEncoded := hex.EncodeToString(compressed) + ">"

	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfArray{
				ast.PdfName("ASCIIHexDecode"),
				ast.PdfName("FlateDecode"),
			},
		},
		Data: []byte(hexEncoded),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q", result, raw)
	}
}

func TestDecodeStream_FilterArrayWithParms(t *testing.T) {
	// FlateDecode with predictor, applied as first filter.
	// 2 columns, PNG Sub filter.
	raw := []byte{
		1, 10, 5, // Sub: 10, 10+5=15
	}

	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfArray{
				ast.PdfName("FlateDecode"),
			},
			"DecodeParms": ast.PdfArray{
				ast.PdfDict{
					"Predictor": ast.PdfInt(11),
					"Columns":   ast.PdfInt(2),
				},
			},
		},
		Data: zlibCompress(raw),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	expected := []byte{10, 15}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestDecodeStream_UnsupportedFilter(t *testing.T) {
	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfName("RunLengthDecode"),
		},
		Data: []byte{0},
	}

	_, err := DecodeStream(stream)
	if !errors.Is(err, ErrUnsupportedFilter) {
		t.Errorf("expected ErrUnsupportedFilter, got: %v", err)
	}
}

func TestApplyDecodeFilter_ASCIIHex(t *testing.T) {
	result, err := ApplyDecodeFilter([]byte("48656c6c6f>"), "ASCIIHexDecode")
	if err != nil {
		t.Fatalf("ApplyDecodeFilter failed: %v", err)
	}
	if !bytes.Equal(result, []byte("Hello")) {
		t.Errorf("got %q, want %q", result, "Hello")
	}
}

func TestDecodeStream_TIFFPredictor(t *testing.T) {
	// TIFF Predictor 2 with FlateDecode.
	// 2 columns, 3 colors → 6 bytes per row.
	// Input: [10, 20, 30, 5, 10, 15]
	// After TIFF un-diff: [10, 20, 30, 15, 30, 45]
	raw := []byte{10, 20, 30, 5, 10, 15}

	stream := ast.PdfStream{
		Dict: ast.PdfDict{
			"Filter": ast.PdfName("FlateDecode"),
			"DecodeParms": ast.PdfDict{
				"Predictor": ast.PdfInt(2),
				"Columns":   ast.PdfInt(2),
				"Colors":    ast.PdfInt(3),
			},
		},
		Data: zlibCompress(raw),
	}

	result, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestExtractDecodeParms_SingleDict(t *testing.T) {
	dict := ast.PdfDict{
		"DecodeParms": ast.PdfDict{
			"Predictor": ast.PdfInt(12),
		},
	}
	result := extractDecodeParms(dict, 1)
	if len(result) != 1 || result[0] == nil {
		t.Fatal("expected single parms dict")
	}
	if pdfInt(result[0]["Predictor"]) != 12 {
		t.Error("expected Predictor=12")
	}
}

func TestExtractDecodeParms_Array(t *testing.T) {
	dict := ast.PdfDict{
		"DecodeParms": ast.PdfArray{
			ast.PdfNull{},
			ast.PdfDict{"Predictor": ast.PdfInt(15)},
		},
	}
	result := extractDecodeParms(dict, 2)
	if result[0] != nil {
		t.Error("expected nil for first parms")
	}
	if result[1] == nil || pdfInt(result[1]["Predictor"]) != 15 {
		t.Error("expected Predictor=15 for second parms")
	}
}

func TestExtractDecodeParms_Missing(t *testing.T) {
	dict := ast.PdfDict{}
	result := extractDecodeParms(dict, 2)
	if result[0] != nil || result[1] != nil {
		t.Error("expected all nil when DecodeParms missing")
	}
}
