package filter

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	pdfast "github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// jpxMainSegment returns the offset of the first marker segment m in the
// main header of codestream cs, or -1 when the main header has none.
func jpxMainSegment(t *testing.T, cs []byte, m uint16) int {
	t.Helper()
	for off := 2; off+4 <= len(cs); {
		marker := binary.BigEndian.Uint16(cs[off:])
		if marker == m {
			return off
		}
		if marker == 0xFF90 { // SOT ends the main header.
			return -1
		}
		off += 2 + int(binary.BigEndian.Uint16(cs[off+2:]))
	}
	t.Fatalf("codestream ends inside its main header")
	return -1
}

// annexJ10 returns the codestream of ITU-T T.800, J.10, which the JPXDecode
// decoder decodes, with edit applied to a copy of it.
func annexJ10(t *testing.T, edit func(cs []byte) []byte) []byte {
	t.Helper()
	return edit(slices.Clone(readJPX(t, "testdata/annex_j10.j2k")))
}

// jpxInsertAfterSIZ inserts the marker segment seg after the SIZ marker
// segment of cs. The codestream is bare, so nothing else counts its length.
func jpxInsertAfterSIZ(t *testing.T, cs, seg []byte) []byte {
	t.Helper()
	siz := jpxMainSegment(t, cs, 0xFF51)
	end := siz + 2 + int(binary.BigEndian.Uint16(cs[siz+2:]))
	return slices.Concat(cs[:end], seg, cs[end:])
}

// jp2Box returns a JP2 box of type kind holding body (T.800, I.4).
func jp2Box(kind string, body ...[]byte) []byte {
	b := slices.Concat(body...)
	return slices.Concat(binary.BigEndian.AppendUint32(nil, uint32(8+len(b))), []byte(kind), b) //nolint:gosec // test boxes are a few hundred bytes.
}

// refusalRow is one construct a codec decoder refuses as a feature it lacks:
// the function that refuses it, and a stream whose data is sound but for it.
type refusalRow struct {
	// site names the function whose fmt.Errorf wraps the codec package's
	// ErrUnsupported, as "package: function" or "package: type.method".
	site string
	// refusal is a part of the site's error message, which tells that site
	// refused the stream rather than another.
	refusal string
	filter  pdfast.PdfName
	stream  pdfast.PdfStream
	// midStream marks a refusal the decoder meets only in the coded data,
	// which the probe reports unsettled and the diagnostics decode to find.
	midStream bool
}

