package cmap

import (
	"context"
	"strings"
	"testing"
)

// TestEmbedPredefined_ReproducesTheBundledMapping pins the property a
// validator cannot check. ISO 19005-1, 6.3.3.3 is satisfied by /UseCMap
// pointing at a stream; nothing downstream asks whether the stream maps what
// the name mapped. A short or empty copy would score exactly as well.
func TestEmbedPredefined_ReproducesTheBundledMapping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prog, ok, err := EmbedPredefined(ctx, "H")
	if err != nil {
		t.Fatalf("EmbedPredefined: %v", err)
	}
	if !ok {
		t.Fatal("declined H, which is bundled and uniform width")
	}

	want := LoadPredefinedCIDMap("H")
	if want == nil {
		t.Fatal("LoadPredefinedCIDMap(H) is nil, so the test proves nothing")
	}
	got, err := ParseCID(ctx, prog)
	if err != nil {
		t.Fatalf("the generated program does not parse: %v", err)
	}
	if len(got.Mapping) != len(want.charToCID) {
		t.Errorf("generated CMap maps %d codes, the bundled one maps %d",
			len(got.Mapping), len(want.charToCID))
	}
	for code, cid := range want.charToCID {
		if got.Mapping[code] != cid {
			t.Fatalf("code %#04x maps to %d, want %d: the copy is not faithful",
				code, got.Mapping[code], cid)
		}
	}
}

// TestEmbedPredefined_DeclaresTheTabulatedCIDSystemInfo is a regression arm.
// An earlier draft hardcoded Supplement 0. ISO 19005-1, 6.3.3.1 compares the
// CMap's Registry, Ordering and Supplement against the CIDFont's, so a
// fabricated value settles the embedding rule and trips the consistency one.
func TestEmbedPredefined_DeclaresTheTabulatedCIDSystemInfo(t *testing.T) {
	t.Parallel()

	prog, ok, err := EmbedPredefined(context.Background(), "H")
	if err != nil || !ok {
		t.Fatalf("EmbedPredefined(H): ok=%v err=%v", ok, err)
	}
	reg, ord, sup, tabulated := PredefinedCMapCIDSystemInfo("H", false)
	if !tabulated {
		t.Fatal("no tabulated CIDSystemInfo for H, so the test proves nothing")
	}
	s := string(prog)
	for _, want := range []string{
		"/Registry (" + reg + ")",
		"/Ordering (" + ord + ")",
		"/Supplement 1 def",
		"/CMapName /H def",
		"/CMapType 1 def",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated program is missing %q", want)
		}
	}
	if sup != 1 {
		t.Errorf("the tabulated PDF 1.x supplement for H is %d, not 1: update this arm", sup)
	}
	if strings.Contains(s, "/Supplement 0 def") {
		t.Error("declared Supplement 0, the fabricated value that trips ISO 19005-1 6.3.3.1")
	}
}

// TestEmbedPredefined_CarriesTheBundledCodespace pins that the codespace comes
// from the data. H spans <2121> to <7E7E>; a defaulted <0000> <FFFF> would
// change how bytes outside that span decode.
func TestEmbedPredefined_CarriesTheBundledCodespace(t *testing.T) {
	t.Parallel()

	prog, ok, _ := EmbedPredefined(context.Background(), "H")
	if !ok {
		t.Fatal("EmbedPredefined(H) declined")
	}
	s := string(prog)
	if !strings.Contains(s, "<2121> <7E7E>") {
		t.Errorf("the bundled codespace <2121> <7E7E> is absent:\n%s", firstLines(s, 14))
	}
	if strings.Contains(s, "<0000> <FFFF>") {
		t.Error("defaulted to a full two-byte codespace instead of the bundled one")
	}
}

