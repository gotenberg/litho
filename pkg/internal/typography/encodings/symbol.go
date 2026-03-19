package encodings

// SymbolSetEncoding maps byte values to glyph names for the Symbol font.
// Sourced from PDF.js src/core/encodings.js.
var SymbolSetEncoding = [256]string{
	// 0-31: empty
	32: "space", 33: "exclam", 34: "universal", 35: "numbersign",
	36: "existential", 37: "percent", 38: "ampersand", 39: "suchthat",
	40: "parenleft", 41: "parenright", 42: "asteriskmath", 43: "plus",
	44: "comma", 45: "minus", 46: "period", 47: "slash",
	48: "zero", 49: "one", 50: "two", 51: "three", 52: "four",
	53: "five", 54: "six", 55: "seven", 56: "eight", 57: "nine",
	58: "colon", 59: "semicolon", 60: "less", 61: "equal",
	62: "greater", 63: "question", 64: "congruent",
	65: "Alpha", 66: "Beta", 67: "Chi", 68: "Delta", 69: "Epsilon",
	70: "Phi", 71: "Gamma", 72: "Eta", 73: "Iota", 74: "theta1",
	75: "Kappa", 76: "Lambda", 77: "Mu", 78: "Nu", 79: "Omicron",
	80: "Pi", 81: "Theta", 82: "Rho", 83: "Sigma", 84: "Tau",
	85: "Upsilon", 86: "sigma1", 87: "Omega", 88: "Xi", 89: "Psi",
	90: "Zeta", 91: "bracketleft", 92: "therefore", 93: "bracketright",
	94: "perpendicular", 95: "underscore", 96: "radicalex",
	97: "alpha", 98: "beta", 99: "chi", 100: "delta", 101: "epsilon",
	102: "phi", 103: "gamma", 104: "eta", 105: "iota", 106: "phi1",
	107: "kappa", 108: "lambda", 109: "mu", 110: "nu", 111: "omicron",
	112: "pi", 113: "theta", 114: "rho", 115: "sigma", 116: "tau",
	117: "upsilon", 118: "omega1", 119: "omega", 120: "xi", 121: "psi",
	122: "zeta", 123: "braceleft", 124: "bar", 125: "braceright",
	126: "similar",
	160: "Euro", 161: "Upsilon1", 162: "minute", 163: "lessequal",
	164: "fraction", 165: "infinity", 166: "florin", 167: "club",
	168: "diamond", 169: "heart", 170: "spade", 171: "arrowboth",
	172: "arrowleft", 173: "arrowup", 174: "arrowright", 175: "arrowdown",
	176: "degree", 177: "plusminus", 178: "second", 179: "greaterequal",
	180: "multiply", 181: "proportional", 182: "partialdiff", 183: "bullet",
	184: "divide", 185: "notequal", 186: "equivalence", 187: "approxequal",
	188: "ellipsis", 189: "arrowvertex", 190: "arrowhorizex",
	191: "carriagereturn", 192: "aleph", 193: "Ifraktur", 194: "Rfraktur",
	195: "weierstrass", 196: "circlemultiply", 197: "circleplus",
	198: "emptyset", 199: "intersection", 200: "union",
	201: "propersuperset", 202: "reflexsuperset", 203: "notsubset",
	204: "propersubset", 205: "reflexsubset", 206: "element",
	207: "notelement", 208: "angle", 209: "gradient",
	210: "registerserif", 211: "copyrightserif", 212: "trademarkserif",
	213: "product", 214: "radical", 215: "dotmath", 216: "logicalnot",
	217: "logicaland", 218: "logicalor", 219: "arrowdblboth",
	220: "arrowdblleft", 221: "arrowdblup", 222: "arrowdblright",
	223: "arrowdbldown", 224: "lozenge", 225: "angleleft",
	226: "registersans", 227: "copyrightsans", 228: "trademarksans",
	229: "summation", 230: "parenlefttp", 231: "parenleftex",
	232: "parenleftbt", 233: "bracketlefttp", 234: "bracketleftex",
	235: "bracketleftbt", 236: "bracelefttp", 237: "braceleftmid",
	238: "braceleftbt", 239: "braceex",
	241: "angleright", 242: "integral", 243: "integraltp",
	244: "integralex", 245: "integralbt", 246: "parenrighttp",
	247: "parenrightex", 248: "parenrightbt", 249: "bracketrighttp",
	250: "bracketrightex", 251: "bracketrightbt", 252: "bracerighttp",
	253: "bracerightmid", 254: "bracerightbt",
}

