package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
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
		cids, notdef, _, ok := collectType0CIDs("Identity-H", nil, usage(0x00, 0x05, 0x01, 0x00))
		if !ok || notdef {
			t.Fatalf("ok=%v notdef=%v, want ok and not notdef", ok, notdef)
		}
		if !cids[5] || !cids[256] {
			t.Errorf("expected CIDs 5 and 256, got %v", cids)
		}
	})

	t.Run("identity odd length is notdef", func(t *testing.T) {
		t.Parallel()
		_, notdef, _, ok := collectType0CIDs("Identity-V", nil, usage(0x00, 0x05, 0x01))
		if !ok || !notdef {
			t.Errorf("odd-length string under a 2-byte CMap must be notdef: ok=%v notdef=%v", ok, notdef)
		}
	})

	t.Run("predefined CMap valid code maps", func(t *testing.T) {
		t.Parallel()
		// 0x41 ('A') is a valid single-byte Shift-JIS code mapped to a CID.
		cids, notdef, _, ok := collectType0CIDs("90ms-RKSJ-H", nil, usage(0x41))
		if !ok || notdef {
			t.Fatalf("ok=%v notdef=%v cids=%v", ok, notdef, cids)
		}
		if len(cids) == 0 {
			t.Error("expected at least one CID for code 0x41")
		}
	})

	t.Run("predefined CMap undefined code is skipped, not a false positive", func(t *testing.T) {
		t.Parallel()
		// 0xFF matches no codespace of this CMap. veraPDF splits such bytes
		// otherwise (cmap.CIDCode.Undefined), so it is skipped rather than
		// reported as .notdef.
		cids, notdef, _, ok := collectType0CIDs("90ms-RKSJ-H", nil, usage(0xFF))
		if !ok || notdef {
			t.Errorf("undefined code must be skipped (no false positive): ok=%v notdef=%v", ok, notdef)
		}
		if len(cids) != 0 {
			t.Errorf("undefined code should yield no CID, got %v", cids)
		}
	})

	t.Run("predefined CMap notdef range maps to its CID", func(t *testing.T) {
		t.Parallel()
		// 90ms-RKSJ-H maps <00> to <1F> to CID 231 through notdefrange, which
		// veraPDF's CMap.toCID applies once no cid mapping covers the code.
		cids, notdef, unmappedZero, ok := collectType0CIDs("90ms-RKSJ-H", nil, usage(0x01))
		if !ok || notdef || unmappedZero {
			t.Fatalf("ok=%v notdef=%v unmappedZero=%v", ok, notdef, unmappedZero)
		}
		if !cids[231] || len(cids) != 1 {
			t.Errorf("code 0x01 must select the notdefrange CID 231, got %v", cids)
		}
	})

	t.Run("predefined vertical CMap resolves its usecmap base", func(t *testing.T) {
		t.Parallel()
		// 90ms-RKSJ-V holds the vertical forms, <8141> among them, and takes
		// everything else, its codespaces included, from 90ms-RKSJ-H.
		cids, notdef, _, ok := collectType0CIDs("90ms-RKSJ-V", nil, usage(0x81, 0x41, 0x41))
		if !ok || notdef {
			t.Fatalf("ok=%v notdef=%v: a -V CMap must decode through its base", ok, notdef)
		}
		if !cids[7887] || !cids[264] || len(cids) != 2 {
			t.Errorf("want the vertical CID 7887 for <8141> and the base CID 264 for <41>, got %v", cids)
		}
	})

	t.Run("predefined CMap unmapped code selects CID 0", func(t *testing.T) {
		t.Parallel()
		// <E000> lies in UniJIS-UCS2-H's codespace and no mapping of it or of
		// a base covers it, so it selects CID 0 (ISO 32000-1, 9.7.6.3).
		m := cmap.LoadPredefinedCIDMap("UniJIS-UCS2-H")
		if m == nil || m.Decode([]byte{0xE0, 0x00})[0].Mapped {
			t.Fatal("<E000> is mapped in UniJIS-UCS2-H, pick another code")
		}
		cids, notdef, unmappedZero, ok := collectType0CIDs("UniJIS-UCS2-H", nil, usage(0xE0, 0x00))
		if !ok || notdef || !unmappedZero {
			t.Fatalf("ok=%v notdef=%v unmappedZero=%v, want an unmapped code reported", ok, notdef, unmappedZero)
		}
		if !cids[0] {
			t.Errorf("an unmapped code must select CID 0, got %v", cids)
		}
	})

	t.Run("embedded or unknown encoding is undecodable", func(t *testing.T) {
		t.Parallel()
		if _, _, _, ok := collectType0CIDs("SomeEmbeddedStreamCMap", nil, usage(0x00, 0x01)); ok {
			t.Error("an unrecognized encoding must report ok=false so the caller skips the check")
		}
	})
}
