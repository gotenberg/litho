package cmap

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

// TestAdobeUCS2_MatchesPublishedResources pins that every embedded resource
// decompresses to the file Adobe publishes, byte for byte, and that the digest
// table and the embedded files name the same CMaps.
func TestAdobeUCS2_MatchesPublishedResources(t *testing.T) {
	t.Parallel()

	entries, err := fs.ReadDir(adobeUCS2FS, "adobeucs2")
	if err != nil {
		t.Fatal(err)
	}
	embedded := make(map[string]bool)
	for _, e := range entries {
		embedded[strings.TrimSuffix(e.Name(), ".gz")] = true
	}
	if len(embedded) != len(adobeUCS2Digests) {
		t.Errorf("embedded %d resources, the digest table names %d", len(embedded), len(adobeUCS2Digests))
	}
	for name, want := range adobeUCS2Digests {
		if !embedded[name] {
			t.Errorf("%s has a digest but no embedded file", name)
			continue
		}
		data, ok, err := adobeUCS2Source(name)
		if !ok || err != nil {
			t.Fatalf("adobeUCS2Source(%s) = %v, %v", name, ok, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s decompresses to SHA-256 %s, want %s", name, got, want)
		}
	}
}

// TestPredefinedUnicode_AdobeCurrentResources pins that the Adobe-*-UCS2 CMaps
// come from Adobe's current resources, where they differ from the 2009 copies
// PDF.js ships: Adobe-GB1-UCS2 maps CID 254 hexadecimal to U+FF5B through
// bfchar, where the PDF.js copy reaches it only through bfrange;
// Adobe-Japan1-UCS2 no longer maps CID 18A through bfchar; Adobe-CNS1-UCS2
// maps CID 4A90, which the PDF.js copy leaves out; Adobe-KR-UCS2, which PDF.js
// does not ship, is there; and a CMap mapping codes to CIDs still comes from
// the PDF.js copies, mapping nothing to Unicode.
func TestPredefinedUnicode_AdobeCurrentResources(t *testing.T) {
	t.Parallel()

	gb1, _ := PredefinedUnicode("Adobe-GB1-UCS2")
	if got := gb1["\x02\x54"]; got != "｛" {
		t.Errorf("Adobe-GB1-UCS2 bfchar CID 254 maps to %q, want U+FF5B", got)
	}
	japan1, _ := PredefinedUnicode("Adobe-Japan1-UCS2")
	if got, ok := japan1["\x01\x8A"]; ok {
		t.Errorf("Adobe-Japan1-UCS2 bfchar holds CID 18A (%q)", got)
	}
	if got := japan1["\x00\xE7"]; got != " " {
		t.Errorf("Adobe-Japan1-UCS2 bfchar CID E7 maps to %q, want U+2002", got)
	}
	_, cns1 := PredefinedUnicode("Adobe-CNS1-UCS2")
	if _, ok := cns1["\x4A\x90"]; !ok {
		t.Error("Adobe-CNS1-UCS2 does not map CID 4A90")
	}
	kr, krAll := PredefinedUnicode("Adobe-KR-UCS2")
	if len(kr) != 8049 || len(krAll) != 22897 {
		t.Errorf("Adobe-KR-UCS2 holds %d single and %d mappings, want 8049 and 22897", len(kr), len(krAll))
	}
	for name, want := range map[string][2]int{
		"Adobe-GB1-UCS2":    {10664, 30284},
		"Adobe-CNS1-UCS2":   {17066, 19179},
		"Adobe-Japan1-UCS2": {17387, 23060},
	} {
		chars, all := PredefinedUnicode(name)
		if len(chars) != want[0] || len(all) != want[1] {
			t.Errorf("%s holds %d single and %d mappings, want %d and %d", name, len(chars), len(all), want[0], want[1])
		}
	}
	if chars, all := PredefinedUnicode("UniGB-UCS2-H"); chars == nil || len(chars) != 0 || len(all) != 0 {
		t.Errorf("UniGB-UCS2-H yields %d single and %d mappings, want empty maps", len(chars), len(all))
	}
}

// TestParseAdobeUCS2 pins the reader of Adobe's text resources: a section is
// read to its end keyword whatever count precedes it, a bfrange maps each
// code to the destination incremented, and a construct the resources never use
// is refused rather than skipped.
func TestParseAdobeUCS2(t *testing.T) {
	t.Parallel()

	chars, all, err := parseAdobeUCS2([]byte("%!PS comment <zz>\n/CIDSystemInfo 3 dict dup begin /Registry (Adobe) def end def\n" +
		"1 begincodespacerange <0000> <FFFF> endcodespacerange\n" +
		"1 beginbfchar\n<0001> <0041>\n<0002> <D83DDE00>\nendbfchar\n" +
		"1 beginbfrange\n<0010> <0012> <0061>\nendbfrange\nendcmap\n"))
	if err != nil {
		t.Fatal(err)
	}
	if chars["\x00\x01"] != "A" || chars["\x00\x02"] != "\U0001F600" || len(chars) != 2 {
		t.Errorf("bfchar mappings = %q", chars)
	}
	if all["\x00\x12"] != "c" || all["\x00\x01"] != "A" || len(all) != 5 {
		t.Errorf("all mappings = %q", all)
	}

	for name, src := range map[string]string{
		"array destination":    "1 beginbfrange <0001> <0002> [<0041> <0042>] endbfrange",
		"unterminated section": "1 beginbfchar <0001> <0041>",
		"odd bfchar operands":  "1 beginbfchar <0001> <0041> <0002> endbfchar",
		"one-byte source":      "1 beginbfchar <01> <0041> endbfchar",
		"bad hex":              "1 beginbfchar <00G1> <0041> endbfchar",
		"unterminated hex":     "1 beginbfchar <0001",
		"mismatched end":       "1 beginbfchar <0001> <0041> endbfrange",
	} {
		if _, _, err := parseAdobeUCS2([]byte(src)); !errors.Is(err, errAdobeUCS2Syntax) {
			t.Errorf("%s: err = %v, want errAdobeUCS2Syntax", name, err)
		}
	}
}