// ZapfDingbatsEncoding maps byte values to glyph names for the ZapfDingbats font.
// Sourced from PDF.js src/core/encodings.js.
var ZapfDingbatsEncoding = [256]string{
	32: "space", 33: "a1", 34: "a2", 35: "a202", 36: "a3", 37: "a4",
	38: "a5", 39: "a119", 40: "a118", 41: "a117", 42: "a11", 43: "a12",
	44: "a13", 45: "a14", 46: "a15", 47: "a16", 48: "a105", 49: "a17",
	50: "a18", 51: "a19", 52: "a20", 53: "a21", 54: "a22", 55: "a23",
	56: "a24", 57: "a25", 58: "a26", 59: "a27", 60: "a28", 61: "a6",
	62: "a7", 63: "a8", 64: "a9", 65: "a10", 66: "a29", 67: "a30",
	68: "a31", 69: "a32", 70: "a33", 71: "a34", 72: "a35", 73: "a36",
	74: "a37", 75: "a38", 76: "a39", 77: "a40", 78: "a41", 79: "a42",
	80: "a43", 81: "a44", 82: "a45", 83: "a46", 84: "a47", 85: "a48",
	86: "a49", 87: "a50", 88: "a51", 89: "a52", 90: "a53", 91: "a54",
	92: "a55", 93: "a56", 94: "a57", 95: "a58", 96: "a59", 97: "a60",
	98: "a61", 99: "a62", 100: "a63", 101: "a64", 102: "a65", 103: "a66",
	104: "a67", 105: "a68", 106: "a69", 107: "a70", 108: "a71", 109: "a72",
	110: "a73", 111: "a74", 112: "a203", 113: "a75", 114: "a204",
	115: "a76", 116: "a77", 117: "a78", 118: "a79", 119: "a81",
	120: "a82", 121: "a83", 122: "a84", 123: "a97", 124: "a98",
	125: "a99", 126: "a100",
	128: "a89", 129: "a90", 130: "a93", 131: "a94", 132: "a91",
	133: "a92", 134: "a205", 135: "a85", 136: "a206", 137: "a86",
	138: "a87", 139: "a88", 140: "a95", 141: "a96",
	161: "a101", 162: "a102", 163: "a103", 164: "a104", 165: "a106",
	166: "a107", 167: "a108", 168: "a112", 169: "a111", 170: "a110",
	171: "a109", 172: "a120", 173: "a121", 174: "a122", 175: "a123",
	176: "a124", 177: "a125", 178: "a126", 179: "a127", 180: "a128",
	181: "a129", 182: "a130", 183: "a131", 184: "a132", 185: "a133",
	186: "a134", 187: "a135", 188: "a136", 189: "a137", 190: "a138",
	191: "a139", 192: "a140", 193: "a141", 194: "a142", 195: "a143",
	196: "a144", 197: "a145", 198: "a146", 199: "a147", 200: "a148",
	201: "a149", 202: "a150", 203: "a151", 204: "a152", 205: "a153",
	206: "a154", 207: "a155", 208: "a156", 209: "a157", 210: "a158",
	211: "a159", 212: "a160", 213: "a161", 214: "a163", 215: "a164",
	216: "a196", 217: "a165", 218: "a192", 219: "a166", 220: "a167",
	221: "a168", 222: "a169", 223: "a170", 224: "a171", 225: "a172",
	226: "a173", 227: "a162", 228: "a174", 229: "a175", 230: "a176",
	231: "a177", 232: "a178", 233: "a179", 234: "a193", 235: "a180",
	236: "a199", 237: "a181", 238: "a200", 239: "a182",
	241: "a201", 242: "a183", 243: "a184", 244: "a197", 245: "a185",
	246: "a194", 247: "a198", 248: "a186", 249: "a195", 250: "a187",
	251: "a188", 252: "a189", 253: "a190", 254: "a191",
}

