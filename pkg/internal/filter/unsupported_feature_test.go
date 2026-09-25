package filter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"os"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/plugins"
)

// jpxPart2Header is the SOC marker and SIZ marker segment of the T.800, J.10
// codestream with Rsiz set to 8000h, which signals capabilities of ISO/IEC
// 15444-2: the decoder refuses it at the SIZ marker segment.
var jpxPart2Header = []byte{
	0xFF, 0x4F, 0xFF, 0x51, 0x00, 0x29, 0x80, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x09,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x01, 0x07, 0x01, 0x01,
}

// jbig2NecessaryExtension is a JBIG2 page information segment followed by an
// extension segment flagged necessary (T.88, 7.4.14), which the decoder
// cannot be expected to know.
func jbig2NecessaryExtension() []byte {
	page := binary.BigEndian.AppendUint32(nil, 0)
	page = append(page, 48, 0, 1)
	page = binary.BigEndian.AppendUint32(page, 19)
	page = append(page, 0, 0, 0, 8, 0, 0, 0, 8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0)
	ext := binary.BigEndian.AppendUint32(nil, 1)
	ext = append(ext, 62, 0, 1)
	ext = binary.BigEndian.AppendUint32(ext, 4)
	return append(page, append(ext, 0xA0, 0, 0, 1)...)
}

// baselineJPEG returns a baseline JPEG of a 16x16 grey gradient from the
// standard library's encoder, which both DCTDecode decoders read, and the
// offset of its SOF0 marker.
func baselineJPEG(t *testing.T) ([]byte, int) {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = byte(i)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	sof := bytes.Index(data, []byte{0xFF, 0xC0})
	if sof < 0 {
		t.Fatal("the encoder wrote no SOF0 marker")
	}
	return data, sof
}

// unsupportedFeatureStreams returns, for each built-in filter that refuses a
// feature it lacks, a minimal stream using one such feature and a stream the
// same filter decodes.
func unsupportedFeatureStreams(t *testing.T) []struct {
	filter             ast.PdfName
	refused, decodable ast.PdfStream
} {
	t.Helper()
	annex := readJPX(t, "testdata/annex_j10.j2k")
	page, errP := os.ReadFile("jbig2/testdata/iso32000_jbig2_page.jb2")
	globals, errG := os.ReadFile("jbig2/testdata/iso32000_jbig2_globals.jb2")
	if errP != nil || errG != nil {
		t.Fatalf("reading the JBIG2 example: %v %v", errP, errG)
	}
	g4 := ast.PdfDict{"K": ast.PdfInt(-1), "Columns": ast.PdfInt(8), "Rows": ast.PdfInt(1)}
	baseline, sof := baselineJPEG(t)
	arithmetic := bytes.Clone(baseline)
	arithmetic[sof+1] = 0xC9
	return []struct {
		filter             ast.PdfName
		refused, decodable ast.PdfStream
	}{
		{
			filter:    "JPXDecode",
			refused:   ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, jpxPart2Header),
			decodable: ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, annex),
		},
		{
			filter:  "JBIG2Decode",
			refused: ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JBIG2Decode")}, jbig2NecessaryExtension()),
			decodable: ast.NewStream(ast.PdfDict{
				"Filter":      ast.PdfName("JBIG2Decode"),
				"DecodeParms": ast.PdfDict{"JBIG2Globals": ast.NewStream(ast.PdfDict{}, globals)},
			}, page),
		},
		{
			// A G4 row opening with the uncompressed-mode extension (T.4,
			// 4.2), then the all-white row of the control.
			filter:    "CCITTFaxDecode",
			refused:   ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("CCITTFaxDecode"), "DecodeParms": g4}, []byte{0x03, 0xC0}),
			decodable: ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("CCITTFaxDecode"), "DecodeParms": g4}, []byte{0x80, 0x10, 0x01}),
		},
		{
			// SOF9, a sequential frame coded arithmetically (T.81, Table
			// B.1), in place of the control's SOF0.
			filter:    "DCTDecode",
			refused:   ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}, arithmetic),
			decodable: ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("DCTDecode")}, baseline),
		},
		{
			filter: "Crypt",
			refused: ast.NewStream(ast.PdfDict{
				"Filter":      ast.PdfName("Crypt"),
				"DecodeParms": ast.PdfDict{"Name": ast.PdfName("OtherCF")},
			}, []byte("still encrypted")),
			decodable: ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("Crypt")}, []byte("plain")),
		},
	}
}

