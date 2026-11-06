package text

import (
	"slices"
	"strings"

	"github.com/go-text/typesetting/language"
)

// Slants of a face or a request, in the order CSS Fonts 4 (5.2, font-style)
// falls back from italic.
const (
	slantNormal uint8 = iota
	slantItalic
	slantOblique
)

// widthNormal is the OS/2 usWidthClass of a normal width (ISO/IEC 14496-22,
// OS/2 table, usWidthClass).
const widthNormal = 5

// familyKey returns the form family names are compared in: lower case,
// without spaces or tabs, so "DejaVu Sans" and "dejavusans" match.
func familyKey(name string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(name)))
}

// generic names a CSS generic family.
type generic uint8

const (
	genericNone generic = iota
	genericSerif
	genericSansSerif
	genericMonospace
	genericCursive
	genericFantasy
	genericSystemUI
)

// genericOf returns the generic family name stands for, genericNone for a
// concrete family.
func genericOf(name string) generic {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "serif":
		return genericSerif
	case "sans-serif":
		return genericSansSerif
	case "monospace":
		return genericMonospace
	case "cursive":
		return genericCursive
	case "fantasy":
		return genericFantasy
	case "system-ui":
		return genericSystemUI
	}
	return genericNone
}

// genericFamilies are the preference lists of the generic families.
//
// The serif, sans-serif and monospace lists mirror the prefer lists of
// fontconfig's conf.d/60-latin.conf, and each ends with the Noto family of
// the generic as a last resort. The cursive, fantasy and system-ui lists
// mirror the shorter lists of the same file; system-ui then takes the
// sans-serif list. Every family a list names brings its metric-compatible
// aliases ([metricAliases]) right after it.
var genericFamilies = map[generic][]string{
	genericSerif: {
		"DejaVu Serif", "Times New Roman", "Thorndale AMT", "Luxi Serif",
		"Nimbus Roman No9 L", "Nimbus Roman", "Times", "Noto Serif",
	},
	genericSansSerif: {
		"DejaVu Sans", "Verdana", "Arial", "Albany AMT", "Luxi Sans",
		"Nimbus Sans L", "Nimbus Sans", "Helvetica", "Lucida Sans Unicode",
		"BPG Glaho International", "Tahoma", "Noto Sans",
	},
	genericMonospace: {
		"DejaVu Sans Mono", "Inconsolata", "Andale Mono", "Courier New",
		"Cumberland AMT", "Luxi Mono", "Nimbus Mono L", "Nimbus Mono",
		"Nimbus Mono PS", "Courier", "Noto Sans Mono",
	},
	genericCursive: {
		"TeX Gyre Chorus", "URW Chancery L", "Z003", "ITC Zapf Chancery",
		"Zapfino", "Comic Sans MS", "Comic Neue",
	},
	genericFantasy: {
		"Impact", "Copperplate Gothic Std", "Cooper Std", "Bauhaus Std", "Papyrus",
	},
	genericSystemUI: {
		"Cantarell", "Noto Sans UI", "Segoe UI",
	},
}

// metricAliasGroups are the metric-compatible families of fontconfig's
// conf.d/30-metric-aliases.conf: families that share every advance width,
// so text set in one keeps its line breaks in another. Each group is a list
// of clusters. A family's aliases are the rest of its cluster in order, then
// the other clusters of its group in order, so Arial resolves to Liberation
// Sans, Arimo and Albany before Helvetica and its clones, and Liberation
// Sans back to Arial first.
var metricAliasGroups = [][][]string{
	{
		{"Arial", "Liberation Sans", "Arimo", "Albany", "Albany AMT"},
		{"Helvetica", "Nimbus Sans", "Nimbus Sans L", "TeX Gyre Heros"},
	},
	{
		{"Arial Narrow", "Liberation Sans Narrow"},
		{"Helvetica Narrow", "Nimbus Sans Narrow", "TeX Gyre Heros Cn"},
	},
	{
		{"Times New Roman", "Liberation Serif", "Tinos", "Thorndale", "Thorndale AMT"},
		{"Times", "Nimbus Roman", "Nimbus Roman No9 L", "TeX Gyre Termes"},
	},
	{
		{"Courier New", "Liberation Mono", "Cousine", "Cumberland", "Cumberland AMT"},
		{"Courier", "Nimbus Mono PS", "Nimbus Mono", "Nimbus Mono L", "TeX Gyre Cursor"},
	},
	{{"Georgia", "Gelasio"}},
	{{"Cambria", "Caladea"}},
	{{"Calibri", "Carlito"}},
	{{"Symbol", "Standard Symbols PS", "Standard Symbols L", "SymbolNeu"}},
	{{"Zapf Dingbats", "ITC Zapf Dingbats", "D050000L", "Dingbats"}},
	{{"ITC Avant Garde Gothic", "URW Gothic", "URW Gothic L", "TeX Gyre Adventor"}},
	{{"ITC Bookman", "Bookman URW", "URW Bookman L", "TeX Gyre Bonum"}},
	{{"ITC Zapf Chancery", "Z003", "URW Chancery L", "TeX Gyre Chorus"}},
	{{"Palatino", "P052", "URW Palladio L", "TeX Gyre Pagella"}},
	{{"New Century Schoolbook", "C059", "Century Schoolbook URW", "Century Schoolbook L", "TeX Gyre Schola"}},
}