// refusalRows returns one row per site where a codec decoder refuses a
// feature it lacks, and one for the Crypt filter.
func refusalRows(t *testing.T) []refusalRow {
	t.Helper()
	jpxStream := func(data []byte) pdfast.PdfStream {
		return pdfast.NewStream(pdfast.PdfDict{"Filter": pdfast.PdfName("JPXDecode")}, data)
	}
	g4 := pdfast.PdfDict{"K": pdfast.PdfInt(-1), "Columns": pdfast.PdfInt(8), "Rows": pdfast.PdfInt(1)}
	dctStream := func(edit func(j []byte, sof, sofLen int) []byte) pdfast.PdfStream {
		j, sof := baselineJPEG(t)
		sofLen := 2 + int(binary.BigEndian.Uint16(j[sof+2:]))
		return pdfast.NewStream(pdfast.PdfDict{"Filter": pdfast.PdfName("DCTDecode")}, edit(j, sof, sofLen))
	}
	return []refusalRow{
		{
			// T.800, A.5.2: a CAP marker segment in the main header.
			site:    "jpx: parseCodestream",
			refusal: "extended capabilities (CAP)",
			filter:  "JPXDecode",
			stream: jpxStream(annexJ10(t, func(cs []byte) []byte {
				return jpxInsertAfterSIZ(t, cs, []byte{0xFF, 0x50, 0x00, 0x06, 0, 0, 0, 0})
			})),
		},
		{
			// T.800, Table A.10: Rsiz signalling ISO/IEC 15444-2.
			site:    "jpx: parseSIZ",
			refusal: "Rsiz 0x8000",
			filter:  "JPXDecode",
			stream: jpxStream(annexJ10(t, func(cs []byte) []byte {
				siz := jpxMainSegment(t, cs, 0xFF51)
				cs[siz+4] = 0x80
				return cs
			})),
		},
		{
			// T.800, Table A.11: a component 32 bits deep.
			site:    "jpx: newImage",
			refusal: "component 0 is 32 bits deep",
			filter:  "JPXDecode",
			stream: jpxStream(annexJ10(t, func(cs []byte) []byte {
				siz := jpxMainSegment(t, cs, 0xFF51)
				cs[siz+4+36] = 0x1F
				return cs
			})),
		},
		{
			// T.800, I.5.3.4 and I.5.3.5: a JP2 file whose one channel looks
			// up a palette column 32 bits deep.
			site:    "jpx: Image.checkChannels",
			refusal: "palette column of 32 bits",
			filter:  "JPXDecode",
			stream: jpxStream(slices.Concat(
				jp2Box("jP  ", []byte{0x0D, 0x0A, 0x87, 0x0A}),
				jp2Box("jp2h",
					jp2Box("pclr", []byte{0, 1, 1, 0x1F, 0, 0, 0, 0}),
					jp2Box("cmap", []byte{0, 0, 1, 0})),
				jp2Box("jp2c", readJPX(t, "testdata/annex_j10.j2k")),
			)),
		},
		{
			// T.800, E-2: seven guard bits and an exponent of 31 give the
			// LL sub-band 37 magnitude bit-planes on the reversible path.
			site:    "jpx: tileComponent.checkReversible",
			refusal: "reversible sub-band of 37 magnitude bit-planes",
			filter:  "JPXDecode",
			stream: jpxStream(annexJ10(t, func(cs []byte) []byte {
				qcd := jpxMainSegment(t, cs, 0xFF5C)
				cs[qcd+4], cs[qcd+5] = 0xE0, 0xF8
				return cs
			})),
		},
		{
			// T.800, A.6.3: an ROI shift of 255 puts every code-block past
			// the decoder's bit-planes, refused at the first one a packet
			// gives a coding pass.
			site:    "jpx: t1.decode",
			refusal: "code-block of 264 magnitude bit-planes",
			filter:  "JPXDecode",
			stream: jpxStream(annexJ10(t, func(cs []byte) []byte {
				return jpxInsertAfterSIZ(t, cs, []byte{0xFF, 0x5E, 0x00, 0x05, 0, 0, 0xFF})
			})),
			midStream: true,
		},
		{
			// T.88, 7.4.14: an extension segment flagged necessary.
			site:    "jbig2: checkExtension",
			refusal: "necessary extension",
			filter:  "JBIG2Decode",
			stream:  pdfast.NewStream(pdfast.PdfDict{"Filter": pdfast.PdfName("JBIG2Decode")}, jbig2NecessaryExtension()),
		},
		{
			// T.4, 4.2: a G4 row opening with the uncompressed-mode extension.
			site:    "ccitt: decoder.row2D",
			refusal: "uncompressed mode",
			filter:  "CCITTFaxDecode",
			stream:  pdfast.NewStream(pdfast.PdfDict{"Filter": pdfast.PdfName("CCITTFaxDecode"), "DecodeParms": g4}, []byte{0x03, 0xC0}),
		},
		{
			// T.81, Table B.1: SOF9, a sequential frame coded arithmetically.
			site:    "dct: decoder.decodeSegments",
			refusal: "frame 0xC9",
			filter:  "DCTDecode",
			stream: dctStream(func(j []byte, sof, _ int) []byte {
				j[sof+1] = 0xC9
				return j
			}),
		},
		{
			// T.81, B.2.2: an extended sequential frame of 12-bit samples.
			site:    "dct: decoder.frame",
			refusal: "12-bit samples",
			filter:  "DCTDecode",
			stream: dctStream(func(j []byte, sof, _ int) []byte {
				j[sof+1], j[sof+4] = 0xC1, 12
				return j
			}),
		},
		{
			// T.81, B.3: a second frame, before any scan.
			site:    "dct: decoder.frame",
			refusal: "a second frame",
			filter:  "DCTDecode",
			stream: dctStream(func(j []byte, sof, sofLen int) []byte {
				return slices.Concat(j[:sof+sofLen], j[sof:sof+sofLen], j[sof+sofLen:])
			}),
		},
		{
			// T.81, B.3: a second frame after the first scan, met only past
			// the coded data the probe does not read.
			site:    "dct: decoder.frame",
			refusal: "a second frame",
			filter:  "DCTDecode",
			stream: dctStream(func(j []byte, sof, sofLen int) []byte {
				eoi := len(j) - 2
				return slices.Concat(j[:eoi], j[sof:sof+sofLen], j[eoi:])
			}),
			midStream: true,
		},
		{
			// ISO 32000-1, 7.4.10: a crypt filter other than Identity.
			site:    "filter: decodeCrypt",
			refusal: "sub-filter \"OtherCF\"",
			filter:  "Crypt",
			stream: pdfast.NewStream(pdfast.PdfDict{
				"Filter":      pdfast.PdfName("Crypt"),
				"DecodeParms": pdfast.PdfDict{"Name": pdfast.PdfName("OtherCF")},
			}, []byte("still encrypted")),
		},
	}
}

