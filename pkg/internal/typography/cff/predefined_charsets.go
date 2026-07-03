package cff

// The three predefined CFF charsets are identified by a charset offset of 0,
// 1, or 2 in the Top DICT (Adobe Technical Note #5176, "The Compact Font
// Format Specification", 18 December 2003, §13 and Appendix C). An offset of
// 0/1/2 is a reserved sentinel, never a byte position into the font (the CFF
// header occupies the first bytes), so it selects a predefined GID → SID
// mapping rather than an in-font charset table.
//
//   0 = ISOAdobe    GID i maps to SID i (identity over the 229 ISOAdobe glyphs)
//   1 = Expert      the SID sequence in [expertCharsetSIDs]
//   2 = ExpertSubset the SID sequence in [expertSubsetCharsetSIDs]
//
// The Expert and ExpertSubset SID sequences below are generated from the glyph
// name lists in Appendix C by resolving each name to its standard-string SID
// (its index in [cffStandardStrings]); [TestPredefinedCharsets] re-derives and
// pins them so a transcription error cannot go unnoticed.

// expertCharsetSIDs is the GID → SID mapping for the Expert predefined charset
// (charset offset 1), Adobe Technical Note #5176 Appendix C. 166 glyphs.
var expertCharsetSIDs = [...]int{
	0, 1, 229, 230, 231, 232, 233, 234, 235, 236, 237, 238, 13, 14, 15, 99,
	239, 240, 241, 242, 243, 244, 245, 246, 247, 248, 27, 28, 249, 250, 251, 252,
	253, 254, 255, 256, 257, 258, 259, 260, 261, 262, 263, 264, 265, 266, 109, 110,
	267, 268, 269, 270, 271, 272, 273, 274, 275, 276, 277, 278, 279, 280, 281, 282,
	283, 284, 285, 286, 287, 288, 289, 290, 291, 292, 293, 294, 295, 296, 297, 298,
	299, 300, 301, 302, 303, 304, 305, 306, 307, 308, 309, 310, 311, 312, 313, 314,
	315, 316, 317, 318, 158, 155, 163, 319, 320, 321, 322, 323, 324, 325, 326, 150,
	164, 169, 327, 328, 329, 330, 331, 332, 333, 334, 335, 336, 337, 338, 339, 340,
	341, 342, 343, 344, 345, 346, 347, 348, 349, 350, 351, 352, 353, 354, 355, 356,
	357, 358, 359, 360, 361, 362, 363, 364, 365, 366, 367, 368, 369, 370, 371, 372,
	373, 374, 375, 376, 377, 378,
}

// expertSubsetCharsetSIDs is the GID → SID mapping for the ExpertSubset
// predefined charset (charset offset 2), Adobe Technical Note #5176 Appendix C.
// 87 glyphs.
var expertSubsetCharsetSIDs = [...]int{
	0, 1, 231, 232, 235, 236, 237, 238, 13, 14, 15, 99, 239, 240, 241, 242,
	243, 244, 245, 246, 247, 248, 27, 28, 249, 250, 251, 253, 254, 255, 256, 257,
	258, 259, 260, 261, 262, 263, 264, 265, 266, 109, 110, 267, 268, 269, 270, 272,
	300, 301, 302, 305, 314, 315, 158, 155, 163, 320, 321, 322, 323, 324, 325, 326,
	150, 164, 169, 327, 328, 329, 330, 331, 332, 333, 334, 335, 336, 337, 338, 339,
	340, 341, 342, 343, 344, 345, 346,
}

// resolveCharset returns the GID → SID mapping for a CFF font's charset. A
// charsetOffset of 0, 1, or 2 selects a predefined charset (ISOAdobe, Expert,
// ExpertSubset); any other value is a byte offset into data and is decoded by
// [parseCharset]. In a CID-keyed font the returned values are CIDs rather than
// SIDs, matching [parseCharset]'s contract.
func resolveCharset(data []byte, charsetOffset, numGlyphs int) ([]int, error) {
	switch charsetOffset {
	case 0: // ISOAdobe: identity, GID i maps to SID i.
		return predefinedCharsetSIDs(nil, numGlyphs), nil
	case 1:
		return predefinedCharsetSIDs(expertCharsetSIDs[:], numGlyphs), nil
	case 2:
		return predefinedCharsetSIDs(expertSubsetCharsetSIDs[:], numGlyphs), nil
	default:
		return parseCharset(data, charsetOffset, numGlyphs)
	}
}

// predefinedCharsetSIDs materializes a numGlyphs-long GID → SID slice from a
// predefined charset table. A nil table is the ISOAdobe identity mapping
// (SID i at GID i). A GID beyond the table's length maps to SID 0 (.notdef):
// the font declares more glyphs than the predefined charset defines, which is
// malformed, so the surplus glyphs carry no name rather than a spurious one.
func predefinedCharsetSIDs(table []int, numGlyphs int) []int {
	if numGlyphs < 0 {
		return nil
	}
	sids := make([]int, numGlyphs)
	for gid := range numGlyphs {
		switch {
		case table == nil:
			sids[gid] = gid
		case gid < len(table):
			sids[gid] = table[gid]
		}
	}
	return sids
}