// DingbatsGlyphsUnicode maps ZapfDingbats glyph names (a1-a206) to Unicode runes.
// Sourced from PDF.js src/core/glyphlist.js (getDingbatsGlyphsUnicode).
var DingbatsGlyphsUnicode = map[string]rune{
	"a1": 0x2701, "a2": 0x2702, "a3": 0x2704, "a4": 0x260E, "a5": 0x2706,
	"a6": 0x271D, "a7": 0x271E, "a8": 0x271F, "a9": 0x2720, "a10": 0x2721,
	"a11": 0x261B, "a12": 0x261E, "a13": 0x270C, "a14": 0x270D, "a15": 0x270E,
	"a16": 0x270F, "a17": 0x2711, "a18": 0x2712, "a19": 0x2713, "a20": 0x2714,
	"a21": 0x2715, "a22": 0x2716, "a23": 0x2717, "a24": 0x2718, "a25": 0x2719,
	"a26": 0x271A, "a27": 0x271B, "a28": 0x271C, "a29": 0x2722, "a30": 0x2723,
	"a31": 0x2724, "a32": 0x2725, "a33": 0x2726, "a34": 0x2727, "a35": 0x2605,
	"a36": 0x2729, "a37": 0x272A, "a38": 0x272B, "a39": 0x272C, "a40": 0x272D,
	"a41": 0x272E, "a42": 0x272F, "a43": 0x2730, "a44": 0x2731, "a45": 0x2732,
	"a46": 0x2733, "a47": 0x2734, "a48": 0x2735, "a49": 0x2736, "a50": 0x2737,
	"a51": 0x2738, "a52": 0x2739, "a53": 0x273A, "a54": 0x273B, "a55": 0x273C,
	"a56": 0x273D, "a57": 0x273E, "a58": 0x273F, "a59": 0x2740, "a60": 0x2741,
	"a61": 0x2742, "a62": 0x2743, "a63": 0x2744, "a64": 0x2745, "a65": 0x2746,
	"a66": 0x2747, "a67": 0x2748, "a68": 0x2749, "a69": 0x274A, "a70": 0x274B,
	"a71": 0x25CF, "a72": 0x274D, "a73": 0x25A0, "a74": 0x274F, "a75": 0x2751,
	"a76": 0x25B2, "a77": 0x25BC, "a78": 0x25C6, "a79": 0x2756, "a81": 0x25D7,
	"a82": 0x2758, "a83": 0x2759, "a84": 0x275A, "a85": 0x276F, "a86": 0x2771,
	"a87": 0x2772, "a88": 0x2773, "a89": 0x2768, "a90": 0x2769, "a91": 0x276C,
	"a92": 0x276D, "a93": 0x276A, "a94": 0x276B, "a95": 0x2774, "a96": 0x2775,
	"a97": 0x275B, "a98": 0x275C, "a99": 0x275D, "a100": 0x275E, "a101": 0x2761,
	"a102": 0x2762, "a103": 0x2763, "a104": 0x2764, "a105": 0x2710, "a106": 0x2765,
	"a107": 0x2766, "a108": 0x2767, "a109": 0x2660, "a110": 0x2665, "a111": 0x2666,
	"a112": 0x2663, "a117": 0x2709, "a118": 0x2708, "a119": 0x2707, "a120": 0x2460,
	"a121": 0x2461, "a122": 0x2462, "a123": 0x2463, "a124": 0x2464, "a125": 0x2465,
	"a126": 0x2466, "a127": 0x2467, "a128": 0x2468, "a129": 0x2469, "a130": 0x2776,
	"a131": 0x2777, "a132": 0x2778, "a133": 0x2779, "a134": 0x277A, "a135": 0x277B,
	"a136": 0x277C, "a137": 0x277D, "a138": 0x277E, "a139": 0x277F, "a140": 0x2780,
	"a141": 0x2781, "a142": 0x2782, "a143": 0x2783, "a144": 0x2784, "a145": 0x2785,
	"a146": 0x2786, "a147": 0x2787, "a148": 0x2788, "a149": 0x2789, "a150": 0x278A,
	"a151": 0x278B, "a152": 0x278C, "a153": 0x278D, "a154": 0x278E, "a155": 0x278F,
	"a156": 0x2790, "a157": 0x2791, "a158": 0x2792, "a159": 0x2793, "a160": 0x2794,
	"a161": 0x2192, "a162": 0x27A3, "a163": 0x2194, "a164": 0x2195, "a165": 0x2192,
	"a166": 0x279B, "a167": 0x279C, "a168": 0x279D, "a169": 0x279E, "a170": 0x279F,
	"a171": 0x27A0, "a172": 0x27A1, "a173": 0x27A2, "a174": 0x27A4, "a175": 0x27A5,
	"a176": 0x27A6, "a177": 0x27A7, "a178": 0x27A8, "a179": 0x27A9, "a180": 0x27AB,
	"a181": 0x27AD, "a182": 0x27AF, "a183": 0x27B2, "a184": 0x27B3, "a185": 0x27B5,
	"a186": 0x27B8, "a187": 0x27BA, "a188": 0x27BB, "a189": 0x27BC, "a190": 0x27BD,
	"a191": 0x27BE, "a192": 0x279A, "a193": 0x27AA, "a194": 0x27B6, "a195": 0x27B9,
	"a196": 0x2798, "a197": 0x27B4, "a198": 0x27B7, "a199": 0x27AC, "a200": 0x27AE,
	"a201": 0x27B1, "a202": 0x2703, "a203": 0x2750, "a204": 0x2752, "a205": 0x276E,
	"a206": 0x2770,
}

