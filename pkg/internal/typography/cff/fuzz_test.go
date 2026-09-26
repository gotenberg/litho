package cff

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// FuzzCFF feeds arbitrary bytes to the CFF font-program readers. Each
// entry point parses an embedded, attacker-controlled CFF table; the
// contract is crash-freedom, not a successful parse.
func FuzzCFF(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x01, 0x00, 0x04, 0x01}) // header: major 1, hdrSize 4, offSize 1
	f.Add(make([]byte, 64))
	// Real fonts, because the synthetic seeds above reach only the header and
	// the INDEX walk. A CID-keyed CFF carries a charset, an FDSelect and an
	// FDArray of Private dicts, and a charstring that calls a subroutine needs
	// the subroutine INDEXes to resolve: neither structure arises by mutating
	// four header bytes, so those readers were measured at zero coverage.
	// These fixtures are the package's own, already used by the unit tests.
	for _, name := range []string{"g5.cff", "cid_fdarray_matrix.cff", "frutiger_subr_width.cff"} {
		data, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			f.Fatalf("reading seed fixture %s: %v", name, err)
		}
		f.Add(data)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		_ = ExtractEncoding(data)
		// PredefinedEncodingNames is a separate entry point from ExtractEncoding
		// above: it returns the whole predefined table without intersecting it
		// against the charset. No other function in this package calls it, so
		// nothing else in this target reaches it.
		_ = PredefinedEncodingNames(data)
		_, _ = GlyphToUnicode(ctx, data)
		_, _ = GlyphNames(ctx, data)
		_, _ = ValidCIDs(data)
		_ = ReadUnitsPerEm(data)
		_, _ = GlyphWidths(ctx, data)
		// The CID-keyed twin of GlyphWidths, and unreachable from every reader
		// above: nothing in this package calls it, so without this line the
		// FDArray-and-Private-dict width path had no crash-freedom coverage at
		// all. The cid_fdarray_matrix.cff seed already loaded above is the
		// fixture that drives it.
		_, _ = CIDWidths(ctx, data)
		_, _, _ = NameKeyedGlyphs(data)
	})
}
