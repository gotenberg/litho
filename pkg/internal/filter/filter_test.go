package filter

import (
	"bytes"
	"compress/lzw"
	"compress/zlib"
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
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("FlateDecode"),
	}, zlibCompress(raw))

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
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("LZWDecode"),
	}, lzwCompress(raw))

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

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("LZWDecode"),
		"DecodeParms": ast.PdfDict{
			"Predictor": ast.PdfInt(12),
			"Columns":   ast.PdfInt(3),
		},
	}, lzwCompress(raw))

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
	stream := ast.NewStream(ast.PdfDict{}, raw)

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

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{
			ast.PdfName("ASCIIHexDecode"),
			ast.PdfName("FlateDecode"),
		},
	}, []byte(hexEncoded))

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
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfName("NoSuchFilter"),
	}, []byte{0})

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
	result, err := ApplyDecodeFilter([]byte("87cURD]i,\"Ebo7~>"), "ASCII85Decode")
	if err != nil {
		t.Fatalf("ApplyDecodeFilter failed: %v", err)
	}
	if string(result) != "Hello World" {
		t.Errorf("got %q, want %q", result, "Hello World")
	}
}

func TestApplyDecodeFilter_ASCII85_Abbreviation(t *testing.T) {
	result, err := ApplyDecodeFilter([]byte("87cURD]i,\"Ebo7~>"), "A85")
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

	result, err := DecodeStream(stream)
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

func TestExtractDecodeParms_Missing(t *testing.T) {
	dict := ast.PdfDict{}
	result := extractDecodeParms(dict, 2)
	if result[0] != nil || result[1] != nil {
		t.Error("expected all nil when DecodeParms missing")
	}
}

// lzwEncode is a test-only LZW encoder that supports both EarlyChange=1 and
// EarlyChange=0 modes, producing MSB-first PDF-compatible LZW bitstreams.
func lzwEncode(data []byte, earlyChange int) []byte {
	type dictEntry struct {
		prefix int // parent code (-1 for root)
		ch     byte
	}

	// Dictionary: indices 0–255 are literals, 256=ClearTable, 257=EOD.
	// Pad to 258 so that append places the first user entry at index 258,
	// matching nextCode.
	dict := make([]dictEntry, 258, 4096)
	for i := range 256 {
		dict[i] = dictEntry{prefix: -1, ch: byte(i)}
	}
	nextCode := 258
	codeWidth := 9

	// Bit-writing state (MSB-first).
	var out bytes.Buffer
	var bits uint64
	nBits := uint(0)

	writeCode := func(code int) {
		bits = bits<<uint(codeWidth) | uint64(code)
		nBits += uint(codeWidth)
		for nBits >= 8 {
			nBits -= 8
			out.WriteByte(byte(bits >> nBits))
		}
	}

	maybeWiden := func() {
		if codeWidth < 12 && nextCode == (1<<codeWidth)-earlyChange {
			codeWidth++
		}
	}

	// lookup searches for prefix+ch in the dictionary, returning the code or -1.
	lookup := func(prefix int, ch byte) int {
		for i := 258; i < len(dict); i++ {
			if dict[i].prefix == prefix && dict[i].ch == ch {
				return i
			}
		}
		return -1
	}

	// Start with a ClearTable.
	writeCode(256)

	if len(data) == 0 {
		writeCode(257)
		if nBits > 0 {
			out.WriteByte(byte(bits << (8 - nBits)))
		}
		return out.Bytes()
	}

	w := int(data[0]) // current match code
	for _, ch := range data[1:] {
		found := lookup(w, ch)
		if found >= 0 {
			w = found
		} else {
			writeCode(w)
			if nextCode <= lzwMaxCode {
				dict = append(dict, dictEntry{prefix: w, ch: ch})
				nextCode++
				maybeWiden()
			}
			w = int(ch)
		}
	}
	writeCode(w)
	writeCode(257) // EOD

	// Flush remaining bits.
	if nBits > 0 {
		out.WriteByte(byte(bits << (8 - nBits)))
	}
	return out.Bytes()
}

func TestDecodeLZW_EarlyChange0_Roundtrip(t *testing.T) {
	// Build input data that forces the dictionary past the 511-entry boundary
	// where EarlyChange=0 and EarlyChange=1 diverge. We need 254+ unique pairs
	// to push nextCode past 511 (258 + 254 = 512).
	// Using a repeating pattern of all 256 byte values guarantees many new
	// dictionary entries.
	var input []byte
	for i := range 512 {
		input = append(input, byte(i%256))
	}

	encoded := lzwEncode(input, 0)

	// Decode with our custom decoder (EarlyChange=0).
	decoded, err := decodeLZW(encoded, ast.PdfDict{
		"EarlyChange": ast.PdfInt(0),
	})
	if err != nil {
		t.Fatalf("decodeLZW EarlyChange=0 failed: %v", err)
	}

	if !bytes.Equal(decoded, input) {
		t.Errorf("EarlyChange=0 roundtrip failed: got %d bytes, want %d bytes", len(decoded), len(input))
		// Show first divergence.
		for i := range min(len(decoded), len(input)) {
			if decoded[i] != input[i] {
				t.Errorf("first mismatch at byte %d: got 0x%02x, want 0x%02x", i, decoded[i], input[i])
				break
			}
		}
	}
}

func TestDecodeLZW_EarlyChange0_HardcodedStream(t *testing.T) {
	// Manually encode "ABCABCABCABC" with EarlyChange=0.
	input := []byte("ABCABCABCABC")
	encoded := lzwEncode(input, 0)

	decoded, err := decodeLZW(encoded, ast.PdfDict{
		"EarlyChange": ast.PdfInt(0),
	})
	if err != nil {
		t.Fatalf("decodeLZW failed: %v", err)
	}

	if !bytes.Equal(decoded, input) {
		t.Errorf("got %q, want %q", decoded, input)
	}
}

func TestDecodeLZW_EarlyChange1_Roundtrip(t *testing.T) {
	// Verify standard EarlyChange=1 still works through the same code path,
	// including past the 511-entry boundary.
	var input []byte
	for i := range 512 {
		input = append(input, byte(i%256))
	}

	encoded := lzwEncode(input, 1)

	decoded, err := decodeLZW(encoded, ast.PdfDict{
		"EarlyChange": ast.PdfInt(1),
	})
	if err != nil {
		t.Fatalf("decodeLZW EarlyChange=1 failed: %v", err)
	}

	if !bytes.Equal(decoded, input) {
		t.Errorf("EarlyChange=1 roundtrip failed: got %d bytes, want %d bytes", len(decoded), len(input))
	}
}

func TestDecodeLZW_EarlyChange0_LargeStream(t *testing.T) {
	// Stress test: 4096 bytes forces the dictionary through multiple width
	// transitions (9→10→11→12 bits).
	var input []byte
	for i := range 4096 {
		input = append(input, byte(i%251)) // prime modulus avoids trivial patterns
	}

	encoded := lzwEncode(input, 0)

	decoded, err := decodeLZW(encoded, ast.PdfDict{
		"EarlyChange": ast.PdfInt(0),
	})
	if err != nil {
		t.Fatalf("decodeLZW EarlyChange=0 large stream failed: %v", err)
	}

	if !bytes.Equal(decoded, input) {
		t.Errorf("large stream roundtrip failed: got %d bytes, want %d bytes", len(decoded), len(input))
	}
}

func TestNewStreamReader_NoFilter(t *testing.T) {
	raw := []byte("unfiltered content")
	stream := ast.NewStream(ast.PdfDict{}, raw)

	rc, err := NewStreamReader(stream)
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

	rc, err := NewStreamReader(stream)
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

	rc, err := NewStreamReader(stream)
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

	rc, err := NewStreamReader(stream)
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

func TestDecodeLZW_EarlyChange0vs1_Differ(t *testing.T) {
	// Prove that a stream encoded with EarlyChange=0 cannot be correctly
	// decoded with EarlyChange=1 once the dictionary crosses the 511 boundary.
	var input []byte
	for i := range 512 {
		input = append(input, byte(i%256))
	}

	encoded0 := lzwEncode(input, 0)
	encoded1 := lzwEncode(input, 1)

	if bytes.Equal(encoded0, encoded1) {
		t.Skip("encodings happen to be identical for this input; cannot test divergence")
	}

	// Decoding an EC=0 stream with EC=1 should produce different (wrong) output.
	wrong, err := decodeLZW(encoded0, ast.PdfDict{
		"EarlyChange": ast.PdfInt(1),
	})
	// It may error or produce wrong output — either proves they differ.
	if err == nil && bytes.Equal(wrong, input) {
		t.Error("expected EC=0 stream decoded with EC=1 to differ, but got identical output")
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

	decoded, err := decodeCCITTFax(data, parms)
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

	decoded, err := decodeCCITTFax(data, parms)
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

	decoded, err := decodeCCITTFax(data, parms)
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

	decoded, err := DecodeStream(stream)
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

	decoded, err := DecodeStream(stream)
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

	decoded, err := DecodeStream(stream)
	if err != nil {
		t.Fatalf("DecodeStream brotli failed: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Errorf("got %q, want %q", decoded, original)
	}
}

func TestBrotli_RoundTrip(t *testing.T) {
	original := []byte("litho PDF engine — Brotli round-trip test")

	encoded, err := ApplyEncodeFilter(original, "BrotliDecode")
	if err != nil {
		t.Fatalf("encode failed: %v", err)
	}

	decoded, err := applyDecodeFilter(encoded, "BrotliDecode", nil)
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

	rc, err := NewStreamReader(stream)
	if err != nil {
		t.Fatalf("NewStreamReader failed: %v", err)
	}
	defer func() {
		_ = rc.Close() //nolint:errcheck
	}()

	decoded, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll failed: %v", err)
	}
	if !bytes.Equal(decoded, original) {
		t.Errorf("got %q, want %q", decoded, original)
	}
}
