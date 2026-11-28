package text

import "strings"

// urwAccepts are the families the URW base 35 files of fontconfig,
// conf.d/61-urw-*.conf, let a URW face stand in for under a "same"
// binding, by family key, where [metricAliasGroups] names no face the
// image installs: Palatino Linotype takes P052 as Palatino does.
var urwAccepts = map[string]string{
	familyKey("Palatino Linotype"):  "P052",
	familyKey("Bookman Old Style"):  "URW Bookman",
	familyKey("ITC Bookman"):        "URW Bookman",
	familyKey("TeX Gyre Bonum"):     "URW Bookman",
	familyKey("Century Schoolbook"): "C059",
	familyKey("Century"):            "C059",
}

// The faces the fontconfig of the reference image ranks first for each
// generic family: Noto Serif, Noto Sans, DejaVu Sans Mono and Z003, by
// the prefer lists of its conf.d/60-latin.conf and 61-urw-z003.conf.
const (
	libreOfficeSerif     = "Noto Serif"
	libreOfficeSansSerif = "Noto Sans"
	libreOfficeMono      = "DejaVu Sans Mono"
	libreOfficeCursive   = "Z003"
)

// LibreOfficeSubstitute returns the face LibreOffice draws family with
// when no installed font serves family or its metric aliases: a family a
// URW face stands in for takes that face; else a fixed-pitch family
// (style:font-pitch, ODF 1.3, Part 3, 19.511) the first monospace face;
// else a family declared serif or sans-serif (declared, the CSS generic
// family of its style:font-family-generic, 19.512) the first face of that
// generic; else the first face of the generic family fontconfig gives
// family ([familyGeneric]), sans-serif for one it gives none or one it
// has no installed face for.
//
// Writer and Calc read declared; Impress does not, and passes "".
//
// # Corpus evidence
//
//   - probe, odt: in the gotenberg/gotenberg:9.4.1 image, Writer prints
//     Comic Sans MS in Z003, Consolas in DejaVu Sans Mono, Garamond and
//     Georgia in Noto Serif, Palatino Linotype in P052,
//     Century Schoolbook in C059, Bookman Old Style in URW Bookman, and
//     Aptos, Impact, Segoe UI, Book Antiqua, Lucida Console and
//     Times-Roman in Noto Sans; declared roman each draws in Noto Serif
//     and declared swiss in Noto Sans, but the three URW stand-ins, which
//     stay; declared modern, script, decorative or system each draws as
//     undeclared. A fixed pitch draws Aptos and Comic Sans MS in DejaVu
//     Sans Mono, declared roman or swiss included, and Palatino Linotype
//     in P052.
//   - probe, ods: Calc prints Comic Sans MS declared roman in Noto Serif,
//     Consolas declared swiss in Noto Sans, Palatino Linotype declared
//     swiss in P052, and Aptos of fixed pitch in DejaVu Sans Mono.
//   - probe, odp: Impress prints Comic Sans MS declared roman in Z003,
//     Consolas declared swiss in DejaVu Sans Mono, Aptos declared roman in
//     Noto Sans, and Aptos of fixed pitch in DejaVu Sans Mono.
//   - libreoffice/ods: chart2/tdf108021.ods prints its chart's Comic Sans
//     MS in Z003.
func LibreOfficeSubstitute(family string, fixed bool, declared string) string {
	// A converter asks once a run: an ASCII name's key is built on the
	// stack, so the call allocates nothing.
	var buf [64]byte
	key := asciiFamilyKey(buf[:0], family)
	if key == nil {
		key = []byte(familyKey(family))
	}
	if face, ok := urwAccepts[string(key)]; ok {
		return face
	}
	if fixed {
		return libreOfficeMono
	}
	switch genericOf(declared) {
	case genericSerif:
		return libreOfficeSerif
	case genericSansSerif:
		return libreOfficeSansSerif
	case genericNone, genericMonospace, genericCursive, genericFantasy, genericSystemUI, genericMath:
	}
	switch familyGeneric(string(key)) {
	case genericSerif:
		return libreOfficeSerif
	case genericMonospace:
		return libreOfficeMono
	case genericCursive:
		return libreOfficeCursive
	case genericNone, genericSansSerif, genericFantasy, genericSystemUI, genericMath:
	}
	return libreOfficeSansSerif
}

// asciiFamilyKey appends to dst the [familyKey] of an ASCII name and
// returns it; nil for a name of other bytes, or one longer than dst can
// hold.
func asciiFamilyKey(dst []byte, name string) []byte {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		name = strings.TrimSpace(name[1 : len(name)-1])
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 0x80:
			return nil
		case c == ' ' || c == '\t':
			continue
		case 'A' <= c && c <= 'Z':
			c += 'a' - 'A'
		}
		if len(dst) == cap(dst) {
			return nil
		}
		dst = append(dst, c)
	}
	return dst
}
