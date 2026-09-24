package filter

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"testing"

	"github.com/andybalholm/brotli"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func zlibCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	_, _ = w.Write(data) //nolint:errcheck // In-memory buffer write cannot fail
	_ = w.Close()        //nolint:errcheck // Flush to in-memory buffer cannot fail
	return buf.Bytes()
}

func lzwCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := lzw.NewWriter(&buf, lzw.MSB, 8)
	_, _ = w.Write(data) //nolint:errcheck // In-memory buffer write cannot fail
	_ = w.Close()        //nolint:errcheck // Flush to in-memory buffer cannot fail
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

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
		"DecodeParms": ast.PdfDict{
			"Predictor":        ast.PdfInt(12),
			"Columns":          ast.PdfInt(3),
			"Colors":           ast.PdfInt(1),
			"BitsPerComponent": ast.PdfInt(8),
		},
	}, zlibCompress(raw))

	result, err := DecodeStream(context.Background(), stream)
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
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
	}, zlibCompress(raw))

	result, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	if !bytes.Equal(result, raw) {
		t.Errorf("got %q, want %q", result, raw)
	}
}

func TestDecodeStreamChecked_IncompleteOnUnsupportedFilter(t *testing.T) {
	raw := []byte("payload")
	// Chain: FlateDecode (supported) then a filter no decoder knows (skipped).
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("NoSuchDecode")},
	}, zlibCompress(raw))

	data, incomplete, err := DecodeStreamChecked(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStreamChecked: %v", err)
	}
	if !incomplete {
		t.Error("expected incomplete=true when an unsupported filter is skipped")
	}
	if !bytes.Equal(data, raw) {
		t.Errorf("data = %q, want %q", data, raw)
	}

	// A fully supported chain reports complete.
	full := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, zlibCompress(raw))
	if _, incomplete2, err := DecodeStreamChecked(context.Background(), full); err != nil {
		t.Fatalf("DecodeStreamChecked (full): %v", err)
	} else if incomplete2 {
		t.Error("expected incomplete=false for a fully supported chain")
	}
}

func TestDecodeStream_LZW(t *testing.T) {
	raw := []byte("The quick brown fox jumps over the lazy dog")
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("LZWDecode"),
	}, lzwCompress(raw))

	result, err := DecodeStream(context.Background(), stream)
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

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("LZWDecode"),
		"DecodeParms": ast.PdfDict{
			"Predictor": ast.PdfInt(12),
			"Columns":   ast.PdfInt(3),
		},
	}, lzwCompress(raw))

	result, err := DecodeStream(context.Background(), stream)
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
	stream := ast.NewStream(ast.PdfDict{}, raw)

	result, err := DecodeStream(context.Background(), stream)
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

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{
			ast.PdfName("ASCIIHexDecode"),
			ast.PdfName("FlateDecode"),
		},
	}, []byte(hexEncoded))

	result, err := DecodeStream(context.Background(), stream)
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

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{
			ast.PdfName("FlateDecode"),
		},
		"DecodeParms": ast.PdfArray{
			ast.PdfDict{
				"Predictor": ast.PdfInt(11),
				"Columns":   ast.PdfInt(2),
			},
		},
	}, zlibCompress(raw))

	result, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	expected := []byte{10, 15}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestDecodeStream_UnsupportedFilter(t *testing.T) {
	raw := []byte{0x42}
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("NoSuchFilter"),
	}, raw)

	// Unsupported filters are skipped, returning the raw data.
	data, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Errorf("expected nil error (skip unsupported), got: %v", err)
	}
	if len(data) != 1 || data[0] != 0x42 {
		t.Errorf("expected raw data, got: %v", data)
	}
}

