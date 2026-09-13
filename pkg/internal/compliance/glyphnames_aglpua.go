package compliance

// aglPUAGlyphNames lists every Adobe Glyph List name whose Unicode value falls
// in a Private Use Area. Adobe assigned its corporate use subarea (U+F600 to
// U+F8FF) to glyphs Unicode had no encoding for when the list was written:
// small capitals, oldstyle figures, and a handful of spacing accents.
//
// ISO 14289-2, 8.4.3 asks whether real content maps to a PUA value, and ISO
// 32000-1, 9.10.2 derives that value from the glyph name, so a font mapping a
// shown code to one of these names reaches the PUA with no /ToUnicode CMap in
// sight. litho's typography tables serve text extraction and prefer the modern
// encodings Unicode later assigned, which is the better answer for extracted
// text and the wrong one here: they map dotlessj to U+0237 and commaaccent to
// U+0326, where the Adobe Glyph List says U+F6BE and U+F6C3.
//
// Generated from the Adobe Glyph List; every entry was cross-checked against
// veraPDF's own AdobeGlyphList (1.30.2) and fontTools, which agree on all 192.
//
//nolint:gochecknoglobals // static lookup table.
var aglPUAGlyphNames = map[string]bool{
	"AEsmall": true, "Aacutesmall": true, "Acircumflexsmall": true,
	"Acute": true, "Acutesmall": true, "Adieresissmall": true,
	"Agravesmall": true, "Aringsmall": true, "Asmall": true,
	"Atildesmall": true, "Brevesmall": true, "Bsmall": true,
	"Caron": true, "Caronsmall": true, "Ccedillasmall": true,
	"Cedillasmall": true, "Circumflexsmall": true, "Csmall": true,
	"Dieresis": true, "DieresisAcute": true, "DieresisGrave": true,
	"Dieresissmall": true, "Dotaccentsmall": true, "Dsmall": true,
	"Eacutesmall": true, "Ecircumflexsmall": true, "Edieresissmall": true,
	"Egravesmall": true, "Esmall": true, "Ethsmall": true,
	"Fsmall": true, "Grave": true, "Gravesmall": true,
	"Gsmall": true, "Hsmall": true, "Hungarumlaut": true,
	"Hungarumlautsmall": true, "Iacutesmall": true, "Icircumflexsmall": true,
	"Idieresissmall": true, "Igravesmall": true, "Ismall": true,
	"Jsmall": true, "Ksmall": true, "LL": true,
	"Lslashsmall": true, "Lsmall": true, "Macron": true,
	"Macronsmall": true, "Msmall": true, "Nsmall": true,
	"Ntildesmall": true, "OEsmall": true, "Oacutesmall": true,
	"Ocircumflexsmall": true, "Odieresissmall": true, "Ogoneksmall": true,
	"Ogravesmall": true, "Oslashsmall": true, "Osmall": true,
	"Otildesmall": true, "Psmall": true, "Qsmall": true,
	"Ringsmall": true, "Rsmall": true, "Scaronsmall": true,
	"Ssmall": true, "Thornsmall": true, "Tildesmall": true,
	"Tsmall": true, "Uacutesmall": true, "Ucircumflexsmall": true,
	"Udieresissmall": true, "Ugravesmall": true, "Usmall": true,
	"Vsmall": true, "Wsmall": true, "Xsmall": true,
	"Yacutesmall": true, "Ydieresissmall": true, "Ysmall": true,
	"Zcaronsmall": true, "Zsmall": true, "afii10063": true,
	"afii10064": true, "afii10192": true, "afii10831": true,
	"afii10832": true, "ampersandsmall": true, "apple": true,
	"arrowhorizex": true, "arrowvertex": true, "asuperior": true,
	"braceex": true, "braceleftbt": true, "braceleftmid": true,
	"bracelefttp": true, "bracerightbt": true, "bracerightmid": true,
	"bracerighttp": true, "bracketleftbt": true, "bracketleftex": true,
	"bracketlefttp": true, "bracketrightbt": true, "bracketrightex": true,
	"bracketrighttp": true, "bsuperior": true, "centinferior": true,
	"centoldstyle": true, "centsuperior": true, "commaaccent": true,
	"commainferior": true, "commasuperior": true, "copyrightsans": true,
	"copyrightserif": true, "cyrBreve": true, "cyrFlex": true,
	"cyrbreve": true, "cyrflex": true, "dblGrave": true,
	"dblgrave": true, "dieresisacute": true, "dieresisgrave": true,
	"dollarinferior": true, "dollaroldstyle": true, "dollarsuperior": true,
	"dotlessj": true, "dsuperior": true, "eightoldstyle": true,
	"esuperior": true, "exclamdownsmall": true, "exclamsmall": true,
	"fiveoldstyle": true, "fouroldstyle": true, "hypheninferior": true,
	"hyphensuperior": true, "integralex": true, "isuperior": true,
	"ll": true, "lsuperior": true, "maichattawalowleftthai": true,
	"maichattawalowrightthai": true, "maichattawaupperleftthai": true, "maieklowleftthai": true,
	"maieklowrightthai": true, "maiekupperleftthai": true, "maihanakatleftthai": true,
	"maitaikhuleftthai": true, "maitholowleftthai": true, "maitholowrightthai": true,
	"maithoupperleftthai": true, "maitrilowleftthai": true, "maitrilowrightthai": true,
	"maitriupperleftthai": true, "msuperior": true, "nikhahitleftthai": true,
	"nineoldstyle": true, "onefitted": true, "oneoldstyle": true,
	"osuperior": true, "parenleftbt": true, "parenleftex": true,
	"parenlefttp": true, "parenrightbt": true, "parenrightex": true,
	"parenrighttp": true, "periodinferior": true, "periodsuperior": true,
	"questiondownsmall": true, "questionsmall": true, "radicalex": true,
	"registersans": true, "registerserif": true, "rsuperior": true,
	"rupiah": true, "saraiileftthai": true, "saraileftthai": true,
	"saraueeleftthai": true, "saraueleftthai": true, "sevenoldstyle": true,
	"sixoldstyle": true, "ssuperior": true, "thanthakhatlowleftthai": true,
	"thanthakhatlowrightthai": true, "thanthakhatupperleftthai": true, "threeoldstyle": true,
	"threequartersemdash": true, "trademarksans": true, "trademarkserif": true,
	"tsuperior": true, "twooldstyle": true, "zerooldstyle": true,
}