// TestProbes_AgreeWithDecoders pins, for every site where a built-in decoder
// refuses a feature it lacks, that a stream sound but for that feature is
// refused by the decoder and flagged by what the diagnostics consult instead
// of decoding: the codec's probe, and the chain the probes run. A refusal the
// decoder meets only in the coded data is unsettled for the probe, and the
// diagnostics decode the stream to find it. Either way
// [Decoder.UndecodableFilters] lists the filter the decoder skips.
func TestProbes_AgreeWithDecoders(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, row := range refusalRows(t) {
		t.Run(row.site, func(t *testing.T) {
			t.Parallel()
			var d *Decoder
			want := []pdfast.PdfName{row.filter}

			_, report, err := d.DecodeStreamReport(ctx, row.stream)
			if err != nil || !slices.Equal(report.Skipped, want) {
				t.Fatalf("decoder: DecodeStreamReport = %+v, %v; want %s skipped", report, err, row.filter)
			}

			if probe, isCodec := codecProbes[row.filter]; isCodec {
				data, err := row.stream.Bytes(ctx)
				if err != nil {
					t.Fatal(err)
				}
				parms := extractDecodeParms(row.stream.Dict, 1)[0]
				err = probe(ctx, data, parms, row.stream.Dict, limits.Default())
				switch {
				case row.midStream && !errors.Is(err, errUnsettled):
					t.Errorf("probe = %v, want %v", err, errUnsettled)
				case !row.midStream && (!errors.Is(err, ErrUnsupportedFilter) || !strings.Contains(err.Error(), row.refusal)):
					t.Errorf("probe = %v, want %v for %q", err, ErrUnsupportedFilter, row.refusal)
				}
			}

			skipped, err := probeChain(ctx, row.stream, d)
			switch {
			case row.midStream && !errors.Is(err, errUnsettled):
				t.Errorf("probeChain = %v, %v; want %v", skipped, err, errUnsettled)
			case !row.midStream && (err != nil || !slices.Equal(skipped, want)):
				t.Errorf("probeChain = %v, %v; want %v", skipped, err, want)
			}

			if bad, err := d.UndecodableFilters(ctx, row.stream); err != nil || !slices.Equal(bad, want) {
				t.Errorf("UndecodableFilters = %v, %v; want %v", bad, err, want)
			}
			rc, err := d.NewStreamReaderChecked(ctx, row.stream)
			if rc != nil {
				rc.Close()
			}
			if !errors.Is(err, ErrUnsupportedFilter) || !strings.Contains(err.Error(), row.refusal) {
				t.Errorf("NewStreamReaderChecked error = %v, want %v for %q", err, ErrUnsupportedFilter, row.refusal)
			}
		})
	}
}

// TestProbes_EveryRefusalSiteHasARow pins that the rows of
// TestProbes_AgreeWithDecoders cover every site where a codec package wraps
// its ErrUnsupported, so a refusal added to a decoder fails here until its
// probe handles it and a row proves the two agree.
func TestProbes_EveryRefusalSiteHasARow(t *testing.T) {
	t.Parallel()

	rows := map[string]bool{}
	for _, row := range refusalRows(t) {
		rows[row.site] = true
	}
	for _, pkg := range []string{"jpx", "jbig2", "ccitt", "dct"} {
		sites := unsupportedSites(t, pkg)
		if len(sites) == 0 {
			t.Fatalf("%s: found no site wrapping ErrUnsupported; the scan is broken", pkg)
		}
		for _, site := range sites {
			if !rows[site] {
				t.Errorf("%s wraps ErrUnsupported and no row proves the probe flags it", site)
			}
		}
		for site := range rows {
			if strings.HasPrefix(site, pkg+": ") && !slices.Contains(sites, site) {
				t.Errorf("row %q names no site of package %s", site, pkg)
			}
		}
	}
}