// TestUnsupportedFeature_OneNotion pins that "litho cannot decode this stream"
// means one thing for a built-in decoder that meets a feature it lacks:
// decoding skips the filter and reports it, [Decoder.UndecodableFilters] lists
// it, and the checked reader refuses it with [ErrUnsupportedFilter].
//
// Before, the filter name alone decided the diagnostics, so each of these
// streams was called decodable while decoding skipped its filter, and a
// caller that trusted the diagnostics read the still-encoded bytes as decoded.
// The same filter over data it decodes is the control.
func TestUnsupportedFeature_OneNotion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range unsupportedFeatureStreams(t) {
		t.Run(string(tc.filter), func(t *testing.T) {
			t.Parallel()
			var d *Decoder

			raw, err := tc.refused.Bytes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			data, report, err := d.DecodeStreamReport(ctx, tc.refused)
			if err != nil || !slices.Equal(report.Skipped, []ast.PdfName{tc.filter}) || !bytes.Equal(data, raw) {
				t.Errorf("DecodeStreamReport = %d bytes, %+v, %v; want the input still encoded, %s skipped",
					len(data), report, err, tc.filter)
			}
			bad, err := d.UndecodableFilters(ctx, tc.refused)
			if err != nil || !slices.Equal(bad, []ast.PdfName{tc.filter}) {
				t.Errorf("UndecodableFilters = %v, %v; want [%s]", bad, err, tc.filter)
			}
			if rc, err := d.NewStreamReaderChecked(ctx, tc.refused); !errors.Is(err, ErrUnsupportedFilter) {
				if rc != nil {
					rc.Close()
				}
				t.Errorf("NewStreamReaderChecked error = %v, want %v", err, ErrUnsupportedFilter)
			}
			// The lenient reader keeps handing the input on, as before.
			rc, err := d.NewStreamReader(ctx, tc.refused)
			if err != nil {
				t.Fatalf("NewStreamReader: %v", err)
			}
			lenient, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || !bytes.Equal(lenient, raw) {
				t.Errorf("NewStreamReader read %d bytes, %v; want the %d input bytes", len(lenient), err, len(raw))
			}

			data, report, err = d.DecodeStreamReport(ctx, tc.decodable)
			if err != nil || report.Incomplete() || len(data) == 0 {
				t.Fatalf("control: DecodeStreamReport = %d bytes, %+v, %v; want a complete decode", len(data), report, err)
			}
			if bad, err := d.UndecodableFilters(ctx, tc.decodable); err != nil || len(bad) != 0 {
				t.Errorf("control: UndecodableFilters = %v, %v; want none", bad, err)
			}
			rc, err = d.NewStreamReaderChecked(ctx, tc.decodable)
			if err != nil {
				t.Fatalf("control: NewStreamReaderChecked: %v", err)
			}
			checked, err := io.ReadAll(rc)
			rc.Close()
			if err != nil || !bytes.Equal(checked, data) {
				t.Errorf("control: NewStreamReaderChecked read %d bytes, %v; want the %d decoded", len(checked), err, len(data))
			}
		})
	}
}

