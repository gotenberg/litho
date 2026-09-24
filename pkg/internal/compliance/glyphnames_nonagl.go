package compliance

// nonAGLGlyphNames lists the glyph names litho's typography tables map to a
// Unicode value even though no glyph list a conforming reader is expected to
// hold defines them.
//
// ISO 32000-1, 9.10.2 derives a character's Unicode value from a glyph name
// through the standard Adobe glyph lists, and ISO 19005-1, 6.3.8,
// ISO 19005-2, 6.2.11.7.2 and ISO 14289-1, 7.21.7 rest their exemptions on that
// derivation. litho's [encodings] tables serve text extraction as well, so they
// take in names from beyond those lists: the TeX math repertoire of big
// delimiters and display operators (integraldisplay, parenleftBigg,
// summationtext), a handful of TeX symbol names (planck, Reals), and the
// underscore-joined ligature spellings (f_f). Extraction is right to render
// them; a conformance exemption is not, because a consumer holding the
// published glyph lists cannot recover the character.
//
// The underscore spellings deserve their own note, because Adobe Technical
// Note #5946 does resolve them, by splitting on the underscore and mapping each
// component. That algorithm governs OpenType glyph naming, not the glyph lists
// 9.10.2 names: glyphlist.txt carries ff, fi and ffi but no f_f, f_i or f_f_i.
// veraPDF reads it the same way, reporting such a glyph as "The glyph can not
// be mapped to Unicode".
//
// Generated as the names in [encodings.GlyphToRune] and encodings.extendedAGL
// that the Adobe Glyph List (glyphlist.txt 2.0, read through
// fontTools.agl.LEGACY_AGL2UV) does not carry, less two sets that are admitted
// in their own right. The first is [encodings.SymbolGlyphsUnicode]. The second
// is the a1 to a191 series of the Adobe ZapfDingbats glyph list
// (zapfdingbats.txt), which veraPDF resolves.
//
// # Corpus evidence
//
//   - pdfjs issue11016_reduced.pdf: its Calibre-Medium subset binds code 31 to
//     f_f, and veraPDF reports it as "The glyph can not be mapped to Unicode".
//   - verapdf 6-2-11-7-2-t01-fail-a.pdf, 6-3-8-t01-fail-a.pdf: the two files
//     that turn on the TeX names. Both embed CMEX10 whose sole glyph is
//     integraldisplay, and both are reported failing the same way.
//   - govdocs thread_000_000859.pdf: binds code 1 to a71 through /Differences
//     on an unembedded ZapfDingbats, and veraPDF reports nothing.
var nonAGLGlyphNames = map[string]bool{
	"Complexes":             true,
	"Integers":              true,
	"Naturals":              true,
	"Rationals":             true,
	"Reals":                 true,
	"angbracketleft":        true,
	"angbracketleftBig":     true,
	"angbracketleftBigg":    true,
	"angbracketleftbig":     true,
	"angbracketleftbigg":    true,
	"angbracketright":       true,
	"angbracketrightBig":    true,
	"angbracketrightBigg":   true,
	"angbracketrightbig":    true,
	"angbracketrightbigg":   true,
	"arrowhookleft":         true,
	"arrowhookright":        true,
	"arrowleftbothalf":      true,
	"arrowlefttophalf":      true,
	"arrownortheast":        true,
	"arrownorthwest":        true,
	"arrowrightbothalf":     true,
	"arrowrighttophalf":     true,
	"arrowsoutheast":        true,
	"arrowsouthwest":        true,
	"backslashBig":          true,
	"backslashBigg":         true,
	"backslashbig":          true,
	"backslashbigg":         true,
	"bardbl":                true,
	"bracehtipdownleft":     true,
	"bracehtipdownright":    true,
	"bracehtipupleft":       true,
	"bracehtipupright":      true,
	"braceleftBig":          true,
	"braceleftBigg":         true,
	"braceleftbig":          true,
	"braceleftbigg":         true,
	"bracerightBig":         true,
	"bracerightBigg":        true,
	"bracerightbig":         true,
	"bracerightbigg":        true,
	"bracketleftBig":        true,
	"bracketleftBigg":       true,
	"bracketleftbig":        true,
	"bracketleftbigg":       true,
	"bracketrightBig":       true,
	"bracketrightBigg":      true,
	"bracketrightbig":       true,
	"bracketrightbigg":      true,
	"ceilingleftBig":        true,
	"ceilingleftBigg":       true,
	"ceilingleftbig":        true,
	"ceilingleftbigg":       true,
	"ceilingrightBig":       true,
	"ceilingrightBigg":      true,
	"ceilingrightbig":       true,
	"ceilingrightbigg":      true,
	"circlecopyrt":          true,
	"circledotdisplay":      true,
	"circledottext":         true,
	"circlemultiplydisplay": true,
	"circlemultiplytext":    true,
	"circleplusdisplay":     true,
	"circleplustext":        true,
	"contintegraldisplay":   true,
	"contintegraltext":      true,
	"controlNULL":           true,
	"coproductdisplay":      true,
	"coproducttext":         true,
	"f_f":                   true,
	"f_f_i":                 true,
	"f_f_l":                 true,
	"f_i":                   true,
	"f_l":                   true,
	"floorleftBig":          true,
	"floorleftBigg":         true,
	"floorleftbig":          true,
	"floorleftbigg":         true,
	"floorrightBig":         true,
	"floorrightBigg":        true,
	"floorrightbig":         true,
	"floorrightbigg":        true,
	"hatwide":               true,
	"hatwider":              true,
	"hatwidest":             true,
	"hyphenation":           true,
	"integraldisplay":       true,
	"integraltext":          true,
	"intercal":              true,
	"intersectiondisplay":   true,
	"intersectiontext":      true,
	"logicalanddisplay":     true,
	"logicalandtext":        true,
	"logicalordisplay":      true,
	"logicalortext":         true,
	"parenleftBig":          true,
	"parenleftBigg":         true,
	"parenleftbig":          true,
	"parenleftbigg":         true,
	"parenrightBig":         true,
	"parenrightBigg":        true,
	"parenrightbig":         true,
	"parenrightbigg":        true,
	"planck":                true,
	"planckover2pi1":        true,
	"prime":                 true,
	"productdisplay":        true,
	"producttext":           true,
	"radicalBig":            true,
	"radicalBigg":           true,
	"radicalbig":            true,
	"radicalbigg":           true,
	"radicalbt":             true,
	"radicaltp":             true,
	"radicalvertex":         true,
	"slashBig":              true,
	"slashBigg":             true,
	"slashbig":              true,
	"slashbigg":             true,
	"stroke":                true,
	"summationdisplay":      true,
	"summationtext":         true,
	"tildewide":             true,
	"tildewider":            true,
	"tildewidest":           true,
	"uniondisplay":          true,
	"unionmultidisplay":     true,
	"unionmultitext":        true,
	"unionsqdisplay":        true,
	"unionsqtext":           true,
	"uniontext":             true,
	"vextenddouble":         true,
	"vextendsingle":         true,
}
