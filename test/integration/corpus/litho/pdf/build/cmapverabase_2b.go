package build

func init() { register("cmapverabase_2b", cmapverabase2b) }

// cmapverabase2b builds cmapverabase_2b.pdf: encoding CMaps only veraPDF's bundle resolves.
//
// veraPDF's PDCMap loads the CMap file it bundles under a name, whether a font's
// /Encoding, a usecmap operand or a /UseCMap entry names it, and decodes codes to
// CIDs through it (ISO 32000-1, 9.7.5.2 and 9.7.5.4). Its bundle holds CMaps
// PDF.js ships none of, the character-collection CMaps of the newest supplements
// (Adobe-KR-9, Adobe-Japan1-7, Adobe-GB1-6) and UniAKR-UTF16-H, and copies of
// Unicode CMaps that map other codes than PDF.js's: UniGB-UTF16-H maps U+2002 to
// CID 7716, which PDF.js's leaves unmapped, and UniKS-UTF16-H leaves U+FA2E
// unmapped, which PDF.js's maps to CID 4419. A code nothing maps selects CID 0
// (9.7.6.3). No corpus file names a base only veraPDF bundles or shows a code
// where the two copies differ, so no count reached this.
//
// Every font is a CIDFontType2 over the three-glyph TrueType program of
// _encodingcmap, whose /CIDToGIDMap sends CIDs 1 to 6 to a glyph; K6's also
// sends CID 7716 to one and K7's CID 4419.
//
// | Font | Encoding                           | Shows     | CIDs              | Verdict                       |
// | ---- | ---------------------------------- | --------- | ----------------- | ----------------------------- |
// | K1   | `/Adobe-KR-9 usecmap`, bfchar      | 0005 0009 | 5, 9 absent       | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
// | K2   | `/Adobe-KR-9 usecmap`, bfchar      | 0001 0006 | 1, 6              | silent, the control           |
// | K3   | `/Adobe-Japan1-7 usecmap`, bfchar  | 5A20      | 0, past CIDCount  | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
// | K4   | `/UseCMap /Adobe-GB1-6`, bfchar    | 0007      | 7 absent          | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
// | K5   | `/UniAKR-UTF16-H`                  | 0041      | 34 absent         | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
// | K6   | `/UniGB-UTF16-H`                   | 2002      | 7716              | silent, the control           |
// | K7   | `/UniKS-UTF16-H`                   | FA2E      | 0, mapped nowhere | 6.2.11.4.1 t2 and 6.2.11.8 t1 |
//
// K4's /UseCMap and K5's /Encoding name no CMap of Table 118, which 6.2.11.3.3
// t3 and t1 answer, and veraPDF knows no CIDSystemInfo for UniAKR-UTF16-H, which
// 6.2.11.3.1 t1 answers.
//
// Every count was confirmed with live veraPDF 1.30.2. See ../FIXTURES.md for the
// authoritative expected firing set, and _encodingcmap for the font program.
func cmapverabase2b() ([]byte, error) {
	kr9 := ecSystemInfo("KR", 9)
	japan17 := ecSystemInfo("Japan1", 7)
	gb16 := ecSystemInfo("GB1", 6)

	f := newECFixture()
	f.font("K1", "LithoVeraBaseAbsent", f.cmap("LithoK1", "/Adobe-KR-9 usecmap\n"+ecBFChar, ecCMapOpts{Info: kr9}), "00050009",
		kr9, nil)
	f.font("K2", "LithoVeraBasePresent", f.cmap("LithoK2", "/Adobe-KR-9 usecmap\n"+ecBFChar, ecCMapOpts{Info: kr9}), "00010006",
		kr9, nil)
	f.font("K3", "LithoVeraBaseUnmapped", f.cmap("LithoK3", "/Adobe-Japan1-7 usecmap\n"+ecBFChar, ecCMapOpts{Info: japan17}),
		"5A20", japan17, nil)
	f.font("K4", "LithoVeraUseCMapAbsent", f.cmap("LithoK4", ecBFChar, ecCMapOpts{UseCMap: n("/Adobe-GB1-6"), Info: gb16}), "0007",
		gb16, nil)
	f.font("K5", "LithoVeraNamedAbsent", n("/UniAKR-UTF16-H"), "0041", kr9, nil)
	f.font("K6", "LithoVeraRevisedPresent", n("/UniGB-UTF16-H"), "2002", ecSystemInfo("GB1", 4),
		f.gidMap([]int{1, 2, 3, 4, 5, 6, 7716}))
	f.font("K7", "LithoVeraRevisedUnmapped", n("/UniKS-UTF16-H"), "FA2E", ecSystemInfo("Korea1", 2),
		f.gidMap([]int{1, 2, 3, 4, 5, 6, 4419}))
	return f.save()
}