// TestEmbedPredefined_Declines covers every arm that must write nothing.
// Without these the function would pass just as well if it emitted whatever it
// could and left the caller to trust it.
func TestEmbedPredefined_Declines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct{ name, why string }{
		// Uniform width once its usecmap base is resolved, but flattened over
		// UniGB-UCS2-H it takes 13849 cidrange entries, past the cap.
		{"UniGB-UCS2-V", "a flattened chain past the range cap"},
		// Mixes one- and two-byte codespace ranges; CIDCMap keys codes by
		// integer value, so <41> and <0041> would collide.
		{"90ms-RKSJ-H", "mixed-width codespace"},
		// Exempt from the embedding rule, and not in the bundled set.
		{"Identity-H", "identity encodings are exempt"},
		// A character collection's own CMap, outside Table 118, which
		// tabulates no CIDSystemInfo for it; inlineNamedBases folds it in.
		{"Adobe-Korea1-2", "no tabulated CIDSystemInfo"},
		{"NotACMapName", "unknown name"},
		{"", "empty name"},
	} {
		prog, ok, err := EmbedPredefined(ctx, tc.name)
		if err != nil {
			t.Errorf("%s: unexpected error %v", tc.name, err)
			continue
		}
		if ok || prog != nil {
			t.Errorf("%s: embedded %d bytes for a CMap it must decline (%s)",
				tc.name, len(prog), tc.why)
		}
	}
}

// TestEmbedPredefined_FlattensAVerticalCMap pins the usecmap chain. V holds
// only the vertical forms and takes every other mapping, and its codespace,
// from H through usecmap. A copy of V's own records would map 53 codes and
// split none, so every other code of a font naming V would turn into .notdef.
func TestEmbedPredefined_FlattensAVerticalCMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prog, ok, err := EmbedPredefined(ctx, "V")
	if err != nil || !ok {
		t.Fatalf("EmbedPredefined(V): ok=%v err=%v", ok, err)
	}
	h, v := LoadPredefinedCIDMap("H"), LoadPredefinedCIDMap("V")
	if h == nil || v == nil {
		t.Fatal("H or V does not resolve, so the test proves nothing")
	}
	if len(v.charToCID) == 0 || len(v.charToCID) >= len(h.charToCID) {
		t.Fatalf("V holds %d codes of its own and H %d: V no longer defers to H, pick another fixture",
			len(v.charToCID), len(h.charToCID))
	}

	got := ParseEmbeddedCIDMap(ctx, prog)
	if got == nil {
		t.Fatal("the generated program does not read back as an encoding CMap")
	}
	for code, cid := range h.charToCID {
		want := cid
		if vc, own := v.charToCID[code]; own {
			want = vc
		}
		dec := got.Decode([]byte{byte(code >> 8), byte(code)})
		if len(dec) != 1 || !dec[0].Mapped || dec[0].CID != want {
			t.Fatalf("code %#04x decodes to %+v, want CID %d", code, dec, want)
		}
	}
	if !strings.Contains(string(prog), "/WMode 1 def") {
		t.Errorf("V is a vertical CMap, but the program does not declare /WMode 1:\n%s", firstLines(string(prog), 8))
	}
	if strings.Contains(string(prog), "usecmap") {
		t.Error("the program still names a usecmap base for a reader to resolve")
	}
}

// TestEmbedPredefined_DeclinesAnOversizeCMap pins the anti-bloat cap with a
// real bundled fixture rather than a contrived one. UniJIS-UCS2-H barely
// compresses: 9772 codes become 8533 cidrange entries and 166 KB. Pasting that
// into a document to settle one rule is a worse outcome than leaving the rule
// reported, and nothing downstream would object to the size.
func TestEmbedPredefined_DeclinesAnOversizeCMap(t *testing.T) {
	t.Parallel()

	const name = "UniJIS-UCS2-H"
	m := LoadPredefinedCIDMap(name)
	if m == nil {
		t.Skipf("%s is not bundled, so the cap cannot be exercised", name)
	}
	// Without this the test would pass silently if the fixture ever fell
	// under the cap, exercising nothing.
	if n := len(compressCID(m.charToCID)); n <= maxEmbeddedCIDRanges {
		t.Fatalf("%s compresses to %d ranges, at or under the cap %d: this arm "+
			"no longer reaches the guard, pick another fixture",
			name, n, maxEmbeddedCIDRanges)
	}

	prog, ok, err := EmbedPredefined(context.Background(), name)
	if err != nil {
		t.Fatalf("EmbedPredefined(%s): %v", name, err)
	}
	if ok || prog != nil {
		t.Errorf("embedded %d bytes for a CMap past the anti-bloat cap of %d ranges",
			len(prog), maxEmbeddedCIDRanges)
	}
}

func firstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}
