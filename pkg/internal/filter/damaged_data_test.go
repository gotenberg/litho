package filter

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/andybalholm/brotli"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// damagePlain is text long enough that a cut of its encoding lands inside a
// block, with output decoded before it.
var damagePlain = bytes.Repeat([]byte("litho keeps what a filter decoded before the damage. "), 200)

// syncFlushed returns plain deflated with zlib and flushed, without the final
// block or the checksum: the data a producer that never closes its encoder
// writes.
func syncFlushed(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// rawDeflate returns plain as raw deflate data, with no zlib framing.
func rawDeflate(t *testing.T, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := flate.NewWriter(&buf, flate.DefaultCompression)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// lzwLiterals returns LZWDecode data (ISO 32000-1, 7.4.4.2) coding each byte
// of plain as its literal code, then the codes of tail, packed nine bits a
// code from the most significant bit and padded with zero bits to the byte.
// It writes no EOD code of its own. Fewer than 253 codes keep the code width
// at nine bits.
func lzwLiterals(t *testing.T, plain []byte, tail ...uint16) []byte {
	t.Helper()
	codes := make([]uint16, 0, len(plain)+len(tail))
	for _, b := range plain {
		codes = append(codes, uint16(b))
	}
	codes = append(codes, tail...)
	if len(codes) > 252 {
		t.Fatalf("%d codes widen past nine bits", len(codes))
	}
	var out []byte
	var acc uint32
	var n uint
	for _, c := range codes {
		acc = acc<<9 | uint32(c)
		for n += 9; n >= 8; n -= 8 {
			out = append(out, byte(acc>>(n-8)))
		}
	}
	if n > 0 {
		out = append(out, byte(acc<<(8-n)))
	}
	return out
}

// TestDecodeStreamReport_FlateLZWAndBrotliDamage pins what the chain reports
// for FlateDecode, LZWDecode and BrotliDecode data damaged after some of it
// decodes: the bytes decoded before the damage come back, and the filter is
// reported damaged, as an ASCII filter is. Data that is whole but for a
// missing end marker or checksum is reported complete, as is deflate data a
// producer flushed and never finished.
//
// Before, every such stream came back as if it had decoded completely.
func TestDecodeStreamReport_FlateLZWAndBrotliDamage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	full := zlibCompress(damagePlain)
	badSum := bytes.Clone(full)
	badSum[len(badSum)-1] ^= 0xFF
	flushed := syncFlushed(t, damagePlain)
	raw := rawDeflate(t, damagePlain)
	literals := damagePlain[:200]

	// The flush ends a meta-block halfway, so a cut after it keeps the first
	// half: the decoder hands on no byte of a meta-block it has not finished.
	var br bytes.Buffer
	bw := brotli.NewWriter(&br)
	half := len(damagePlain) / 2
	if _, err := bw.Write(damagePlain[:half]); err != nil {
		t.Fatal(err)
	}
	if err := bw.Flush(); err != nil {
		t.Fatal(err)
	}
	flushedAt := br.Len()
	if _, err := bw.Write(damagePlain[half:]); err != nil {
		t.Fatal(err)
	}
	if err := bw.Close(); err != nil {
		t.Fatal(err)
	}
	brotliFull := br.Bytes()

	for _, tc := range []struct {
		name    string
		filter  ast.PdfName
		data    []byte
		plain   []byte
		damaged bool
		// whole reports that the output is all of plain; otherwise it is a
		// non-empty strict prefix of it.
		whole bool
	}{
		{name: "flate cut inside a block", filter: "FlateDecode", data: full[:len(full)/2], plain: damagePlain, damaged: true},
		// A block type of 11 is reserved (RFC 1951, 3.2.3), and 0x07 starts
		// a final block of that type after the flush.
		{name: "flate corrupt after a flush", filter: "FlateDecode", data: slices.Concat(flushed, []byte{0x07}), plain: damagePlain, damaged: true, whole: true},
		{name: "flate stopped at a sync flush", filter: "FlateDecode", data: flushed, plain: damagePlain, whole: true},
		{name: "flate stopped at a sync flush, line feed counted", filter: "FlateDecode", data: slices.Concat(flushed, []byte("\r\n")), plain: damagePlain, whole: true},
		{name: "flate without its checksum", filter: "FlateDecode", data: full[:len(full)-4], plain: damagePlain, whole: true},
		{name: "flate with a wrong checksum", filter: "FlateDecode", data: badSum, plain: damagePlain, whole: true},
		{name: "raw deflate cut inside a block", filter: "Fl", data: raw[:len(raw)/2], plain: damagePlain, damaged: true},
		{name: "lzw with a code past its table", filter: "LZWDecode", data: lzwLiterals(t, literals, 511), plain: literals, damaged: true, whole: true},
		{name: "lzw without its EOD", filter: "LZW", data: lzwLiterals(t, literals), plain: literals, whole: true},
		{name: "lzw with its EOD", filter: "LZWDecode", data: lzwLiterals(t, literals, lzwEODCode), plain: literals, whole: true},
		{name: "brotli cut short", filter: "BrotliDecode", data: brotliFull[:(flushedAt+len(brotliFull))/2], plain: damagePlain, damaged: true},
		{name: "brotli whole", filter: "BrotliDecode", data: brotliFull, plain: damagePlain, whole: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stream := ast.NewStream(ast.PdfDict{"Filter": tc.filter}, tc.data)
			data, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, stream)
			if err != nil {
				t.Fatalf("DecodeStreamReport error = %v, want the decoded bytes", err)
			}
			switch {
			case tc.whole && !bytes.Equal(data, tc.plain):
				t.Errorf("decoded %d bytes, want the whole %d", len(data), len(tc.plain))
			case !tc.whole && (len(data) == 0 || len(data) >= len(tc.plain) || !bytes.HasPrefix(tc.plain, data)):
				t.Errorf("decoded %d bytes, want a strict prefix of the %d", len(data), len(tc.plain))
			}
			var want []ast.PdfName
			if tc.damaged {
				want = []ast.PdfName{tc.filter}
			}
			if !slices.Equal(report.Damaged, want) || len(report.Skipped) != 0 {
				t.Errorf("report = %+v, want damaged %v and nothing skipped", report, want)
			}
			if _, incomplete, err := DecodeStreamChecked(ctx, stream); err != nil || incomplete != tc.damaged {
				t.Errorf("DecodeStreamChecked incomplete = %v, %v; want %v", incomplete, err, tc.damaged)
			}
			// DecodeStream hands back the same bytes with no error, as before.
			if got, err := DecodeStream(ctx, stream); err != nil || !bytes.Equal(got, data) {
				t.Errorf("DecodeStream = %d bytes, %v; want the %d reported", len(got), err, len(data))
			}
			// A damaged filter decodes, so it is not an undecodable one.
			if bad, err := (*Decoder)(nil).UndecodableFilters(ctx, stream); err != nil || len(bad) != 0 {
				t.Errorf("UndecodableFilters = %v, %v; want none", bad, err)
			}
			// A later filter reads the bytes kept, as DecodeStreamUpTo hands
			// them on.
			chained := ast.NewStream(ast.PdfDict{"Filter": ast.PdfArray{tc.filter, ast.PdfName("NoSuchDecode")}}, tc.data)
			if upTo, found, err := DecodeStreamUpTo(ctx, chained, "NoSuchDecode"); err != nil || !found || !bytes.Equal(upTo, data) {
				t.Errorf("DecodeStreamUpTo = %d bytes, %v, %v; want the %d kept", len(upTo), found, err, len(data))
			}
		})
	}
}