func TestApplyDecodeFilter_ASCIIHex(t *testing.T) {
	result, err := applyDecodeFilter(context.Background(), []byte("48656c6c6f>"), "ASCIIHexDecode", nil, nil)
	if err != nil {
		t.Fatalf("ApplyDecodeFilter failed: %v", err)
	}
	if !bytes.Equal(result, []byte("Hello")) {
		t.Errorf("got %q, want %q", result, "Hello")
	}
}

func TestDecodeASCII85(t *testing.T) {
	// "87cURD]i,\"Ebo7" is the ASCII85 encoding of "Hello World".
	const encoded = "87cURD]i,\"Ebo7"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "basic with EOD marker",
			input: encoded + "~>",
			want:  "Hello World",
		},
		{
			name:  "trailing garbage after EOD",
			input: encoded + "~>\x00\x00padding",
			want:  "Hello World",
		},
		{
			name:  "missing EOD marker",
			input: encoded,
			want:  "Hello World",
		},
		{
			name:  "whitespace inside stream",
			input: "87cUR D]i,\n\"Ebo7~>",
			want:  "Hello World",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeASCII85([]byte(tt.input))
			if err != nil {
				t.Fatalf("decodeASCII85 failed: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyDecodeFilter_ASCII85(t *testing.T) {
	result, err := applyDecodeFilter(context.Background(), []byte("87cURD]i,\"Ebo7~>"), "ASCII85Decode", nil, nil)
	if err != nil {
		t.Fatalf("ApplyDecodeFilter failed: %v", err)
	}
	if string(result) != "Hello World" {
		t.Errorf("got %q, want %q", result, "Hello World")
	}
}

func TestApplyDecodeFilter_ASCII85_Abbreviation(t *testing.T) {
	result, err := applyDecodeFilter(context.Background(), []byte("87cURD]i,\"Ebo7~>"), "A85", nil, nil)
	if err != nil {
		t.Fatalf("ApplyDecodeFilter failed: %v", err)
	}
	if string(result) != "Hello World" {
		t.Errorf("got %q, want %q", result, "Hello World")
	}
}

func TestDecodeStream_TIFFPredictor(t *testing.T) {
	// TIFF Predictor 2 with FlateDecode.
	// 2 columns, 3 colors → 6 bytes per row.
	// Input: [10, 20, 30, 5, 10, 15]
	// After TIFF un-diff: [10, 20, 30, 15, 30, 45]
	raw := []byte{10, 20, 30, 5, 10, 15}

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
		"DecodeParms": ast.PdfDict{
			"Predictor": ast.PdfInt(2),
			"Columns":   ast.PdfInt(2),
			"Colors":    ast.PdfInt(3),
		},
	}, zlibCompress(raw))

	result, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream failed: %v", err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %v, want %v", result, expected)
	}
}

func TestTIFFPredictor_BPC4(t *testing.T) {
	// BPC=4, Colors=1, Columns=3 → 3 nibble samples per row = 12 bits = 2 bytes
	// (with 4 bits of padding at the end of each row).
	//
	// Original samples:  [5, 10, 3]
	// TIFF differencing (encoding):
	//   sample[0] = 5               → stored as 5
	//   sample[1] = 10 - 5 = 5      → stored as 5
	//   sample[2] = 3 - 10 = -7 ≡ 9 (mod 16)  → stored as 9
	//
	// Packed (MSB-first): nibbles [5, 5, 9, 0(pad)]
	//   byte 0: 0x55  (5<<4 | 5)
	//   byte 1: 0x90  (9<<4 | 0)
	//
	// After TIFF un-differencing:
	//   sample[0] = 5
	//   sample[1] = (5 + 5) & 0xF = 10
	//   sample[2] = (9 + 10) & 0xF = 3
	//
	// Reconstructed packed: nibbles [5, 10, 3, 0(pad)]
	//   byte 0: 0x5A  (5<<4 | 10)
	//   byte 1: 0x30  (3<<4 | 0)

	diffed := []byte{0x55, 0x90}
	params := &predictorParams{
		Predictor:        2,
		Columns:          3,
		Colors:           1,
		BitsPerComponent: 4,
	}

	result, err := applyTIFFPredictor(diffed, params)
	if err != nil {
		t.Fatalf("applyTIFFPredictor BPC=4 failed: %v", err)
	}

	expected := []byte{0x5A, 0x30}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %02x, want %02x", result, expected)
	}
}

