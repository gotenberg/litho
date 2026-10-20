package mutations

import (
	"fmt"
	"testing"
)

// binaryObjects returns a one-page document whose page uses a TrueType font
// with an embedded program (object 7) and an ICCBased colour space (object 8),
// and whose catalog names an output intent profile (object 9). pageExtra and
// catalogExtra are spliced into the page and catalog dictionaries.
func binaryObjects(pageExtra, catalogExtra string) map[int]string {
	objs := copiesObjects("/OutputIntents [<< /Type /OutputIntent /S /GTS_PDFA1 " +
		"/OutputConditionIdentifier (sRGB) /DestOutputProfile 9 0 R >>] " + catalogExtra)
	objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /Font << /F1 5 0 R >> /ColorSpace << /CS0 [/ICCBased 8 0 R] >> >> " + pageExtra + " >>"
	objs[5] = "<< /Type /Font /Subtype /TrueType /BaseFont /Demo /FontDescriptor 6 0 R >>"
	objs[6] = "<< /Type /FontDescriptor /FontName /Demo /Flags 32 /FontBBox [0 0 1000 1000] " +
		"/ItalicAngle 0 /Ascent 800 /Descent -200 /CapHeight 700 /StemV 80 /FontFile2 7 0 R >>"
	objs[7] = streamObject("/Length1 32", []byte("glyf name table: SECRET Notice"))
	objs[8] = streamObject("/N 3", []byte("ICC profile desc: SECRET"))
	objs[9] = streamObject("/N 3", []byte("ICC profile desc: SECRET"))
	return objs
}

// TestScanOutputForTokens_SkipsFontProgramsAndProfiles pins that the data of an
// embedded font program and of an ICC profile is not compared. Text extraction
// reads character codes through /ToUnicode or the encoding, never the font
// program (ISO 32000-1, 9.10.2), and a profile is colour data (8.6.5.5), so a
// match there is a copy no reader finds.
func TestScanOutputForTokens_SkipsFontProgramsAndProfiles(t *testing.T) {
	t.Parallel()
	res := scanFor(t, copiesDoc(t, binaryObjects("", ""), nil, ""), "SECRET")
	if !res.Clean() {
		t.Errorf("a font program or profile was compared: %+v", res)
	}

	// The control: the same document with the word drawn on the page is not
	// clean, so the result above does not come from a scan that reads nothing.
	objs := binaryObjects("", "")
	objs[4] = streamObject("", []byte("BT /F1 12 Tf 100 600 Td (SECRET) Tj ET"))
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 4, "stream") {
		t.Errorf("the page's own copy was not reported: %+v", res)
	}
}

