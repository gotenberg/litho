package filter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// salvageCase is a stream whose data a filter decodes whole, salvages whole
// despite a defect, or keeps only the prefix of.
type salvageCase struct {
	name   string
	filter ast.PdfName
	data   []byte
	// plain is what the stream decodes to by default.
	plain []byte
	// salvaged reports that the data breaks a rule of its format and decodes
	// whole all the same.
	salvaged bool
	// damaged reports that only a prefix of plain decodes.
	damaged bool
}

func salvageCases(t *testing.T) []salvageCase {
	t.Helper()
	full := zlibCompress(damagePlain)
	badSum := bytes.Clone(full)
	badSum[len(badSum)-1] ^= 0xFF
	noSum := full[:len(full)-4]
	shortSum := full[:len(full)-2]
	raw := rawDeflate(t, damagePlain)
	flushed := syncFlushed(t, damagePlain)
	literals := damagePlain[:200]
	return []salvageCase{
		{name: "flate whole", filter: "FlateDecode", data: full, plain: damagePlain},
		{name: "flate bad checksum", filter: "FlateDecode", data: badSum, plain: damagePlain, salvaged: true},
		{name: "flate checksum missing", filter: "FlateDecode", data: noSum, plain: damagePlain, salvaged: true},
		{name: "flate checksum cut", filter: "FlateDecode", data: shortSum, plain: damagePlain, salvaged: true},
		{name: "flate header damaged", filter: "FlateDecode", data: raw, plain: damagePlain, salvaged: true},
		{name: "flate stopped at a sync flush", filter: "FlateDecode", data: flushed, plain: damagePlain, salvaged: true},
		{name: "flate truncated", filter: "FlateDecode", data: full[:len(full)/2], plain: damagePlain, damaged: true},
		{name: "flate truncated without header", filter: "FlateDecode", data: raw[:len(raw)/2], plain: damagePlain, damaged: true},
		{name: "lzw whole", filter: "LZWDecode", data: lzwLiterals(t, literals, lzwEODCode), plain: literals},
		{name: "lzw EOD missing", filter: "LZWDecode", data: lzwLiterals(t, literals), plain: literals, salvaged: true},
		{name: "lzw code out of range", filter: "LZWDecode", data: lzwLiterals(t, literals, 511), plain: literals, damaged: true},
	}
}

// TestDecodeStreamReport_Salvaged pins that a filter which decodes data whole
// despite a defect of its format, a zlib checksum missing or wrong, a damaged
// zlib header, deflate data stopped at a sync flush, or LZWDecode data without
// its EOD code, records that it salvaged the stream, without changing the
// bytes it hands on. Before, such a stream was indistinguishable from a whole
// one.
func TestDecodeStreamReport_Salvaged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range salvageCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stream := ast.NewStream(ast.PdfDict{"Filter": tc.filter}, tc.data)
			got, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, stream)
			if err != nil {
				t.Fatalf("DecodeStreamReport: %v", err)
			}
			if tc.damaged {
				if len(got) == 0 || !bytes.HasPrefix(tc.plain, got) {
					t.Fatalf("got %d bytes, want a non-empty prefix of %d", len(got), len(tc.plain))
				}
			} else if !bytes.Equal(got, tc.plain) {
				t.Fatalf("got %d bytes, want the %d plain bytes", len(got), len(tc.plain))
			}
			if want := tc.salvaged; slices.Equal(report.Salvaged, []ast.PdfName{tc.filter}) != want {
				t.Errorf("Salvaged = %v, want salvaged %t", report.Salvaged, want)
			}
			if want := tc.damaged; slices.Equal(report.Damaged, []ast.PdfName{tc.filter}) != want {
				t.Errorf("Damaged = %v, want damaged %t", report.Damaged, want)
			}
			if report.Incomplete() != tc.damaged {
				t.Errorf("Incomplete = %t, want %t", report.Incomplete(), tc.damaged)
			}
			if report.Clean() != (!tc.damaged && !tc.salvaged) {
				t.Errorf("Clean = %t", report.Clean())
			}
		})
	}
}