func TestTIFFPredictor_BPC16(t *testing.T) {
	// BPC=16, Colors=1, Columns=3 → 3 samples × 2 bytes = 6 bytes per row.
	// Samples are big-endian uint16; differencing must carry across the
	// byte boundary, which the pre-fix sub-byte path could not do.
	//
	// Original samples: [300, 1000, 65535]
	// Differenced (encoding): [300, 700, 64535]
	//   300   = 0x012C, 700 = 0x02BC, 64535 = 0xFC17
	diffed := []byte{0x01, 0x2C, 0x02, 0xBC, 0xFC, 0x17}
	params := &predictorParams{
		Predictor:        2,
		Columns:          3,
		Colors:           1,
		BitsPerComponent: 16,
	}

	result, err := applyTIFFPredictor(diffed, params)
	if err != nil {
		t.Fatalf("applyTIFFPredictor BPC=16 failed: %v", err)
	}

	// Un-differenced: [300, 1000, 65535] → 0x012C 0x03E8 0xFFFF
	expected := []byte{0x01, 0x2C, 0x03, 0xE8, 0xFF, 0xFF}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %02x, want %02x", result, expected)
	}
}

func TestTIFFPredictor_UnsupportedBPC(t *testing.T) {
	// A BitsPerComponent PDF images never use must error rather than
	// silently mis-decode.
	params := &predictorParams{
		Predictor:        2,
		Columns:          2,
		Colors:           1,
		BitsPerComponent: 12,
	}
	_, err := applyTIFFPredictor([]byte{0, 0, 0}, params)
	if !errors.Is(err, ErrUnsupportedPredictor) {
		t.Fatalf("got %v, want ErrUnsupportedPredictor", err)
	}
}

func TestTIFFPredictor_BPC2(t *testing.T) {
	// BPC=2, Colors=1, Columns=4 → 4 samples × 2 bits = 8 bits = 1 byte per row.
	//
	// Original samples:  [1, 3, 2, 0]
	// TIFF differencing:
	//   sample[0] = 1               → 1
	//   sample[1] = 3 - 1 = 2       → 2
	//   sample[2] = 2 - 3 = -1 ≡ 3  → 3
	//   sample[3] = 0 - 2 = -2 ≡ 2  → 2
	//
	// Packed: crumbs [1, 2, 3, 2] → 01 10 11 10 → 0x6E
	//
	// After un-differencing: [1, 3, 2, 0]
	// Packed: 01 11 10 00 → 0x78

	diffed := []byte{0x6E}
	params := &predictorParams{
		Predictor:        2,
		Columns:          4,
		Colors:           1,
		BitsPerComponent: 2,
	}

	result, err := applyTIFFPredictor(diffed, params)
	if err != nil {
		t.Fatalf("applyTIFFPredictor BPC=2 failed: %v", err)
	}

	expected := []byte{0x78}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %02x, want %02x", result, expected)
	}
}

func TestTIFFPredictor_BPC1(t *testing.T) {
	// BPC=1, Colors=1, Columns=8 → 8 samples × 1 bit = 1 byte per row.
	//
	// Original:    [1, 0, 1, 1, 0, 0, 1, 0] = 0xB2
	// Differenced: [1, 1, 1, 0, 1, 0, 1, 1] = 0xEB (each diff = cur - prev mod 2)
	//
	// Un-differencing (cumulative add mod 2):
	//   s[0]=1, s[1]=(1+1)&1=0, s[2]=(1+0)&1=1, s[3]=(0+1)&1=1,
	//   s[4]=(1+1)&1=0, s[5]=(0+0)&1=0, s[6]=(1+0)&1=1, s[7]=(1+1)&1=0
	// = [1, 0, 1, 1, 0, 0, 1, 0] = 0xB2

	diffed := []byte{0xEB}
	params := &predictorParams{
		Predictor:        2,
		Columns:          8,
		Colors:           1,
		BitsPerComponent: 1,
	}

	result, err := applyTIFFPredictor(diffed, params)
	if err != nil {
		t.Fatalf("applyTIFFPredictor BPC=1 failed: %v", err)
	}

	expected := []byte{0xB2}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %02x, want %02x", result, expected)
	}
}

