package cmap

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"
)

// TestVeraPDFCMapsMatchDigests pins that every embedded copy decompresses to
// the file its generator verified, so a hand edit of the resources fails.
func TestVeraPDFCMapsMatchDigests(t *testing.T) {
	t.Parallel()
	for name, want := range veraPDFCMapDigests {
		data, ok, err := veraPDFCMapSource(name)
		if err != nil || !ok {
			t.Fatalf("%s: ok=%v err=%v", name, ok, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s: SHA-256 %s, want %s", name, got, want)
		}
		if !bytes.Contains(data, []byte("Redistribution and use in source and binary forms")) {
			t.Errorf("%s: the file lost the BSD license its header carries", name)
		}
		if !veraPDFBundledCMaps[name] {
			t.Errorf("%s is embedded but not listed as bundled", name)
		}
	}
}

// TestConformanceCIDBaseVeraPDFOnly pins that a base only veraPDF 1.30.2
// bundles resolves for the conformance checks and stays unresolved for text
// extraction. Adobe-KR-9 maps the codes 0000 to 5970 to themselves inside
// the code space <0000> <59FF>, so a code past CID 22896 selects CID 0 (ISO
// 32000-1, 9.7.6.3).
func TestConformanceCIDBaseVeraPDFOnly(t *testing.T) {
	t.Parallel()
	if b := NamedBase("Adobe-KR-9"); b.Map != nil || !b.Unresolved {
		t.Errorf("NamedBase(Adobe-KR-9) = %+v, want unresolved for text extraction", b)
	}
	b := ConformanceNamedBase("Adobe-KR-9")
	if b.Map == nil {
		t.Fatal("ConformanceNamedBase(Adobe-KR-9) did not resolve")
	}
	if !b.Map.UnmappedNotdef() {
		t.Error("a fully resolved base is not closed")
	}
	for code, want := range map[int]int{0x0005: 5, 0x5970: 22896} {
		if cid, ok := b.Map.lookup(code); !ok || cid != want {
			t.Errorf("Adobe-KR-9 %04X = %d (%v), want %d", code, cid, ok, want)
		}
	}
	if got := b.Map.Decode([]byte{0x59, 0x71}); len(got) != 1 || got[0].Mapped || got[0].Undefined {
		t.Errorf("Adobe-KR-9 5971 = %+v, want one unmapped code inside the code space", got)
	}
	for _, name := range []string{"Adobe-CNS1-7", "Adobe-GB1-6", "Adobe-Japan1-7", "Adobe-Japan2-0", "Adobe-KR-0", "UniAKR-UTF16-H", "Adobe-KR-UCS2"} {
		if ConformanceCIDBase(name) == nil {
			t.Errorf("ConformanceCIDBase(%s) did not resolve", name)
		}
		if PredefinedCIDBase(name) != nil {
			t.Errorf("PredefinedCIDBase(%s) resolved: PDF.js ships no copy", name)
		}
	}
	// UniAKR-UTF16-H maps U+0041 to CID 34 and U+AC00 to CID 221.
	got := ConformanceCIDBase("UniAKR-UTF16-H").Decode([]byte{0x00, 0x41, 0xAC, 0x00})
	if len(got) != 2 || got[0].CID != 34 || got[1].CID != 221 {
		t.Errorf("UniAKR-UTF16-H decodes 0041 AC00 to %+v, want CIDs 34 and 221", got)
	}
	for _, name := range []string{"No-Such-CMap", "", "../verapdfcmaps/Adobe-KR-9"} {
		if b := ConformanceNamedBase(name); b.Map != nil || b.Unresolved {
			t.Errorf("ConformanceNamedBase(%q) = %+v, want the zero Base veraPDF ignores", name, b)
		}
	}
}

// TestConformanceCIDBaseRevisedCMaps pins that a Unicode CMap whose veraPDF
// copy maps other codes than PDF.js's reads veraPDF's for the conformance
// checks and PDF.js's for text extraction, its -V variant included through
// the usecmap chain.
func TestConformanceCIDBaseRevisedCMaps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		code       int
		vera, text int // 0 for a code the copy leaves unmapped
	}{
		{"UniGB-UTF16-H", 0x2002, 7716, 0},
		{"UniGB-UTF16-V", 0x2002, 7716, 0},
		{"UniJIS-UTF16-H", 0x02C9, 129, 0},
		{"UniCNS-UTF16-H", 0x2550, 14091, 322},
		{"UniKS-UTF16-H", 0xFA2E, 0, 4419},
	}
	for _, c := range cases {
		vera, text := ConformanceCIDBase(c.name), PredefinedCIDBase(c.name)
		if vera == nil || text == nil {
			t.Fatalf("%s did not resolve", c.name)
		}
		if got, _ := vera.lookup(c.code); got != c.vera {
			t.Errorf("%s %04X conformance = %d, want %d", c.name, c.code, got, c.vera)
		}
		if got, _ := text.lookup(c.code); got != c.text {
			t.Errorf("%s %04X text = %d, want %d", c.name, c.code, got, c.text)
		}
	}
	if v := ConformanceCIDBase("UniGB-UTF16-V"); v == nil || v.WMode() != 1 {
		t.Error("UniGB-UTF16-V lost its vertical writing mode")
	}
}