// TestDecodeStreamReport_DamageBeforeAnyByte pins that damage met before
// anything decodes still fails the stream, with the error it failed with
// before, and not as damage.
func TestDecodeStreamReport_DamageBeforeAnyByte(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		filter ast.PdfName
		data   []byte
		want   error
	}{
		{"flate header only", "FlateDecode", zlibCompress(damagePlain)[:2], io.ErrUnexpectedEOF},
		// Code 300 as the first code, where only a literal may stand.
		{"lzw non-literal first", "LZWDecode", []byte{0x96, 0x00, 0x00}, ErrLZWNonLiteralAfterClear},
	} {
		stream := ast.NewStream(ast.PdfDict{"Filter": tc.filter}, tc.data)
		data, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, stream)
		if !errors.Is(err, tc.want) || data != nil || errors.Is(err, errDamagedData) || report.Incomplete() {
			t.Errorf("%s: DecodeStreamReport = %d bytes, %+v, %v; want %v", tc.name, len(data), report, err, tc.want)
		}
	}
}

// TestDecodeStreamReport_DamagedFlateKeepsItsPredictor pins that the bytes
// FlateDecode keeps before damage go through the stream's predictor, as the
// whole stream's do, and the filter is still reported damaged.
func TestDecodeStreamReport_DamagedFlateKeepsItsPredictor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Rows of four bytes under the PNG Up predictor, each adding one to the
	// row above, varied enough that the deflate data spans many bytes.
	var rows []byte
	for i := range 400 {
		rows = append(rows, 2, byte(i), byte(i*7), byte(i*13), byte(i*31))
	}
	dict := ast.PdfDict{
		"Filter":      ast.PdfName("FlateDecode"),
		"DecodeParms": ast.PdfDict{"Predictor": ast.PdfInt(12), "Columns": ast.PdfInt(4)},
	}
	compressed := zlibCompress(rows)
	whole, err := DecodeStream(ctx, ast.NewStream(dict, compressed))
	if err != nil {
		t.Fatal(err)
	}
	data, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, ast.NewStream(dict, compressed[:len(compressed)/2]))
	if err != nil || !slices.Equal(report.Damaged, []ast.PdfName{"FlateDecode"}) {
		t.Fatalf("DecodeStreamReport = %d bytes, %+v, %v; want FlateDecode damaged", len(data), report, err)
	}
	if len(data) == 0 || len(data) >= len(whole) || !bytes.HasPrefix(whole, data) {
		t.Errorf("decoded %d bytes, want a strict prefix of the %d predicted", len(data), len(whole))
	}
}