func TestTIFFPredictor_BPC4_MultiRow(t *testing.T) {
	// Two rows of BPC=4, Colors=1, Columns=3 to verify rows are independent.
	// Row 1: diffed [5, 5, 9] padded → 0x55 0x90 → undiffed [5, 10, 3] → 0x5A 0x30
	// Row 2: diffed [2, 3, 1] padded → 0x23 0x10 → undiffed [2, 5, 6]  → 0x25 0x60
	diffed := []byte{0x55, 0x90, 0x23, 0x10}
	params := &predictorParams{
		Predictor:        2,
		Columns:          3,
		Colors:           1,
		BitsPerComponent: 4,
	}

	result, err := applyTIFFPredictor(diffed, params)
	if err != nil {
		t.Fatalf("applyTIFFPredictor BPC=4 multi-row failed: %v", err)
	}

	expected := []byte{0x5A, 0x30, 0x25, 0x60}
	if !bytes.Equal(result, expected) {
		t.Errorf("got %02x, want %02x", result, expected)
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

// TestExtractDecodeParms_ArrayUnderSingleFilter ignores an array of
// parameters under the single filter of an image, which ISO 32000-1, Table 5
// gives a dictionary, and keeps it for any other stream and under a filter
// array.
func TestExtractDecodeParms_ArrayUnderSingleFilter(t *testing.T) {
	dict := ast.PdfDict{
		"Subtype":     ast.PdfName("Image"),
		"Filter":      ast.PdfName("FlateDecode"),
		"DecodeParms": ast.PdfArray{ast.PdfDict{"Predictor": ast.PdfInt(15)}},
	}
	if result := extractDecodeParms(dict, 1); result[0] != nil {
		t.Errorf("image: parms = %v, want none", result[0])
	}
	dict["Filter"] = ast.PdfArray{ast.PdfName("FlateDecode")}
	if result := extractDecodeParms(dict, 1); result[0] == nil {
		t.Error("image under a one-filter array: lost its parameters")
	}
	dict["Filter"] = ast.PdfName("FlateDecode")
	delete(dict, "Subtype")
	if result := extractDecodeParms(dict, 1); result[0] == nil {
		t.Error("xref-like stream: lost its parameters")
	}
}

func TestExtractDecodeParms_Missing(t *testing.T) {
	dict := ast.PdfDict{}
	result := extractDecodeParms(dict, 2)
	if result[0] != nil || result[1] != nil {
		t.Error("expected all nil when DecodeParms missing")
	}
}

func TestNewStreamReader_NoFilter(t *testing.T) {
	raw := []byte("unfiltered content")
	stream := ast.NewStream(ast.PdfDict{}, raw)

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("got %q, want %q", got, raw)
	}
}

func TestNewStreamReader_Flate(t *testing.T) {
	raw := []byte("hello from flate stream reader")
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
	}, zlibCompress(raw))

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("got %q, want %q", got, raw)
	}
}

func TestNewStreamReader_LZW(t *testing.T) {
	raw := []byte("LZW streaming test data")
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("LZWDecode"),
	}, lzwCompress(raw))

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, raw) {
		t.Errorf("got %q, want %q", got, raw)
	}
}