// TestLoadConformanceCIDMap pins that a Type 0 /Encoding naming a CMap
// veraPDF bundles decodes for the conformance checks, Table 118 or not, as
// veraPDF's PDCMap loads it by name.
func TestLoadConformanceCIDMap(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Adobe-Japan1-2", "UniAKR-UTF16-H", "UniGB-UTF16-H"} {
		if LoadConformanceCIDMap(name) == nil {
			t.Errorf("LoadConformanceCIDMap(%s) = nil", name)
		}
	}
	if LoadPredefinedCIDMap("Adobe-Japan1-2") != nil {
		t.Error("text extraction now decodes Adobe-Japan1-2")
	}
	for _, name := range []string{"Identity-H", "Identity-V", "No-Such-CMap"} {
		if LoadConformanceCIDMap(name) != nil {
			t.Errorf("LoadConformanceCIDMap(%s) resolved", name)
		}
	}
}

// TestConformanceProgramIsWhatTheCheckerReads pins that the program a repair
// embeds for a name decodes, read back as an embedded CMap stream, exactly as
// the conformance checks decoded the name: UniAKR-UTF16-H sends <0041> to CID
// 34, a surrogate pair to its supplementary-plane CID, and <0000> through its
// notdefrange.
func TestConformanceProgramIsWhatTheCheckerReads(t *testing.T) {
	t.Parallel()
	got, ok, err := ConformanceProgram("UniAKR-UTF16-H")
	if err != nil || !ok {
		t.Fatalf("ConformanceProgram(UniAKR-UTF16-H): ok=%v err=%v", ok, err)
	}
	if got.Registry != "Adobe" || got.Ordering != "KR" || got.Supplement != 9 || got.WMode != 0 {
		t.Errorf("declared %s-%s-%d WMode %d, want Adobe-KR-9 WMode 0",
			got.Registry, got.Ordering, got.Supplement, got.WMode)
	}
	embedded := ParseEmbeddedCIDMap(context.Background(), got.Program)
	named := LoadConformanceCIDMap("UniAKR-UTF16-H")
	if embedded == nil || named == nil {
		t.Fatalf("embedded=%v named=%v, want both read", embedded != nil, named != nil)
	}
	codes := []byte{0x00, 0x41, 0xD8, 0x3C, 0xDD, 0x70, 0x00, 0x00, 0xAC, 0x00}
	want := named.Decode(codes)
	if have := embedded.Decode(codes); !slices.Equal(have, want) {
		t.Errorf("the embedded program decodes %v, the name %v", have, want)
	}
	if len(want) == 0 || want[0].CID != 34 {
		t.Errorf("<0041> decodes as %v, want CID 34 first", want)
	}
}

// TestConformanceProgramDeclines pins the names no faithful copy stands for:
// a CMap PDF.js alone bundles, the UCS2 CMap veraPDF shares with it, a
// program that still names its base, and a name nothing bundles.
func TestConformanceProgramDeclines(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"H", "Adobe-KR-UCS2", "UniJIS-UTF16-V", "UniXX-UTF16-H"} {
		if _, ok, err := ConformanceProgram(name); ok || err != nil {
			t.Errorf("ConformanceProgram(%s): ok=%v err=%v, want declined", name, ok, err)
		}
	}
}

// TestConformanceProgramReadsEveryCIDSystemInfo pins that every program the
// verapdfcmaps resources hold, but UniJIS-UTF16-V, which runs usecmap, yields
// a CIDSystemInfo, and the registry and ordering veraPDF tabulates wherever
// it tabulates the name.
func TestConformanceProgramReadsEveryCIDSystemInfo(t *testing.T) {
	t.Parallel()
	for name := range veraPDFCMapDigests {
		got, ok, err := ConformanceProgram(name)
		if name == "UniJIS-UTF16-V" {
			if ok {
				t.Errorf("%s: embedded with its usecmap", name)
			}
			continue
		}
		if err != nil || !ok {
			t.Errorf("%s: ok=%v err=%v", name, ok, err)
			continue
		}
		if got.Registry != "Adobe" || got.Ordering == "" {
			t.Errorf("%s: CIDSystemInfo %s-%s-%d", name, got.Registry, got.Ordering, got.Supplement)
		}
		// ISO 32000-2 moved UniKS-UTF16-H to Adobe-KR, so the program may
		// match either generation's row.
		reg1, ord1, _, known := PredefinedCMapCIDSystemInfo(name, false)
		reg2, ord2, _, _ := PredefinedCMapCIDSystemInfo(name, true)
		if known && (reg1 != got.Registry || ord1 != got.Ordering) && (reg2 != got.Registry || ord2 != got.Ordering) {
			t.Errorf("%s: the program declares %s-%s, the table %s-%s and %s-%s",
				name, got.Registry, got.Ordering, reg1, ord1, reg2, ord2)
		}
	}
}