// metricAliases maps a family key to the keys of its metric-compatible
// families, in preference order, built from [metricAliasGroups].
var metricAliases = buildMetricAliases()

// buildMetricAliases flattens [metricAliasGroups]. A family listed in two
// groups keeps the aliases of the first.
func buildMetricAliases() map[string][]string {
	out := make(map[string][]string)
	for _, group := range metricAliasGroups {
		for ci, cluster := range group {
			for fi, family := range cluster {
				key := familyKey(family)
				if _, ok := out[key]; ok {
					continue
				}
				var aliases []string
				for k, other := range cluster {
					if k != fi {
						aliases = append(aliases, familyKey(other))
					}
				}
				for cj, rest := range group {
					if cj == ci {
						continue
					}
					for _, other := range rest {
						aliases = append(aliases, familyKey(other))
					}
				}
				out[key] = aliases
			}
		}
	}
	return out
}

// cjkRegion names the Noto CJK variant a language reads: JP, SC, TC, HK or
// KR, "" for a language that is not Chinese, Japanese or Korean.
func cjkRegion(lang string) string {
	parts := strings.FieldsFunc(strings.ToLower(lang), func(r rune) bool { return r == '-' || r == '_' })
	if len(parts) == 0 {
		return ""
	}
	switch parts[0] {
	case "ja":
		return "JP"
	case "ko":
		return "KR"
	case "zh":
		for _, p := range parts[1:] {
			switch p {
			case "hk", "mo":
				return "HK"
			case "tw", "hant":
				return "TC"
			case "cn", "sg", "hans":
				return "SC"
			}
		}
		return "SC"
	}
	return ""
}

// cjkRegions is the order the Noto CJK variants are tried in for a
// character of a CJK script: Japanese first, as fontconfig's
// conf.d/70-noto-cjk.conf orders them for a pattern without a language.
var cjkRegions = []string{"JP", "SC", "TC", "HK", "KR"}

// cjkFamily returns the Noto CJK family of region for the generic g:
// Noto Serif CJK for serif, Noto Sans Mono CJK for monospace, Noto Sans CJK
// otherwise, as conf.d/70-noto-cjk.conf prepends them.
func cjkFamily(g generic, region string) string {
	if g == genericSerif {
		return "Noto Serif CJK " + region
	}
	if g == genericMonospace {
		return "Noto Sans Mono CJK " + region
	}
	return "Noto Sans CJK " + region
}

// familyList builds a list of family keys without duplicates.
type familyList struct {
	keys []string
}

// add appends the key of name, unless it is already listed.
func (l *familyList) add(key string) {
	if key != "" && !slices.Contains(l.keys, key) {
		l.keys = append(l.keys, key)
	}
}

// addWithAliases appends name and its metric-compatible aliases.
func (l *familyList) addWithAliases(name string) {
	key := familyKey(name)
	l.add(key)
	for _, alias := range metricAliases[key] {
		l.add(alias)
	}
}

// addGeneric appends the preference list of g. For a CJK region, the Noto
// CJK family of the region comes first, as conf.d/70-noto-cjk.conf prepends
// it to the generic family of a pattern in that language.
func (l *familyList) addGeneric(g generic, region string) {
	if region != "" && g != genericNone {
		l.add(familyKey(cjkFamily(g, region)))
	}
	for _, name := range genericFamilies[g] {
		l.addWithAliases(name)
	}
	if g == genericSystemUI {
		l.addGeneric(genericSansSerif, "")
	}
}

// requestedFamilies returns the keys of the families style asks for, in
// preference order: each concrete family followed by its metric-compatible
// aliases, each generic family replaced by its preference list. An empty
// list asks for sans-serif.
func requestedFamilies(style Style) []string {
	var l familyList
	region := cjkRegion(style.Lang)
	if len(style.Families) == 0 {
		l.addGeneric(genericSansSerif, region)
		return l.keys
	}
	for _, name := range style.Families {
		if g := genericOf(name); g != genericNone {
			l.addGeneric(g, region)
			continue
		}
		l.addWithAliases(name)
	}
	return l.keys
}