// TestZlibHeaderLen_AgreesWithCompressZlib pins that the header check the
// FlateDecode filter reads with accepts exactly the headers compress/zlib's
// reader accepts, which decided before whether data was read as zlib or as
// raw deflate: every two-byte header, whole and cut to one byte, and each
// naming a preset dictionary with the identifier of none and of another.
func TestZlibHeaderLen_AgreesWithCompressZlib(t *testing.T) {
	t.Parallel()

	for h := range 1 << 16 {
		var data [6]byte
		binary.BigEndian.PutUint16(data[:], uint16(h)) //nolint:gosec // h is below 1<<16.
		cases := [][]byte{data[:1], data[:2]}
		if h&0x20 != 0 {
			for _, dictID := range []uint32{1, 0xDEADBEEF} {
				withID := data
				binary.BigEndian.PutUint32(withID[2:], dictID)
				cases = append(cases, withID[:5], withID[:])
			}
		}
		for _, in := range cases {
			_, zerr := zlib.NewReader(bytes.NewReader(in))
			n, err := zlibHeaderLen(in)
			if (err == nil) != (zerr == nil) || (err != nil && !errors.Is(err, zerr)) || (err == nil && n != 2+4*min(1, h&0x20)) {
				t.Fatalf("header %x: zlibHeaderLen = %d, %v; compress/zlib says %v", in, n, err, zerr)
			}
		}
	}
}