// SymbolGlyphsUnicode maps Symbol font glyph names to Unicode runes.
// Entries that differ from or don't exist in the standard AGL.
var SymbolGlyphsUnicode = map[string]rune{
	"universal": 0x2200, "existential": 0x2203, "suchthat": 0x220B,
	"asteriskmath": 0x2217, "congruent": 0x2245, "Alpha": 0x0391,
	"Beta": 0x0392, "Chi": 0x03A7, "Delta": 0x0394,
	"Epsilon": 0x0395, "Phi": 0x03A6, "Gamma": 0x0393,
	"Eta": 0x0397, "Iota": 0x0399, "theta1": 0x03D1,
	"Kappa": 0x039A, "Lambda": 0x039B, "Mu": 0x039C,
	"Nu": 0x039D, "Omicron": 0x039F, "Pi": 0x03A0,
	"Theta": 0x0398, "Rho": 0x03A1, "Sigma": 0x03A3,
	"Tau": 0x03A4, "Upsilon": 0x03A5, "sigma1": 0x03C2,
	"Omega": 0x03A9, "Xi": 0x039E, "Psi": 0x03A8,
	"Zeta": 0x0396, "therefore": 0x2234, "perpendicular": 0x22A5,
	"radicalex": 0xF8E5, "alpha": 0x03B1, "beta": 0x03B2,
	"chi": 0x03C7, "delta": 0x03B4, "epsilon": 0x03B5,
	"phi": 0x03C6, "gamma": 0x03B3, "eta": 0x03B7,
	"iota": 0x03B9, "phi1": 0x03D5, "kappa": 0x03BA,
	"lambda": 0x03BB, "mu": 0x03BC, "nu": 0x03BD,
	"omicron": 0x03BF, "pi": 0x03C0, "theta": 0x03B8,
	"rho": 0x03C1, "sigma": 0x03C3, "tau": 0x03C4,
	"upsilon": 0x03C5, "omega1": 0x03D6, "omega": 0x03C9,
	"xi": 0x03BE, "psi": 0x03C8, "zeta": 0x03B6,
	"similar": 0x223C, "Upsilon1": 0x03D2, "minute": 0x2032,
	"lessequal": 0x2264, "infinity": 0x221E, "club": 0x2663,
	"diamond": 0x2666, "heart": 0x2665, "spade": 0x2660,
	"arrowboth": 0x2194, "arrowleft": 0x2190, "arrowup": 0x2191,
	"arrowright": 0x2192, "arrowdown": 0x2193, "second": 0x2033,
	"greaterequal": 0x2265, "proportional": 0x221D, "partialdiff": 0x2202,
	"notequal": 0x2260, "equivalence": 0x2261, "approxequal": 0x2248,
	"arrowvertex": 0xF8E6, "arrowhorizex": 0xF8E7, "carriagereturn": 0x21B5,
	"aleph": 0x2135, "Ifraktur": 0x2111, "Rfraktur": 0x211C,
	"weierstrass": 0x2118, "circlemultiply": 0x2297, "circleplus": 0x2295,
	"emptyset": 0x2205, "intersection": 0x2229, "union": 0x222A,
	"propersuperset": 0x2283, "reflexsuperset": 0x2287, "notsubset": 0x2284,
	"propersubset": 0x2282, "reflexsubset": 0x2286, "element": 0x2208,
	"notelement": 0x2209, "angle": 0x2220, "gradient": 0x2207,
	"registerserif": 0xF6DA, "copyrightserif": 0xF6D9, "trademarkserif": 0xF6DB,
	"product": 0x220F, "radical": 0x221A, "dotmath": 0x22C5,
	"logicalnot": 0x00AC, "logicaland": 0x2227, "logicalor": 0x2228,
	"arrowdblboth": 0x21D4, "arrowdblleft": 0x21D0, "arrowdblup": 0x21D1,
	"arrowdblright": 0x21D2, "arrowdbldown": 0x21D3, "lozenge": 0x25CA,
	"angleleft": 0x2329, "registersans": 0xF8E8, "copyrightsans": 0xF6D8,
	"trademarksans": 0xF8EA, "summation": 0x2211, "parenlefttp": 0xF8EB,
	"parenleftex": 0xF8EC, "parenleftbt": 0xF8ED, "bracketlefttp": 0xF8EE,
	"bracketleftex": 0xF8EF, "bracketleftbt": 0xF8F0, "bracelefttp": 0xF8F1,
	"braceleftmid": 0xF8F2, "braceleftbt": 0xF8F3, "braceex": 0xF8F4,
	"angleright": 0x232A, "integral": 0x222B, "integraltp": 0x2320,
	"integralex": 0xF8F5, "integralbt": 0x2321, "parenrighttp": 0xF8F6,
	"parenrightex": 0xF8F7, "parenrightbt": 0xF8F8, "bracketrighttp": 0xF8F9,
	"bracketrightex": 0xF8FA, "bracketrightbt": 0xF8FB, "bracerighttp": 0xF8FC,
	"bracerightmid": 0xF8FD, "bracerightbt": 0xF8FE,
	"Euro": 0x20AC,
}

