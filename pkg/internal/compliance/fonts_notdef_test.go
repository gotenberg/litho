package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestCollectType0CIDs covers the Type0 code->CID decoding for both Identity
// and predefined-CMap encodings, including the .notdef cases.
func TestCollectType0CIDs(t *testing.T) {
	t.Parallel()
	usage := func(b ...byte) *fontUsageInfo {
		return &fontUsageInfo{rawStrings: []ast.PdfString{ast.PdfString(b)}}
	}

	t.Run("identity even length collects CIDs", func(t *testing.T) {
		t.Parallel()
		cids, notdef, ok := collectType0CIDs("Identity-H", nil, usage(0x00, 0x05, 0x01, 0x00))
		if !ok || notdef {
			t.Fatalf("ok=%v notdef=%v, want ok and not notdef", ok, notdef)
		}
		if !cids[5] || !cids[256] {
			t.Errorf("expected CIDs 5 and 256, got %v", cids)
		}
	})

	t.Run("identity odd length ignores trailing byte", func(t *testing.T) {
		t.Parallel()
		// A trailing odd byte is an incomplete code, not a glyph reference.
		// veraPDF does not treat it as a .notdef reference, so litho decodes
		// the valid 2-byte prefix (0x0005) and ignores the dangling 0x01.
		cids, notdef, ok := collectType0CIDs("Identity-V", nil, usage(0x00, 0x05, 0x01))
		if !ok || notdef {
			t.Errorf("odd-length trailing byte must not be notdef: ok=%v notdef=%v", ok, notdef)
		}
		if !cids[0x0005] {
			t.Errorf("expected the valid 2-byte prefix 0x0005 to decode, got %v", cids)
		}
	})

	t.Run("predefined CMap valid code maps", func(t *testing.T) {
		t.Parallel()
		// 0x41 ('A') is a valid single-byte Shift-JIS code mapped to a CID.
		cids, notdef, ok := collectType0CIDs("90ms-RKSJ-H", nil, usage(0x41))
		if !ok || notdef {
			t.Fatalf("ok=%v notdef=%v cids=%v", ok, notdef, cids)
		}
		if len(cids) == 0 {
			t.Error("expected at least one CID for code 0x41")
		}
	})

	t.Run("predefined CMap undefined code is skipped, not a false positive", func(t *testing.T) {
		t.Parallel()
		// 0xFF is undefined in this CMap. To avoid a false positive when a
		// mapping lives in an unresolved usecmap base, it is skipped (not
		// flagged) rather than reported as .notdef.
		cids, notdef, ok := collectType0CIDs("90ms-RKSJ-H", nil, usage(0xFF))
		if !ok || notdef {
			t.Errorf("undefined code must be skipped (no false positive): ok=%v notdef=%v", ok, notdef)
		}
		if len(cids) != 0 {
			t.Errorf("undefined code should yield no CID, got %v", cids)
		}
	})

	t.Run("embedded or unknown encoding is undecodable", func(t *testing.T) {
		t.Parallel()
		if _, _, ok := collectType0CIDs("SomeEmbeddedStreamCMap", nil, usage(0x00, 0x01)); ok {
			t.Error("an unrecognized encoding must report ok=false so the caller skips the check")
		}
	})
}