// TestRefuseSalvaged pins that a decoder refusing salvaged data fails every
// stream a filter salvaged or kept only the prefix of with
// [ErrSalvagedStream], eagerly and through the streaming reader, and still
// decodes a whole stream.
func TestRefuseSalvaged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	strict := NewDecoder(nil, limits.Default()).RefuseSalvaged()
	for _, tc := range salvageCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stream := ast.NewStream(ast.PdfDict{"Filter": tc.filter}, tc.data)
			refused := tc.salvaged || tc.damaged

			got, err := strict.DecodeStream(ctx, stream)
			if refused {
				if !errors.Is(err, ErrSalvagedStream) {
					t.Errorf("DecodeStream: err = %v, want ErrSalvagedStream", err)
				}
			} else if err != nil || !bytes.Equal(got, tc.plain) {
				t.Errorf("DecodeStream: %d bytes, err = %v", len(got), err)
			}

			for _, open := range []func(context.Context, ast.PdfStream) (io.ReadCloser, error){strict.NewStreamReader, strict.NewStreamReaderChecked} {
				got, err := readAll(ctx, open, stream)
				if refused {
					if !errors.Is(err, ErrSalvagedStream) {
						t.Errorf("stream reader: err = %v, want ErrSalvagedStream", err)
					}
				} else if err != nil || !bytes.Equal(got, tc.plain) {
					t.Errorf("stream reader: %d bytes, err = %v", len(got), err)
				}
			}
		})
	}
}

// TestNewStreamReader_SalvageUnchanged pins that the default streaming reader
// hands on the bytes the eager decode hands on for every salvaged stream, and
// records the salvage for its caller.
func TestNewStreamReader_SalvageUnchanged(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range salvageCases(t) {
		if tc.damaged && tc.filter == "LZWDecode" {
			// The streaming LZW reader fails on a code its table lacks rather
			// than keep the prefix, as it did before salvage was recorded.
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stream := ast.NewStream(ast.PdfDict{"Filter": tc.filter}, tc.data)
			want, err := DecodeStream(ctx, stream)
			if err != nil {
				t.Fatal(err)
			}
			rc, err := NewStreamReader(ctx, stream)
			if err != nil {
				t.Fatal(err)
			}
			got, rerr := io.ReadAll(rc)
			cerr := rc.Close()
			if rerr != nil || cerr != nil {
				t.Fatalf("read: %v, close: %v", rerr, cerr)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("streamed %d bytes, eager %d", len(got), len(want))
			}
			s, ok := rc.(interface{ Salvage() error })
			if !ok {
				t.Fatalf("%T records no salvage", rc)
			}
			if (s.Salvage() != nil) != (tc.salvaged || tc.damaged) {
				t.Errorf("Salvage() = %v", s.Salvage())
			}
		})
	}
}

func readAll(ctx context.Context, open func(context.Context, ast.PdfStream) (io.ReadCloser, error), stream ast.PdfStream) ([]byte, error) {
	rc, err := open(ctx, stream)
	if err != nil {
		return nil, err
	}
	got, err := io.ReadAll(rc)
	if cerr := rc.Close(); err == nil {
		err = cerr
	}
	return got, err
}

// TestSalvagedFilters pins the per-stream diagnostic: the filters of the chain
// whose data was salvaged or cut at damage, found without decoding past an
// image codec.
func TestSalvagedFilters(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range salvageCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stream := ast.NewStream(ast.PdfDict{"Filter": ast.PdfArray{tc.filter, ast.PdfName("DCTDecode")}}, tc.data)
			got, err := (*Decoder)(nil).SalvagedFilters(ctx, stream)
			if err != nil {
				t.Fatal(err)
			}
			want := []ast.PdfName(nil)
			if tc.salvaged || tc.damaged {
				want = []ast.PdfName{tc.filter}
			}
			if !slices.Equal(got, want) {
				t.Errorf("SalvagedFilters = %v, want %v", got, want)
			}
		})
	}
}