// BuildSymbolEncoding builds a byte→rune encoding map for Symbol fonts by
// resolving glyph names from SymbolSetEncoding through SymbolGlyphsUnicode,
// then falling back to the standard glyph list.
func BuildSymbolEncoding() map[byte]rune {
	enc := make(map[byte]rune, 128)
	for i := range 256 {
		name := SymbolSetEncoding[i]
		if name == "" {
			continue
		}
		if r, ok := SymbolGlyphsUnicode[name]; ok {
			enc[byte(i)] = r
		} else if r, ok := GlyphToRune[name]; ok {
			enc[byte(i)] = r
		} else if r, ok := extendedAGL[name]; ok {
			enc[byte(i)] = r
		}
	}
	return enc
}

// BuildZapfDingbatsEncoding builds a byte→rune encoding map for ZapfDingbats
// fonts by resolving glyph names from ZapfDingbatsEncoding through
// DingbatsGlyphsUnicode.
func BuildZapfDingbatsEncoding() map[byte]rune {
	enc := make(map[byte]rune, 128)
	for i := range 256 {
		name := ZapfDingbatsEncoding[i]
		if name == "" {
			continue
		}
		if r, ok := DingbatsGlyphsUnicode[name]; ok {
			enc[byte(i)] = r
		} else if r, ok := GlyphToRune[name]; ok {
			enc[byte(i)] = r
		}
	}
	return enc
}