// TestUndecodableFilters_Chains covers what the diagnostics list for a chain:
// an unknown filter found from its name without decoding, a refusal behind
// another filter, one a declining plugin hands to the built-in decoder, and
// a skip the chain then fails after, where the filters the names settle are
// listed as well.
func TestUndecodableFilters_Chains(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	declining := NewDecoder([]plugins.FilterDecoder{&mockPlugin{name: "JPXDecode", err: plugins.ErrUnsupportedFilter}}, limits.Limits{})

	for _, tc := range []struct {
		name    string
		decoder *Decoder
		stream  ast.PdfStream
		want    []ast.PdfName
	}{
		{
			name: "unknown filter",
			stream: ast.NewStream(ast.PdfDict{
				"Filter": ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("NoSuchDecode")},
			}, []byte("not deflate")),
			want: []ast.PdfName{"NoSuchDecode"},
		},
		{
			name: "refusal behind FlateDecode",
			stream: ast.NewStream(ast.PdfDict{
				"Filter": ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("JPXDecode")},
			}, zlibCompress(jpxPart2Header)),
			want: []ast.PdfName{"JPXDecode"},
		},
		{
			name:    "refusal a declining plugin hands on",
			decoder: declining,
			stream:  ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("JPXDecode")}, jpxPart2Header),
			want:    []ast.PdfName{"JPXDecode"},
		},
		{
			name: "skip, then malformed data, then an unknown filter",
			stream: ast.NewStream(ast.PdfDict{
				"Filter": ast.PdfArray{ast.PdfName("Crypt"), ast.PdfName("FlateDecode"), ast.PdfName("NoSuchDecode")},
				"DecodeParms": ast.PdfArray{
					ast.PdfDict{"Name": ast.PdfName("OtherCF")}, ast.PdfNull{}, ast.PdfNull{},
				},
			}, []byte("not deflate")),
			want: []ast.PdfName{"Crypt", "NoSuchDecode"},
		},
		{
			name: "malformed data a filter refuses as malformed",
			stream: ast.NewStream(ast.PdfDict{
				"Filter": ast.PdfName("JPXDecode"),
			}, []byte{0xFF, 0x4F, 0xFF, 0x51, 0x00}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := tc.decoder.UndecodableFilters(ctx, tc.stream)
			if err != nil || !slices.Equal(got, tc.want) {
				t.Errorf("UndecodableFilters = %v, %v; want %v", got, err, tc.want)
			}
			rc, err := tc.decoder.NewStreamReaderChecked(ctx, tc.stream)
			if rc != nil {
				rc.Close()
			}
			if len(tc.want) > 0 && !errors.Is(err, ErrUnsupportedFilter) {
				t.Errorf("NewStreamReaderChecked error = %v, want %v", err, ErrUnsupportedFilter)
			}
		})
	}
}

// TestNewStreamReaderChecked_NamesEveryUnknownFilter pins that the checked
// reader refuses a chain holding filters no decoder knows before decoding
// any filter, naming each of them and none of the others.
func TestNewStreamReaderChecked_NamesEveryUnknownFilter(t *testing.T) {
	t.Parallel()

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("FooDecode"), ast.PdfName("BarDecode")},
	}, []byte("not deflate"))
	_, err := (*Decoder)(nil).NewStreamReaderChecked(context.Background(), stream)
	if !errors.Is(err, ErrUnsupportedFilter) {
		t.Fatalf("NewStreamReaderChecked error = %v, want %v", err, ErrUnsupportedFilter)
	}
	msg := err.Error()
	if !bytes.Contains([]byte(msg), []byte("FooDecode, BarDecode")) || bytes.Contains([]byte(msg), []byte("FlateDecode")) {
		t.Errorf("error %q does not name exactly FooDecode and BarDecode", msg)
	}
}

// TestUndecodableFilters_Cancelled reports a cancelled context instead of an
// answer, whether or not the chain needs decoding.
func TestUndecodableFilters_Cancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, f := range []ast.PdfName{"NoSuchDecode", "JPXDecode"} {
		stream := ast.NewStream(ast.PdfDict{"Filter": f}, jpxPart2Header)
		if got, err := (*Decoder)(nil).UndecodableFilters(ctx, stream); !errors.Is(err, context.Canceled) || got != nil {
			t.Errorf("%s: UndecodableFilters = %v, %v; want %v", f, got, err, context.Canceled)
		}
	}
}