func TestNewStreamReader_FlateWithPredictor(t *testing.T) {
	// Predictor forces fallback to eager decode path.
	raw := []byte{
		0, 10, 20, 30,
		2, 5, 10, 15,
	}
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
		"DecodeParms": ast.PdfDict{
			"Predictor": ast.PdfInt(12),
			"Columns":   ast.PdfInt(3),
		},
	}, zlibCompress(raw))

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rc.Close() }()

	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}

	expected := []byte{10, 20, 30, 15, 30, 45}
	if !bytes.Equal(got, expected) {
		t.Errorf("got %v, want %v", got, expected)
	}
}

func TestDecodeCCITTFax_Group4_AllWhite(t *testing.T) {
	// Group 4 encoded 8x1 all-white image.
	// V(0) encodes an all-white row (single '1' bit), then EOFB (two 12-bit
	// codes of 000000000001).
	// Bitstream: 1 000000000001 000000000001 (padded)
	data := []byte{0x80, 0x10, 0x01}

	parms := ast.PdfDict{
		"K":       ast.PdfInt(-1),
		"Columns": ast.PdfInt(8),
		"Rows":    ast.PdfInt(1),
	}

	decoded, err := decodeCCITTFax(context.Background(), data, parms)
	if err != nil {
		t.Fatalf("decodeCCITTFax Group4 all-white failed: %v", err)
	}

	expected := []byte{0xFF}
	if !bytes.Equal(decoded, expected) {
		t.Errorf("got %02x, want %02x", decoded, expected)
	}
}

func TestDecodeCCITTFax_Group4_TwoRows(t *testing.T) {
	// Group 4, 8x2, both rows all-white.
	// Row 0: V(0) = 1 bit
	// Row 1: V(0) = 1 bit
	// EOFB: 000000000001 000000000001
	// Bitstream: 1 1 000000000001 000000000001 (padded)
	data := []byte{0xC0, 0x10, 0x01}

	parms := ast.PdfDict{
		"K":       ast.PdfInt(-1),
		"Columns": ast.PdfInt(8),
		"Rows":    ast.PdfInt(2),
	}

	decoded, err := decodeCCITTFax(context.Background(), data, parms)
	if err != nil {
		t.Fatalf("decodeCCITTFax Group4 2-row all-white failed: %v", err)
	}

	expected := []byte{0xFF, 0xFF}
	if !bytes.Equal(decoded, expected) {
		t.Errorf("got %02x, want %02x", decoded, expected)
	}
}

func TestDecodeCCITTFax_Group4_WhiteThenBlack(t *testing.T) {
	// Group 4, 8x2: row 0 = all white, row 1 = all black.
	// Row 0: V(0) = 1
	// Row 1: horizontal mode (001) + white(0) run-length code (00110101)
	//         + black(8) run-length code (00011)
	// EOFB: 000000000001 000000000001
	data := []byte{0x93, 0x51, 0x40, 0x04, 0x00, 0x40}

	parms := ast.PdfDict{
		"K":       ast.PdfInt(-1),
		"Columns": ast.PdfInt(8),
		"Rows":    ast.PdfInt(2),
	}

	decoded, err := decodeCCITTFax(context.Background(), data, parms)
	if err != nil {
		t.Fatalf("decodeCCITTFax Group4 white+black failed: %v", err)
	}

	expected := []byte{0xFF, 0x00}
	if !bytes.Equal(decoded, expected) {
		t.Errorf("got %02x, want %02x", decoded, expected)
	}
}

func TestApplyDecodeFilter_CCITTFax(t *testing.T) {
	// Verify CCITTFaxDecode is wired into the main filter dispatch.
	data := []byte{0x80, 0x10, 0x01}
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("CCITTFaxDecode"),
		"DecodeParms": ast.PdfDict{
			"K":       ast.PdfInt(-1),
			"Columns": ast.PdfInt(8),
			"Rows":    ast.PdfInt(1),
		},
	}, data)

	decoded, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream CCITTFaxDecode failed: %v", err)
	}

	expected := []byte{0xFF}
	if !bytes.Equal(decoded, expected) {
		t.Errorf("got %02x, want %02x", decoded, expected)
	}
}