// TestScanOutputForTokens_SkippedFontProgramIsNotUnreadable pins that a font
// program the scan does not compare is not decoded either, so a damaged one no
// longer fails the scan closed.
func TestScanOutputForTokens_SkippedFontProgramIsNotUnreadable(t *testing.T) {
	t.Parallel()
	objs := binaryObjects("", "")
	objs[7] = streamObject("/Filter /FlateDecode /Length1 32", []byte("not flate data"))
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !res.Clean() {
		t.Errorf("an undecodable font program was read: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsABinarySlotAlsoReachedAsText pins that the skip
// trusts the slot, not the object. A stream a crafted file names both as a
// /FontFile2 and as something a reader does read, a page's contents or a
// private data stream, is still compared whichever reference the walk meets
// first. The walk ranges dictionaries, so repeating the scan meets both orders.
func TestScanOutputForTokens_ReadsABinarySlotAlsoReachedAsText(t *testing.T) {
	t.Parallel()
	type aliasCase struct {
		objs map[int]string
		want int
	}
	cases := map[string]aliasCase{
		"page contents": {want: 7, objs: func() map[int]string {
			objs := binaryObjects("", "")
			objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 7 0 R " +
				"/Resources << /Font << /F1 5 0 R >> >> >>"
			objs[7] = streamObject("/Length1 32", []byte("BT /F1 12 Tf 100 600 Td (SECRET) Tj ET"))
			return objs
		}()},
		"piece info":  {want: 7, objs: binaryObjects("", "/PieceInfo << /App << /Private 7 0 R >> >>")},
		"icc profile": {want: 8, objs: binaryObjects("/PieceInfo << /App << /Private 8 0 R >> >>", "")},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := copiesDoc(t, tc.objs, nil, "")
			for range 20 {
				res := scanFor(t, doc, "SECRET")
				if !hasHit(res, tc.want, "stream") {
					t.Fatalf("a stream also reached as text was skipped: %+v", res)
				}
			}
		})
	}
}

// cmapObjects returns a one-page document whose page uses a Type0 font
// (object 10) with a /ToUnicode CMap (object 11) and an /Encoding CMap stream
// (object 12) that names a parent through /UseCMap (object 13), and a
// TrueType font (object 14) with a /ToUnicode CMap of its own (object 15).
// Each CMap holds the PostScript a CMap file is written in and the word
// SECRET. pageExtra is spliced into the page dictionary.
func cmapObjects(pageExtra string) map[int]string {
	cmap := []byte("/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"/CIDSystemInfo << /Registry (Adobe) /Ordering (SECRET) /Supplement 0 >> def " +
		"1 begincodespacerange <00> <FF> endcodespacerange endcmap end end")
	systemInfo := "/CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >>"
	objs := copiesObjects("")
	objs[3] = "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /Font << /F1 10 0 R /F2 14 0 R >> >> " + pageExtra + " >>"
	objs[10] = "<< /Type /Font /Subtype /Type0 /BaseFont /Demo /Encoding 12 0 R " +
		"/DescendantFonts [<< /Type /Font /Subtype /CIDFontType2 /BaseFont /Demo " +
		systemInfo + " >>] /ToUnicode 11 0 R >>"
	objs[11] = streamObject("", cmap)
	objs[12] = streamObject("/Type /CMap /CMapName /Demo-H /UseCMap 13 0 R "+systemInfo, cmap)
	objs[13] = streamObject("/Type /CMap /CMapName /Demo-Parent "+systemInfo, cmap)
	// No /Type: a font dictionary known by its /Subtype alone, as some
	// producers write it.
	objs[14] = "<< /Subtype /TrueType /BaseFont /Plain /ToUnicode 15 0 R >>"
	objs[15] = streamObject("", cmap)
	return objs
}

// TestScanOutputForTokens_SkipsCMaps pins that the data of a font's
// /ToUnicode CMap, of a Type0 font's /Encoding CMap and of a CMap its
// /UseCMap names is not compared. A CMap maps character codes to Unicode or
// to CIDs (ISO 32000-1, 9.10.3 and 9.7.5): it is the font's table, not a
// record of what a page drew, so a match there is a copy no reader finds.
func TestScanOutputForTokens_SkipsCMaps(t *testing.T) {
	t.Parallel()
	res := scanFor(t, copiesDoc(t, cmapObjects(""), nil, ""), "SECRET")
	if !res.Clean() {
		t.Errorf("a CMap was compared: %+v", res)
	}

	// The control: the same document with the word drawn on the page is not
	// clean, so the result above does not come from a scan that reads nothing.
	objs := cmapObjects("")
	objs[4] = streamObject("", []byte("BT /F2 12 Tf 100 600 Td (SECRET) Tj ET"))
	if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 4, "stream") {
		t.Errorf("the page's own copy was not reported: %+v", res)
	}
}

// TestScanOutputForTokens_ReadsACMapAlsoReachedAsText pins that the skip trusts
// the slot, not the object: a CMap stream a crafted file also names somewhere
// a reader reads is compared whichever reference the walk meets first. The
// walk ranges dictionaries, so repeating the scan meets both orders.
func TestScanOutputForTokens_ReadsACMapAlsoReachedAsText(t *testing.T) {
	t.Parallel()
	cases := map[string]int{
		"tounicode": 11,
		"encoding":  12,
		"usecmap":   13,
	}
	for name, objNum := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc := copiesDoc(t, cmapObjects(fmt.Sprintf("/PieceInfo << /App << /Private %d 0 R >> >>", objNum)), nil, "")
			for range 20 {
				if res := scanFor(t, doc, "SECRET"); !hasHit(res, objNum, "stream") {
					t.Fatalf("a CMap also reached as text was skipped: %+v", res)
				}
			}
		})
	}
}

// TestScanOutputForTokens_ReadsCMapKeysOutsideTheirDictionary pins that the
// CMap slots are trusted only in the dictionary that defines them: /ToUnicode
// and /Encoding in a font dictionary (ISO 32000-1, Tables 111 and 121) and
// /UseCMap in a CMap stream dictionary (Table 120). The same key in a
// producer's private dictionary names data the format says nothing about, so
// it is compared.
func TestScanOutputForTokens_ReadsCMapKeysOutsideTheirDictionary(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"ToUnicode", "Encoding", "UseCMap"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			objs := copiesObjects(fmt.Sprintf("/PieceInfo << /App << /%s 5 0 R >> >>", key))
			objs[5] = streamObject("", []byte("SECRET"))
			if res := scanFor(t, copiesDoc(t, objs, nil, ""), "SECRET"); !hasHit(res, 5, "stream") {
				t.Errorf("a /%s outside its dictionary was skipped: %+v", key, res)
			}
		})
	}
}