// matchFamilies returns the families [Fonts.Match] tries for style: the
// requested ones, then sans-serif, which takes an unknown family.
func matchFamilies(style Style) []string {
	l := familyList{keys: requestedFamilies(style)}
	l.addGeneric(genericSansSerif, cjkRegion(style.Lang))
	return l.keys
}

// firstGeneric returns the first generic family style names, sans-serif
// when it names none.
func firstGeneric(style Style) generic {
	for _, name := range style.Families {
		if g := genericOf(name); g != genericNone {
			return g
		}
	}
	return genericSansSerif
}

// scriptFamilies returns the families fontconfig prefers for a character of
// script, as keys: the generic list of g for Latin, Greek and Cyrillic, the
// Noto CJK families for a CJK script with region's first, and the Noto or
// DejaVu family of the script for Arabic, Hebrew, Devanagari and Thai, after
// the orth-based preferences of fontconfig's conf.d/65-nonlatin.conf and
// conf.d/70-noto-cjk.conf. Other scripts have no preference.
func scriptFamilies(script language.Script, g generic, region string) []string {
	var l familyList
	switch script { //nolint:exhaustive // only the scripts the fontconfig confs prefer families for have a list
	case language.Latin, language.Greek, language.Cyrillic:
		l.addGeneric(g, "")
	case language.Han, language.Hiragana, language.Katakana, language.Hangul, language.Bopomofo:
		for _, rg := range append([]string{region}, cjkRegions...) {
			if rg == "" {
				continue
			}
			l.add(familyKey(cjkFamily(g, rg)))
			l.add(familyKey(cjkFamily(genericSansSerif, rg)))
		}
	case language.Arabic:
		for _, name := range []string{"Noto Sans Arabic", "Noto Naskh Arabic", "DejaVu Sans"} {
			l.add(familyKey(name))
		}
	case language.Hebrew:
		for _, name := range []string{"Noto Sans Hebrew", "DejaVu Sans"} {
			l.add(familyKey(name))
		}
	case language.Devanagari:
		l.add(familyKey("Noto Sans Devanagari"))
	case language.Thai:
		l.add(familyKey("Noto Sans Thai"))
	}
	return l.keys
}

// request is the aspect a [Style] asks for.
type request struct {
	weight int
	slant  uint8
}

// requestOf returns the aspect style asks for. A normal width is always
// asked for: [Style] has no width.
func requestOf(style Style) request {
	req := request{weight: int(style.weight())}
	if style.Italic {
		req.slant = slantItalic
	}
	return req
}

// aspectRank orders fc for req: lower is better, compared field by field.
type aspectRank [6]int

// rankFace ranks fc against req in the order of CSS Fonts Level 4, 5.2
// (Matching font styles), which fontconfig shares: width (font-stretch)
// first, then slant (font-style), then weight (font-weight). Bold italic in a
// family of regular, bold and italic faces takes the italic face.
//
// A normal width is asked for, and narrower widths come before wider ones.
// An italic request takes italic, then oblique, then normal; a normal one
// normal, then oblique, then italic. For a weight between 400 and 500,
// weights up to 500 come first in ascending order, then the lighter ones in
// descending order, then the heavier ones. Below 400 lighter weights come
// first, above 500 heavier ones. Index order breaks ties.
func rankFace(req request, fc *Face) aspectRank {
	var r aspectRank
	if fc.width <= widthNormal {
		r[0], r[1] = 0, widthNormal-fc.width
	} else {
		r[0], r[1] = 1, fc.width-widthNormal
	}
	if fc.slant != req.slant {
		r[2] = 2
		if fc.slant == slantOblique {
			r[2] = 1
		}
	}
	w, want := fc.weight, req.weight
	switch {
	case want >= 400 && want <= 500:
		switch {
		case w >= want && w <= 500:
			r[3], r[4] = 0, w-want
		case w < want:
			r[3], r[4] = 1, want-w
		default:
			r[3], r[4] = 2, w-want
		}
	case want < 400:
		if w <= want {
			r[3], r[4] = 0, want-w
		} else {
			r[3], r[4] = 1, w-want
		}
	default:
		if w >= want {
			r[3], r[4] = 0, w-want
		} else {
			r[3], r[4] = 1, want-w
		}
	}
	r[5] = fc.order
	return r
}

// less reports whether a ranks before b.
func (a aspectRank) less(b aspectRank) bool {
	return slices.Compare(a[:], b[:]) < 0
}

// bestInFamily returns the face of family key that covers r, by its
// footprint, and ranks best for req, nil when none does. Ties go to index
// order.
func (f *Fonts) bestInFamily(key string, req request, r rune) *Face {
	var best *Face
	var bestRank aspectRank
	for _, fc := range f.byFamily[key] {
		if !fc.covers(r) {
			continue
		}
		if rank := rankFace(req, fc); best == nil || rank.less(bestRank) {
			best, bestRank = fc, rank
		}
	}
	return best
}