func TestApplyDecodeFilter_CCF_Abbreviation(t *testing.T) {
	// Verify the /CCF abbreviation also works.
	data := []byte{0x80, 0x10, 0x01}
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("CCF"),
		"DecodeParms": ast.PdfDict{
			"K":       ast.PdfInt(-1),
			"Columns": ast.PdfInt(8),
			"Rows":    ast.PdfInt(1),
		},
	}, data)

	decoded, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream CCF failed: %v", err)
	}

	expected := []byte{0xFF}
	if !bytes.Equal(decoded, expected) {
		t.Errorf("got %02x, want %02x", decoded, expected)
	}
}

func brotliCompress(data []byte) []byte {
	var buf bytes.Buffer
	w := brotli.NewWriter(&buf)
	_, _ = w.Write(data) //nolint:errcheck // In-memory buffer write cannot fail
	_ = w.Close()        //nolint:errcheck // Flush to in-memory buffer cannot fail
	return buf.Bytes()
}

func TestDecodeStream_Brotli(t *testing.T) {
	original := []byte("The quick brown fox jumps over the lazy dog")
	compressed := brotliCompress(original)

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("BrotliDecode"),
	}, compressed)

	decoded, err := DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("DecodeStream brotli failed: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Errorf("got %q, want %q", decoded, original)
	}
}

func TestBrotli_RoundTrip(t *testing.T) {
	original := []byte("litho PDF engine, Brotli round-trip test")

	encoded, err := ApplyEncodeFilter(original, "BrotliDecode")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := applyDecodeFilter(context.Background(), encoded, "BrotliDecode", nil, nil)
	if err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if !bytes.Equal(decoded, original) {
		t.Errorf("round-trip mismatch: got %q, want %q", decoded, original)
	}
}

func TestNewStreamReader_Brotli(t *testing.T) {
	original := []byte("streaming brotli decode test")
	compressed := brotliCompress(original)

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("BrotliDecode"),
	}, compressed)

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatalf("NewStreamReader failed: %v", err)
	}
	defer func() {
		_ = rc.Close() //nolint:errcheck // Test cleanup: a close failure here cannot affect the assertion already made.
	}()

	decoded, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Errorf("got %q, want %q", decoded, original)
	}
}

// TestDecodeCCITTFax_Group3 decodes the Group 3 codings the previous decoder
// refused: one-dimensional rows without end-of-line codes, which ISO 32000-1,
// Table 11 makes optional, and mixed coding, whose tag bits name each row's
// coding. Both rows are three white, two black, three white pixels.
func TestDecodeCCITTFax_Group3(t *testing.T) {
	for _, tc := range []struct {
		name string
		k    int
		bits []byte
	}{
		// 1000 11 1000, twice.
		{"one-dimensional", 0, []byte{0b10001110, 0b00100011, 0b10000000}},
		// tag 1, 1000 11 1000; tag 0, V0 V0 V0.
		{"mixed", 1, []byte{0b11000111, 0b00001110}},
	} {
		parms := ast.PdfDict{"K": ast.PdfInt(tc.k), "Columns": ast.PdfInt(8), "Rows": ast.PdfInt(2)}
		got, err := decodeCCITTFax(context.Background(), tc.bits, parms)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !bytes.Equal(got, []byte{0b11100111, 0b11100111}) {
			t.Errorf("%s: %08b, want 11100111 twice", tc.name, got)
		}
	}
	if _, err := decodeCCITTFax(context.Background(), nil, ast.PdfDict{"Columns": ast.PdfInt(1 << 30)}); !errors.Is(err, ErrDecompressionBomb) {
		t.Errorf("a width past the cap: %v, want %v", err, ErrDecompressionBomb)
	}
}