// unsupportedSites returns, as "pkg: function" or "pkg: type.method", every
// function of package directory pkg whose body calls fmt.Errorf with the
// package's ErrUnsupported among the arguments.
func unsupportedSites(t *testing.T, pkg string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	var sites []string
	fset := token.NewFileSet()
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !wrapsUnsupported(fn.Body) {
				continue
			}
			site := fn.Name.Name
			if fn.Recv != nil && len(fn.Recv.List) == 1 {
				recv := fn.Recv.List[0].Type
				if star, isStar := recv.(*ast.StarExpr); isStar {
					recv = star.X
				}
				if id, isIdent := recv.(*ast.Ident); isIdent {
					site = id.Name + "." + site
				}
			}
			if s := fmt.Sprintf("%s: %s", pkg, site); !slices.Contains(sites, s) {
				sites = append(sites, s)
			}
		}
	}
	return sites
}

// wrapsUnsupported reports whether body calls fmt.Errorf with the identifier
// ErrUnsupported among its arguments.
func wrapsUnsupported(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return !found
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Errorf" {
			return true
		}
		if pkg, isIdent := sel.X.(*ast.Ident); !isIdent || pkg.Name != "fmt" {
			return true
		}
		for _, arg := range call.Args {
			if id, isIdent := arg.(*ast.Ident); isIdent && id.Name == "ErrUnsupported" {
				found = true
			}
		}
		return !found
	})
	return found
}

// TestProbes_Controls pins that each codec's probe finds nothing to refuse in
// a stream its decoder decodes, so the diagnostics call it decodable without
// decoding it.
func TestProbes_Controls(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	for _, tc := range unsupportedFeatureStreams(t) {
		probe, isCodec := codecProbes[tc.filter]
		if !isCodec {
			continue
		}
		t.Run(string(tc.filter), func(t *testing.T) {
			t.Parallel()
			if _, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, tc.decodable); err != nil || report.Incomplete() {
				t.Fatalf("control: DecodeStreamReport = %+v, %v; want a complete decode", report, err)
			}
			data, err := tc.decodable.Bytes(ctx)
			if err != nil {
				t.Fatal(err)
			}
			parms := extractDecodeParms(tc.decodable.Dict, 1)[0]
			if err := probe(ctx, data, parms, tc.decodable.Dict, limits.Default()); err != nil {
				t.Errorf("probe = %v, want nil", err)
			}
			if skipped, err := probeChain(ctx, tc.decodable, nil); err != nil || len(skipped) != 0 {
				t.Errorf("probeChain = %v, %v; want none", skipped, err)
			}
		})
	}
}

// TestProbeChain_Unsettled pins the chains the probes cannot settle, which the
// diagnostics decode instead: a filter that must run after a codec whose
// output the probe did not decode. The answer is decoding's all the same.
func TestProbeChain_Unsettled(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// JPXDecode decodes the J.10 example, then a Crypt filter naming a crypt
	// filter litho cannot apply refuses the samples.
	stream := pdfast.NewStream(pdfast.PdfDict{
		"Filter":      pdfast.PdfArray{pdfast.PdfName("JPXDecode"), pdfast.PdfName("Crypt")},
		"DecodeParms": pdfast.PdfArray{pdfast.PdfNull{}, pdfast.PdfDict{"Name": pdfast.PdfName("OtherCF")}},
	}, readJPX(t, "testdata/annex_j10.j2k"))
	if _, err := probeChain(ctx, stream, nil); !errors.Is(err, errUnsettled) {
		t.Errorf("probeChain error = %v, want %v", err, errUnsettled)
	}
	_, report, err := (*Decoder)(nil).DecodeStreamReport(ctx, stream)
	if err != nil {
		t.Fatal(err)
	}
	bad, err := (*Decoder)(nil).UndecodableFilters(ctx, stream)
	if err != nil || !slices.Equal(bad, report.Skipped) || !slices.Equal(bad, []pdfast.PdfName{"Crypt"}) {
		t.Errorf("UndecodableFilters = %v, %v; decoding skipped %v, want [Crypt]", bad, err, report.Skipped)
	}
}
