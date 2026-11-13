package text

import (
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"

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
// without spaces or tabs, so "DejaVu Sans" and "dejavusans" match. The
// double quotes that mark a family name spelled like a generic family (see
// [Style.Families]) do not count.
func familyKey(name string) string {
	name = strings.TrimSpace(name)
	if len(name) >= 2 && name[0] == '"' && name[len(name)-1] == '"' {
		name = name[1 : len(name)-1]
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, strings.ToLower(strings.TrimSpace(name)))
}

// isASCII reports whether s holds ASCII bytes alone.
func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
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

// genericNames are the names of the generic families, lower case.
var genericNames = [...]struct {
	name string
	g    generic
}{
	{"serif", genericSerif},
	{"sans-serif", genericSansSerif},
	{"monospace", genericMonospace},
	{"cursive", genericCursive},
	{"fantasy", genericFantasy},
	{"system-ui", genericSystemUI},
}

// genericOf returns the generic family name stands for, genericNone for a
// concrete family. Case does not count; a quoted name is concrete.
func genericOf(name string) generic {
	name = strings.TrimSpace(name)
	if isASCII(name) {
		// ASCII folds as it lower-cases, and comparing so copies nothing.
		for _, n := range genericNames {
			if strings.EqualFold(name, n.name) {
				return n.g
			}
		}
		return genericNone
	}
	lower := strings.ToLower(name)
	for _, n := range genericNames {
		if lower == n.name {
			return n.g
		}
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

// browserDefaults are the families Chromium asks fontconfig for in place of
// a generic family, the defaults of its Linux font settings
// (chrome/app/resources/locale_settings_linux.grd): Times New Roman for
// serif, Arial for sans-serif, Comic Sans MS for cursive and Impact for
// fantasy. Chromium does not ask fontconfig for the generic itself, so
// fontconfig's metric aliases turn these into Liberation Serif and
// Liberation Sans where those are installed, ahead of the DejaVu faces
// 60-latin.conf prefers. Monospace and system-ui have none: Chromium asks
// for "Monospace" and the toolkit's "Sans", which fontconfig resolves by its
// own lists.
var browserDefaults = map[generic]string{
	genericSerif:     "Times New Roman",
	genericSansSerif: "Arial",
	genericCursive:   "Comic Sans MS",
	genericFantasy:   "Impact",
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

// add appends key, unless it is empty or already listed.
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
// list asks for sans-serif. The list is shared: the caller reads it and
// never writes it.
func requestedFamilies(style Style) []string {
	return resolvedFamilies(style).requested
}

// buildRequested returns what [requestedFamilies] returns for families in
// the CJK region region.
func buildRequested(families []string, region string) []string {
	var l familyList
	if len(families) == 0 {
		l.addGeneric(genericSansSerif, region)
		return l.keys
	}
	for _, name := range families {
		if g := genericOf(name); g != genericNone {
			l.addGeneric(g, region)
			continue
		}
		l.addWithAliases(name)
	}
	return l.keys
}

// familyLists are the family keys a list of families resolves to in one
// language: the ones it asks for ([requestedFamilies]) and the ones
// [Fonts.Match] tries ([matchFamilies]). Both are read and never written,
// their capacity their length, so an append copies them.
type familyLists struct {
	requested, match []string
}

// maxFamilyCache caps the family lists [resolvedFamilies] keeps. Memory
// guard, not spec-mandated: a document names a few dozen font lists in a
// few languages. A table that is full is emptied before the next list
// goes in, so the lists of the documents being converted are the ones
// kept, whatever an earlier document filled it with.
const maxFamilyCache = 4096

// maxFamilyKeyBytes caps the key, the language and the family names, of a
// list [resolvedFamilies] keeps: 512 bytes. Anti-DoS guard, not
// spec-mandated: the table outlives the conversion that fills it, so a
// document must not park kilobytes of names in it. The font list of a
// stylesheet is a few dozen bytes; a longer one is resolved at each call,
// to the same keys.
const maxFamilyKeyBytes = 512

// familyCache holds the family lists of each list of families and
// language resolved, for every conversion of the process: a list resolves
// alike whatever the index, so a paragraph does not expand the generic
// families' preference lists again.
var familyCache = struct {
	mu sync.RWMutex
	m  map[string]familyLists
}{m: map[string]familyLists{}}

// resolvedFamilies returns the family lists of style's families and
// language.
func resolvedFamilies(style Style) familyLists {
	// The key is built on the stack and looked up without a copy: a
	// paragraph whose list was resolved before allocates nothing here.
	// Each name is written after its length, so that no two lists, nor a
	// list and its language, write one key.
	var buf [256]byte
	kb := strconv.AppendInt(buf[:0], int64(len(style.Lang)), 10)
	kb = append(kb, ':')
	kb = append(kb, style.Lang...)
	for _, f := range style.Families {
		kb = strconv.AppendInt(kb, int64(len(f)), 10)
		kb = append(kb, ':')
		kb = append(kb, f...)
	}
	kept := len(kb) <= maxFamilyKeyBytes
	if kept {
		familyCache.mu.RLock()
		l, ok := familyCache.m[string(kb)]
		familyCache.mu.RUnlock()
		if ok {
			return l
		}
	}
	region := cjkRegion(style.Lang)
	requested := slices.Clip(buildRequested(style.Families, region))
	m := familyList{keys: requested}
	m.addGeneric(genericSansSerif, region)
	l := familyLists{requested: requested, match: slices.Clip(m.keys)}
	if kept {
		familyCache.mu.Lock()
		if len(familyCache.m) >= maxFamilyCache {
			familyCache.m = make(map[string]familyLists, maxFamilyCache/4)
		}
		familyCache.m[string(kb)] = l
		familyCache.mu.Unlock()
	}
	return l
}

// BrowserFamilies returns families as Chromium resolves them, a new list
// for [Style.Families]; families itself is not modified.
//
// Chromium does not ask fontconfig for a generic family: it asks for the
// family its settings name in its place ([browserDefaults]), Times New
// Roman for serif, Arial for sans-serif, Comic Sans MS for cursive and
// Impact for fantasy, which fontconfig's metric aliases turn into Liberation
// Serif and Liberation Sans where those are installed, ahead of the DejaVu
// faces fontconfig's own lists prefer. BrowserFamilies puts that family
// right before each generic it stands for, so the generic's list stays the
// fallback. Monospace and system-ui keep fontconfig's lists, as Chromium
// asks for "Monospace" and the toolkit's "Sans".
//
// A list that names no generic family [Style.Families] resolves, nil and
// empty lists included, ends with serif. CSS Fonts 4, 5 (Font Matching
// Algorithm), has the user agent fall back to its default font when no
// family of the list is available, and Chromium's default is its standard
// font, Times New Roman, the family it asks for in place of serif. So
// Georgia alone draws with Liberation Serif, not with the sans-serif face
// [Fonts.Match] falls back to.
func BrowserFamilies(families []string) []string {
	out := make([]string, 0, len(families)+2)
	hasGeneric := false
	for _, name := range families {
		if g := genericOf(name); g != genericNone {
			hasGeneric = true
			if d := browserDefaults[g]; d != "" {
				out = append(out, d)
			}
		}
		out = append(out, name)
	}
	if !hasGeneric {
		out = append(out, browserDefaults[genericSerif], "serif")
	}
	return out
}

// Fallback names the program whose font fallback [Fonts.Layout] follows
// for a character the face [Fonts.Match] picks lacks. The zero value
// follows fontconfig's resolution of the style's families; a converter sets
// the rule of the program whose output its format is judged against.
type Fallback uint8

const (
	// FallbackFontconfig tries the families the style asks for with their
	// generic lists expanded, then the preferences of the character's
	// script under the style's first generic family.
	FallbackFontconfig Fallback = iota
	// FallbackChromium follows Chromium on Linux. Each family of the style
	// stands for one family: a concrete family for itself and its metric
	// aliases, a generic family for the family [BrowserFamilies] puts
	// before it, or, for monospace and system-ui, the first family of the
	// generic's list the index holds. A character none of them covers then
	// takes the face fontconfig ranks first for a pattern that names no
	// family: the sans-serif preferences, the Noto CJK family of the
	// language, Japanese without one.
	FallbackChromium
	// FallbackLibreOffice follows LibreOffice. The concrete families of
	// the style come first, each with its metric aliases. A character none
	// of them covers then takes the face fontconfig ranks first for a
	// pattern that names the style's first family: the preferences of that
	// family's generic family (fontconfig's conf.d/45-latin.conf, sans-serif
	// for a family it does not name), the Noto CJK family of the language,
	// Simplified Chinese without one. A character of a CJK script takes the
	// preferences of the generic family of the style's first family alone,
	// sans-serif for one fontconfig gives none. [LibreOfficeFallback]
	// reads the generic family from the list as the document names it;
	// FallbackLibreOffice alone reads it from [Style.Families], which
	// [BrowserFamilies] may have given a serif family the document did not
	// name.
	FallbackLibreOffice
)

// fallbackRuleBits are the low bits of a [Fallback] that name its rule;
// the fallbackGenericBits above them hold the generic family
// [LibreOfficeFallback] read for the run, and the next ones that for a
// character of a CJK script, 0 for none.
const (
	fallbackRuleBits    = 2
	fallbackGenericBits = 3
)

// rule returns the rule fb names.
func (fb Fallback) rule() Fallback {
	return fb & (1<<fallbackRuleBits - 1)
}

// generic returns the generic family fb carries, genericNone for none.
func (fb Fallback) generic() generic {
	return generic(fb>>fallbackRuleBits) & (1<<fallbackGenericBits - 1)
}

// cjkGeneric returns the generic family fb carries for a character of a
// CJK script, genericNone for none.
func (fb Fallback) cjkGeneric() generic {
	return generic(fb >> (fallbackRuleBits + fallbackGenericBits))
}

// LibreOfficeFallback returns [FallbackLibreOffice] for a run whose
// document names the families named, before [BrowserFamilies] rewrites
// them, its script's own family first: it carries the generic family
// fontconfig ranks the run's fallback faces by, that of the first entry of
// named that is a generic family or a family fontconfig gives one,
// sans-serif when none is; and for a character of a CJK script that of
// the first family alone, sans-serif when fontconfig gives it none, since
// LibreOffice asks fontconfig for the Asian family by itself.
//
// # Corpus evidence
//
//   - probe, odt: in the gotenberg/gotenberg:9.4.1 image, LibreOffice
//     prints Han under an Asian family it lacks in Noto Sans CJK SC
//     whatever the western family, and under Liberation Serif or DejaVu
//     Serif named as the Asian family in Noto Serif CJK SC.
func LibreOfficeFallback(named []string) Fallback {
	_, g := libreOfficeFamilies(named)
	cjk := genericSansSerif
	if len(named) > 0 {
		if first := genericOf(named[0]); first != genericNone {
			cjk = first
		} else if first := familyGeneric(familyKey(named[0])); first != genericNone {
			cjk = first
		}
	}
	return FallbackLibreOffice | Fallback(g)<<fallbackRuleBits | Fallback(cjk)<<(fallbackRuleBits+fallbackGenericBits)
}

// fontconfigGenerics are the generic families fontconfig gives the families
// it knows, by family key: the defaults of conf.d/45-latin.conf and of the
// URW base 35 files conf.d/61-urw-*.conf. fontconfig appends sans-serif to
// a pattern that names none of them (conf.d/49-sansserif.conf).
var fontconfigGenerics = func() map[string]generic {
	m := make(map[string]generic)
	for g, names := range map[generic][]string{
		genericSerif: {
			"Bitstream Vera Serif", "Cambria", "Constantia", "DejaVu Serif", "Elephant", "Garamond",
			"Georgia", "Liberation Serif", "Luxi Serif", "MS Serif", "Nimbus Roman No9 L", "Nimbus Roman",
			"Palatino Linotype", "Thorndale AMT", "Thorndale", "Times New Roman", "Times",
			"C059", "P052", "Standard Symbols PS", "URW Bookman",
		},
		genericSansSerif: {
			"Albany AMT", "Albany", "Arial Unicode MS", "Arial", "Bitstream Vera Sans", "Britannic",
			"Calibri", "Candara", "Century Gothic", "Corbel", "DejaVu Sans", "Helvetica LT Std",
			"Helvetica", "Haettenschweiler", "Liberation Sans", "MS Sans Serif", "Nimbus Sans L",
			"Nimbus Sans", "Luxi Sans", "Tahoma", "Trebuchet MS", "Twentieth Century", "Verdana",
			"Nimbus Sans Narrow", "URW Gothic",
		},
		genericMonospace: {
			"Andale Mono", "Bitstream Vera Sans Mono", "Consolas", "Courier New", "Courier Std",
			"Courier", "Cumberland AMT", "Cumberland", "DejaVu Sans Mono", "Fixedsys", "Inconsolata",
			"Liberation Mono", "Luxi Mono", "Nimbus Mono L", "Nimbus Mono", "Nimbus Mono PS", "Terminal",
		},
		genericFantasy:  {"Bauhaus Std", "Cooper Std", "Copperplate Gothic Std", "Impact"},
		genericCursive:  {"Comic Sans MS", "ITC Zapf Chancery Std", "Zapfino", "Z003"},
		genericSystemUI: {"Cantarell", "Noto Sans UI", "Segoe UI", "Segoe UI Historic", "Segoe UI Symbol"},
	} {
		for _, name := range names {
			m[familyKey(name)] = g
		}
	}
	return m
}()

// familyGeneric returns the generic family fontconfig gives the family of
// key: its own, else that of its first metric-compatible alias fontconfig
// names, as conf.d/30-metric-aliases.conf adds the aliases to the pattern
// before 45-latin.conf runs; genericNone for a family neither names.
func familyGeneric(key string) generic {
	if g, ok := fontconfigGenerics[key]; ok {
		return g
	}
	for _, alias := range metricAliases[key] {
		if g, ok := fontconfigGenerics[alias]; ok {
			return g
		}
	}
	return genericNone
}

// chromiumFamilies returns the keys of the families [FallbackChromium]
// tries for a character the matched face lacks, in order: each concrete
// family of families with its metric aliases, the families
// [BrowserFamilies] inserts included, and for a generic family without a
// browser default the first family of its list f holds.
func (f *Fonts) chromiumFamilies(families []string) []string {
	var l familyList
	for _, name := range families {
		g := genericOf(name)
		switch {
		case g == genericNone:
			l.addWithAliases(name)
		case browserDefaults[g] == "":
			var gl familyList
			gl.addGeneric(g, "")
			for _, key := range gl.keys {
				if len(f.byFamily[key]) > 0 {
					l.add(key)
					break
				}
			}
		}
	}
	return l.keys
}

// withoutBrowserDefaults returns families, a list [BrowserFamilies]
// rewrote, as close as it tells to the list it was given: without the
// families it puts before a generic family, and without the serif family
// it appends after the last of them. It returns families itself when
// [BrowserFamilies] inserted nothing, a new list otherwise.
func withoutBrowserDefaults(families []string) []string {
	var out []string
	for i, name := range families {
		switch {
		case isBrowserDefault(families, i):
		case i == len(families)-1 && i > 0 && genericOf(name) == genericSerif && isBrowserDefault(families, i-1) && !slices.ContainsFunc(families[:i-1], func(n string) bool { return genericOf(n) != genericNone }):
		default:
			if out != nil {
				out = append(out, name)
			}
			continue
		}
		if out == nil {
			out = append(make([]string, 0, len(families)), families[:i]...)
		}
	}
	if out == nil {
		return families
	}
	return out
}

// isBrowserDefault reports whether families[i] is the family
// [BrowserFamilies] puts before the generic family that follows it.
func isBrowserDefault(families []string, i int) bool {
	if i+1 >= len(families) {
		return false
	}
	d := browserDefaults[genericOf(families[i+1])]
	return d != "" && familyKey(d) == familyKey(families[i])
}

// libreOfficeFamilies returns the keys of the families
// [FallbackLibreOffice] tries for a character the matched face lacks, the
// concrete families of named with their metric aliases, and the generic
// family fontconfig ranks the rest of the faces by: that of the first entry
// of named that is a generic family or a family fontconfig gives one
// ([familyGeneric]), sans-serif when none is.
func libreOfficeFamilies(named []string) ([]string, generic) {
	var l familyList
	g := genericNone
	for _, name := range named {
		ng := genericOf(name)
		if ng == genericNone {
			l.addWithAliases(name)
			ng = familyGeneric(familyKey(name))
		}
		if g == genericNone {
			g = ng
		}
	}
	if g == genericNone {
		g = genericSansSerif
	}
	return l.keys, g
}

// matchFamilies returns the families [Fonts.Match] tries for style: the
// requested ones, then sans-serif, which takes an unknown family.
func matchFamilies(style Style) []string {
	return resolvedFamilies(style).match
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
// Noto CJK families for a CJK script with region's first, DejaVu Sans then
// the Noto families of the script for Arabic and Hebrew, and the Noto family
// of the script for Devanagari and Thai, after the orth-based preferences of
// fontconfig's conf.d/65-nonlatin.conf and conf.d/70-noto-cjk.conf. Other
// scripts have no preference.
//
// DejaVu Sans comes first for Arabic and Hebrew because fontconfig sorts the
// faces that cover a character by its family lists, and 60-latin.conf names
// DejaVu Sans in the sans-serif and serif lists where no list names a Noto
// Arabic or Hebrew family.
//
// # Corpus evidence
//
//   - probe, html: in the gotenberg/gotenberg:9.4.1 image, Chromium prints
//     a paragraph of Arabic that names no font in DejaVu Sans, although Noto
//     Sans Arabic, Noto Naskh Arabic and Noto Kufi Arabic are installed.
//   - probe, odt: LibreOffice in the same image prints the paragraph in
//     DejaVu Sans, and a paragraph of Hebrew too; fc-match -s there ranks
//     DejaVu Sans first for U+0627 and U+05D0 under sans-serif and serif.
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
		for _, name := range []string{"DejaVu Sans", "Noto Sans Arabic", "Noto Naskh Arabic"} {
			l.add(familyKey(name))
		}
	case language.Hebrew:
		for _, name := range []string{"DejaVu Sans", "Noto Sans Hebrew"} {
			l.add(familyKey(name))
		}
	case language.Devanagari:
		l.add(familyKey("Noto Sans Devanagari"))
	case language.Thai:
		l.add(familyKey("Noto Sans Thai"))
	}
	return l.keys
}

// fallbackFamilies are the keys of the sans-serif list, aliases expanded,
// which a character no requested or script family covers tries before any
// other face. fontconfig adds sans-serif to a pattern that names no generic
// family (conf.d/49-sansserif.conf), and Chromium asks fontconfig for a
// character's fallback with no family at all, so the sans-serif list ranks
// the faces that cover the character where no other list names one.
//
// # Corpus evidence
//
//   - probe, html: in the gotenberg/gotenberg:9.4.1 image, Chromium prints
//     U+2605, U+2606, U+2713 and U+2717 of a paragraph that names no font
//     in DejaVu Sans, which neither Liberation Serif nor DejaVu Serif covers,
//     where litho drew them in Noto Sans CJK JP, the first face in index
//     order to cover them.
//   - probe, odt: LibreOffice in the same image prints them in DejaVu Sans.
var fallbackFamilies = func() []string {
	var l familyList
	l.addGeneric(genericSansSerif, "")
	return l.keys
}()

// request is the aspect a [Style] asks for.
type request struct {
	weight int
	slant  uint8
	// stretch is the width in percent; oblique the angle of an oblique
	// request, 0 for italic.
	stretch float64
	oblique float64
	// noBold and noItalic forbid synthesis.
	noBold, noItalic bool
	// rule is the fallback rule of the style, which ranks the instances of
	// a variable font (see [instanceWeightRank]).
	rule Fallback
}

// requestOf returns the aspect style asks for.
func requestOf(style Style) request {
	req := request{weight: int(style.weight()), stretch: style.stretch(), noBold: style.NoSyntheticBold, noItalic: style.NoSyntheticItalic, rule: style.Fallback.rule()}
	if style.Italic {
		req.slant = slantItalic
		req.oblique = style.Oblique
	}
	return req
}

// width returns the width r asks for in percent, 100 when unset.
func (r request) width() float64 {
	if r.stretch <= 0 {
		return 100
	}
	return r.stretch
}

// widthPercents maps an OS/2 usWidthClass, 1 to 9, to its font-stretch
// percentage (CSS Fonts 4, 2.3, Table 2).
var widthPercents = [10]float64{100, 50, 62.5, 75, 87.5, 100, 112.5, 125, 150, 200}

// widthPercent returns the font-stretch percentage of an OS/2 width class.
func widthPercent(class int) float64 {
	if class < 1 || class >= len(widthPercents) {
		return 100
	}
	return widthPercents[class]
}

// stretchOrder ranks a face of width have for a request of width want
// (CSS Fonts 4, 5.2, step 4a): the width itself, then, for a width up to
// 100%, the narrower ones, nearest first, then the wider ones; above
// 100%, the wider ones first.
func stretchOrder(want, have float64) (int, float64) {
	switch {
	case have == want:
		return 0, 0
	case want <= 100 && have < want, want > 100 && have > want:
		return 1, math.Abs(have - want)
	}
	return 2, math.Abs(have - want)
}

// skewOf returns the synthetic shear of a slanted request. CSS Fonts 4,
// 2.4 has a synthetic oblique face generated where no oblique face exists,
// slanted clockwise for a positive angle and counter-clockwise for a
// negative one, and leaves the slant to the user agent. Chromium shears
// italic, and oblique by an angle of at least [italicThreshold] degrees,
// by [syntheticItalicSkew] whatever the angle, and draws a smaller or a
// negative angle upright; litho follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium in gotenberg/gotenberg:9.4.1 draws an
//     @font-face face with no oblique (Ahem) sheared by 0.25 for oblique
//     14, 15, 20, 30, 45, 60, 80, 89 and 90 degrees and for italic, and
//     upright for oblique 0.5 to 13.9 degrees and for every negative
//     angle tried (-1 to -90 degrees); again for 10, -10, 20 and -20
//     degrees in a screenshot: only 20 degrees is sheared.
func skewOf(req request) float64 {
	if req.oblique == 0 || req.oblique >= italicThreshold {
		return syntheticItalicSkew
	}
	return 0
}

// italicThreshold is the oblique angle, in degrees, from which Chromium
// counts a style as italic (FontSelectionValue's italic threshold) and
// synthesises a slant.
const italicThreshold = 14

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
// first, above 500 heavier ones. The faces of a variable font, its named
// instances, rank by weight as [instanceWeightRank] says. Index order breaks
// ties.
//
// Under [FallbackChromium] a static face ranks by weight as fontconfig
// ranks it for Chromium, nearest in fontconfig's weight scale
// ([fontconfigDistance]), which departs from CSS Fonts 4, 5.2, step 4c:
// Chromium draws font-weight 510 to 551 with a family's 500 face where
// the clause takes its 600 one, on screen and in print alike. A tie goes
// to the face first in index order, as fontconfig's goes to the first
// face of its set.
func rankFace(req request, fc *Face) aspectRank {
	var r aspectRank
	k, d := stretchOrder(req.width(), widthPercent(fc.width))
	r[0], r[1] = k, int(d*8)
	if fc.slant != req.slant {
		r[2] = 2
		if fc.slant == slantOblique {
			r[2] = 1
		}
	}
	w, want := fc.weight, req.weight
	switch {
	case fc.axes != nil:
		r[3], r[4] = instanceWeightRank(want, w, req.rule)
	case req.rule == FallbackChromium:
		r[3] = fontconfigDistance(want, w)
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

// The synthetic bold of Skia, which Chromium draws with: the outline is
// stroked with a line of fakeBoldSmallRatio of the size up to
// fakeBoldSmallSize points, fakeBoldLargeRatio from fakeBoldLargeSize
// points, interpolated linearly between (SkScalerContext's
// kStdFakeBoldInterpKeys and kStdFakeBoldInterpValues).
const (
	fakeBoldSmallSize  = 9.0
	fakeBoldLargeSize  = 36.0
	fakeBoldSmallRatio = 1.0 / 24
	fakeBoldLargeRatio = 1.0 / 32
)

// syntheticBoldDelta is how much heavier than the face a requested weight
// must be for Chromium to embolden it: a 700 request on a 400 face, not a
// 600 one.
const syntheticBoldDelta = 200

// syntheticItalicSkew is the shear Chromium slants a face without italic
// by, about 14 degrees.
const syntheticItalicSkew = 0.25

// synthesis returns the stroke width and skew that synthesise the bold and
// italic req asks for and fc lacks, at size points, as Chromium does (CSS
// Fonts 4, 3.5, font-synthesis: weight and style allowed by default): bold
// when the requested weight exceeds the face's by more than
// [syntheticBoldDelta], italic when an italic or oblique face is asked for
// and fc is upright.
func synthesis(req request, fc *Face, size float64) (stroke, skew float64) {
	if fc == nil {
		return 0, 0
	}
	if fc.doc != nil {
		return fc.doc.synthesis(req, size)
	}
	if req.weight > fc.weight+syntheticBoldDelta && !req.noBold {
		stroke = fakeBoldStroke(size)
	}
	if req.slant != slantNormal && fc.slant == slantNormal && !req.noItalic {
		skew = skewOf(req)
	}
	return stroke, skew
}

// fakeBoldStroke returns the width of the line Skia strokes a synthetic
// bold outline with at size points.
func fakeBoldStroke(size float64) float64 {
	ratio := fakeBoldSmallRatio
	switch {
	case size >= fakeBoldLargeSize:
		ratio = fakeBoldLargeRatio
	case size > fakeBoldSmallSize:
		t := (size - fakeBoldSmallSize) / (fakeBoldLargeSize - fakeBoldSmallSize)
		ratio = fakeBoldSmallRatio + float64(t*(fakeBoldLargeRatio-fakeBoldSmallRatio))
	}
	return size * ratio
}