// TestDecodeFlateDecode_SameBytesAsCompressZlib pins that the FlateDecode
// filter hands back the bytes and the failures it handed back when it read
// through compress/zlib, for every cut of a zlib stream, of raw deflate data
// and of a sync-flushed stream, and for a flipped bit at every position: only
// the report of damage changed.
func TestDecodeFlateDecode_SameBytesAsCompressZlib(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// A text that repeats and one that does not, so the deflate data holds
	// both matches and literals.
	noise := make([]byte, 300)
	x := uint32(1)
	for i := range noise {
		x = x*1103515245 + 12345
		noise[i] = byte(x >> 16)
	}
	inputs := [][]byte{nil, {0x78}, zlibCompress(nil), zlibCompress([]byte{0})}
	for _, plain := range [][]byte{damagePlain[:600], noise} {
		for _, whole := range [][]byte{zlibCompress(plain), rawDeflate(t, plain), syncFlushed(t, plain)} {
			for n := 0; n <= len(whole); n++ {
				inputs = append(inputs, whole[:n])
			}
			for i := range whole {
				for bit := range 8 {
					flipped := bytes.Clone(whole)
					flipped[i] ^= 1 << bit
					inputs = append(inputs, flipped)
				}
			}
		}
	}
	for _, in := range inputs {
		got, err := decodeFlateDecode(ctx, in, limits.DefaultMaxStreamBytes)
		want, wantErr := zlibReference(in)
		if errors.Is(err, errDamagedData) || errors.Is(err, errSalvagedData) {
			err = nil
		}
		if !bytes.Equal(got, want) || (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
			t.Fatalf("input %x: decodeFlateDecode = %d bytes, %v; compress/zlib gave %d bytes, %v", in, len(got), err, len(want), wantErr)
		}
	}
}

// zlibReference decodes FlateDecode data the way the filter did through
// compress/zlib: every byte that inflated, with no error, and an error only
// when nothing inflated.
func zlibReference(data []byte) ([]byte, error) {
	r, _, err := openFlate(data)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, r)
	closeErr := r.Close()
	switch {
	case buf.Len() > 0:
		return buf.Bytes(), nil
	case copyErr != nil && copyErr != io.ErrUnexpectedEOF:
		return nil, copyErr
	case closeErr != nil:
		return nil, errors.New("failed to close zlib reader: " + closeErr.Error())
	}
	return []byte{}, nil
}

// TestDecodeStreamReport_CCITTUncompressedModeAfterTheFirstRow pins that a
// CCITTFaxDecode stream whose second row enters uncompressed mode hands on
// its first row and is reported incomplete, as damaged data is, where it came
// back as if the one row were the whole image. It is not a refusal: the
// probe, which reads the first row, and the diagnostics find nothing litho
// cannot decode, and the checked reader reads the row.
func TestDecodeStreamReport_CCITTUncompressedModeAfterTheFirstRow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Horizontal white 3 black 2, then V0 to the edge (ITU-T T.4, 4.2),
	// followed by the extension code that opens uncompressed mode.
	parms := ast.PdfDict{"K": ast.PdfInt(-1), "Columns": ast.PdfInt(8), "Rows": ast.PdfInt(2)}
	stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("CCITTFaxDecode"), "DecodeParms": parms}, []byte{0x31, 0xC0, 0xF0})
	firstRow := []byte{0b11100111}

	var d *Decoder
	data, report, err := d.DecodeStreamReport(ctx, stream)
	if err != nil || !bytes.Equal(data, firstRow) {
		t.Fatalf("DecodeStreamReport = %08b, %v; want the first row", data, err)
	}
	if want := []ast.PdfName{"CCITTFaxDecode"}; !slices.Equal(report.Damaged, want) || len(report.Skipped) != 0 {
		t.Errorf("report = %+v, want %v damaged and nothing skipped", report, want)
	}

	raw, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := codecProbes["CCITTFaxDecode"](ctx, raw, parms, stream.Dict, limits.Default()); err != nil {
		t.Errorf("probe = %v, want nil", err)
	}
	if bad, err := d.UndecodableFilters(ctx, stream); err != nil || len(bad) != 0 {
		t.Errorf("UndecodableFilters = %v, %v; want none", bad, err)
	}
	rc, err := d.NewStreamReaderChecked(ctx, stream)
	if err != nil {
		t.Fatalf("NewStreamReaderChecked: %v", err)
	}
	defer rc.Close()
	if got, err := io.ReadAll(rc); err != nil || !bytes.Equal(got, firstRow) {
		t.Errorf("checked reader = %08b, %v; want the first row", got, err)
	}
}
