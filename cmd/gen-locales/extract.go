package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The LibreOffice core files the extract reads, relative to a checkout of
// the pinned commit.
const (
	dataDir    = "i18npool/source/localedata/data"
	isolangCxx = "i18nlangtag/source/isolang/isolang.cxx"
	langH      = "include/i18nlangtag/lang.h"
)

// maxSourceBytes caps one source file. Anti-DoS guard for a build-time
// tool, not a format limit: the largest locale file is under 100 KB.
const maxSourceBytes = 4 << 20

// node is an element of a locale file.
type node struct {
	name  string
	attrs map[string]string
	text  string
	kids  []*node
}

// child returns the first child named name, nil for none.
func (n *node) child(name string) *node {
	if n == nil {
		return nil
	}
	for _, k := range n.kids {
		if k.name == name {
			return k
		}
	}
	return nil
}

// children returns the children named name.
func (n *node) children(name string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for _, k := range n.kids {
		if k.name == name {
			out = append(out, k)
		}
	}
	return out
}

// textOf returns the text of the child named name, "" for none.
func (n *node) textOf(name string) string {
	if c := n.child(name); c != nil {
		return c.text
	}
	return ""
}

// parseXML reads a locale file into a tree.
func parseXML(data []byte) (*node, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var stack []*node
	var root *node
	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			n := &node{name: t.Name.Local, attrs: map[string]string{}}
			for _, a := range t.Attr {
				n.attrs[a.Name.Local] = a.Value
			}
			if len(stack) > 0 {
				p := stack[len(stack)-1]
				p.kids = append(p.kids, n)
			} else {
				root = n
			}
			stack = append(stack, n)
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if len(stack) > 0 {
				stack[len(stack)-1].text += string(t)
			}
		}
	}
	if root == nil {
		return nil, errors.New("no root element")
	}
	return root, nil
}

// sources holds the files of the checkout, by path, and their digest.
type sources struct {
	files map[string][]byte
	names []string
}

// readSources reads the locale files and the two identifier files of the
// checkout at dir, and checks their digest against the pinned one.
func readSources(dir string) (*sources, error) {
	s := &sources{files: map[string][]byte{}}
	entries, err := os.ReadDir(filepath.Join(dir, dataDir))
	if err != nil {
		return nil, err
	}
	paths := []string{isolangCxx, langH}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".xml") {
			paths = append(paths, dataDir+"/"+e.Name())
		}
	}
	slices.Sort(paths)
	h := sha256.New()
	for _, p := range paths {
		f, err := os.Open(filepath.Join(dir, filepath.FromSlash(p)))
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, maxSourceBytes+1))
		f.Close() //nolint:errcheck,gosec // a read-only file has nothing to flush
		if err != nil {
			return nil, err
		}
		if len(data) > maxSourceBytes {
			return nil, fmt.Errorf("%s: larger than %d bytes", p, maxSourceBytes)
		}
		sum := sha256.Sum256(data)
		fmt.Fprintf(h, "%s %x\n", p, sum)
		s.files[p] = data
		if strings.HasPrefix(p, dataDir+"/") {
			s.names = append(s.names, strings.TrimSuffix(filepath.Base(p), ".xml"))
		}
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sourcesSHA256 {
		return nil, fmt.Errorf("sources digest %s, want %s: not the pinned commit", got, sourcesSHA256)
	}
	return s, nil
}

// extract reads every locale of the sources into records.
func extract(s *sources) (*extractFile, error) {
	trees := map[string]*node{}
	for _, name := range s.names {
		root, err := parseXML(s.files[dataDir+"/"+name+".xml"])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		trees[name] = root
	}
	ids, err := lcids(string(s.files[langH]), string(s.files[isolangCxx]))
	if err != nil {
		return nil, err
	}
	out := &extractFile{Commit: commit}
	for _, name := range s.names {
		r, err := record(name, trees, ids)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		out.Locales = append(out.Locales, r)
	}
	return out, nil
}

// section returns the element name of the locale file, following its ref
// attribute to another locale's.
func section(trees map[string]*node, file, name string) (*node, error) {
	for range 8 {
		n := trees[file].child(name)
		if n == nil {
			return nil, fmt.Errorf("no %s", name)
		}
		ref := n.attrs["ref"]
		if ref == "" {
			return n, nil
		}
		file = strings.ReplaceAll(ref, "-", "_")
		if trees[file] == nil {
			return nil, fmt.Errorf("%s refers to unknown locale %s", name, ref)
		}
	}
	return nil, fmt.Errorf("%s: references nested too deep", name)
}

// tagOf returns the BCP 47 tag of a locale's LC_INFO: its Variant when it
// states one, else its language and country.
func tagOf(info *node) string {
	if v := strings.TrimSpace(info.textOf("Variant")); v != "" {
		return v
	}
	lang := strings.TrimSpace(info.child("Language").textOf("LangID"))
	country := strings.TrimSpace(info.child("Country").textOf("CountryID"))
	if country == "" {
		return lang
	}
	return lang + "-" + country
}

// record extracts one locale.
func record(file string, trees map[string]*node, ids map[string][]uint16) (localeRecord, error) {
	root := trees[file]
	info := root.child("LC_INFO")
	r := localeRecord{File: file, Tag: tagOf(info)}
	r.Language = strings.TrimSpace(info.child("Language").textOf("DefaultName"))
	r.Country = strings.TrimSpace(info.child("Country").textOf("DefaultName"))
	r.LCIDs = ids[strings.ToLower(r.Tag)]
	ctype, err := section(trees, file, "LC_CTYPE")
	if err != nil {
		return r, err
	}
	sep := ctype.child("Separators")
	r.Decimal = sep.textOf("DecimalSeparator")
	r.Group = sep.textOf("ThousandSeparator")
	r.DateSep = sep.textOf("DateSeparator")
	r.TimeSep = sep.textOf("TimeSeparator")
	r.Time100Sep = sep.textOf("Time100SecSeparator")
	r.ListSep = sep.textOf("ListSeparator")
	r.DayOfWeekSep = sep.textOf("LongDateDayOfWeekSeparator")
	r.AM = ctype.textOf("TimeAM")
	r.PM = ctype.textOf("TimePM")

	misc, err := section(trees, file, "LC_MISC")
	if err != nil {
		return r, err
	}
	words := misc.child("ReservedWords")
	r.True, r.False = words.textOf("trueWord"), words.textOf("falseWord")

	if err := r.calendar(trees, file); err != nil {
		return r, err
	}
	if err := r.currency(trees, file); err != nil {
		return r, err
	}
	if err := r.formats(trees, file); err != nil {
		return r, err
	}
	return r, nil
}

// calendar reads the names of the Gregorian calendar: the default
// calendar's when it is Gregorian, else the Gregorian one the locale
// carries beside it.
func (r *localeRecord) calendar(trees map[string]*node, file string) error {
	cal, err := section(trees, file, "LC_CALENDAR")
	if err != nil {
		return err
	}
	var greg *node
	for _, c := range cal.children("Calendar") {
		if c.attrs["unoid"] != "gregorian" {
			if c.attrs["default"] == "true" {
				r.Calendar = c.attrs["unoid"]
			}
			continue
		}
		greg = c
	}
	if greg == nil {
		return errors.New("no Gregorian calendar")
	}
	part := func(name string) (*node, error) {
		n := greg.child(name)
		for depth := 0; n != nil && n.attrs["ref"] != ""; depth++ {
			if depth > 8 {
				return nil, fmt.Errorf("%s: references nested too deep", name)
			}
			// A ref names a locale and a calendar, en_US_gregorian, or
			// a locale alone, whose Gregorian calendar it means.
			ref := n.attrs["ref"]
			loc, unoid, ok := ref, "gregorian", true
			if trees[ref] == nil {
				loc, unoid, ok = cutLast(ref, "_")
			}
			if !ok || trees[loc] == nil {
				return nil, fmt.Errorf("%s refers to unknown %s", name, ref)
			}
			c, err := section(trees, loc, "LC_CALENDAR")
			if err != nil {
				return nil, err
			}
			n = nil
			for _, k := range c.children("Calendar") {
				if k.attrs["unoid"] == unoid {
					n = k.child(name)
				}
			}
		}
		return n, nil
	}
	days, err := part("DaysOfWeek")
	if err != nil {
		return err
	}
	if len(days.children("Day")) != 7 {
		return errors.New("not 7 days")
	}
	for i, d := range days.children("Day") {
		r.Days[i] = [2]string{d.textOf("DefaultAbbrvName"), d.textOf("DefaultFullName")}
	}
	months, err := part("MonthsOfYear")
	if err != nil {
		return err
	}
	if len(months.children("Month")) != 12 {
		return errors.New("not 12 months")
	}
	var narrow [12]string
	for i, m := range months.children("Month") {
		r.Months[i] = [2]string{m.textOf("DefaultAbbrvName"), m.textOf("DefaultFullName")}
		narrow[i] = m.textOf("DefaultNarrowName")
	}
	if narrow != ([12]string{}) {
		r.Narrow = &narrow
	}
	for _, c := range []struct {
		name string
		dst  **[12][2]string
	}{{"GenitiveMonths", &r.Genitive}, {"PartitiveMonths", &r.Partitive}} {
		n, err := part(c.name)
		if err != nil {
			return err
		}
		if n == nil {
			continue
		}
		if len(n.children("Month")) != 12 {
			return fmt.Errorf("not 12 months in %s", c.name)
		}
		var g [12][2]string
		for i, m := range n.children("Month") {
			g[i] = [2]string{m.textOf("DefaultAbbrvName"), m.textOf("DefaultFullName")}
		}
		*c.dst = &g
	}
	// Partitive names fall back on the genitive ones, those on the
	// nominative ones (i18npool calendar_gregorian.cxx): a list equal to
	// its fallback is left out.
	if r.Genitive != nil && *r.Genitive == r.Months {
		r.Genitive = nil
	}
	if r.Partitive != nil {
		fallback := r.Months
		if r.Genitive != nil {
			fallback = *r.Genitive
		}
		if *r.Partitive == fallback {
			r.Partitive = nil
		}
	}
	return nil
}

// cutLast cuts s around the last sep.
func cutLast(s, sep string) (string, string, bool) {
	i := strings.LastIndex(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+len(sep):], true
}

// currency reads the default currency's symbol.
func (r *localeRecord) currency(trees map[string]*node, file string) error {
	cur, err := section(trees, file, "LC_CURRENCY")
	if err != nil {
		return err
	}
	for _, c := range cur.children("Currency") {
		if c.attrs["default"] == "true" {
			r.Currency = c.textOf("CurrencySymbol")
			d, err := strconv.Atoi(strings.TrimSpace(c.textOf("DecimalPlaces")))
			if err != nil || d < 0 || d > 9 {
				return fmt.Errorf("currency DecimalPlaces %q", c.textOf("DecimalPlaces"))
			}
			r.CurrencyDigits = d
			return nil
		}
	}
	return errors.New("no default currency")
}

// wantedIndices are the format indices of LibreOffice's built-in formats
// the table keeps (i18npool's NumberFormatIndex): the scientific and
// percent formats a CSV value takes, the currency formats, and the date,
// time and date-time formats.
var wantedIndices = []int{7, 8, 9, 12, 13, 14, 15, 18, 19, 20, 21, 33, 39, 40, 41, 42, 43, 44, 45, 46, 47, 50}

// formats reads the format codes of LC_FORMAT, translated into the
// SpreadsheetML syntax, the date acceptance patterns, the default format
// of each usage and the digit grouping.
func (r *localeRecord) formats(trees map[string]*node, file string) error {
	own := trees[file].child("LC_FORMAT")
	if own == nil {
		return errors.New("no LC_FORMAT")
	}
	replaceTo := own.attrs["replaceTo"]
	f, err := section(trees, file, "LC_FORMAT")
	if err != nil {
		return err
	}
	replaceFrom := f.attrs["replaceFrom"]
	for _, p := range f.children("DateAcceptancePattern") {
		r.Acceptance = append(r.Acceptance, p.text)
	}
	kw := keywordsFor(r.Tag)
	type elem struct {
		index         int
		usage, typ    string
		def           bool
		code          string
		translatedErr error
	}
	var elems []elem
	general := ""
	for _, e := range f.children("FormatElement") {
		idx, err := strconv.Atoi(e.attrs["formatindex"])
		if err != nil {
			return fmt.Errorf("formatindex %q", e.attrs["formatindex"])
		}
		code := e.textOf("FormatCode")
		if replaceFrom != "" {
			code = strings.ReplaceAll(code, replaceFrom, replaceTo)
		}
		if idx == 0 {
			general = code
		}
		elems = append(elems, elem{index: idx, usage: e.attrs["usage"], typ: e.attrs["type"], def: e.attrs["default"] == "true", code: code})
	}
	tr := translator{kw: kw, general: general, decimal: r.Decimal, group: r.Group, dowSep: r.DayOfWeekSep}
	r.Codes = map[string]string{}
	for _, e := range elems {
		if !slices.Contains(wantedIndices, e.index) {
			continue
		}
		code, err := tr.translate(e.code)
		if err != nil {
			r.Unsupported = append(r.Unsupported, fmt.Sprintf("%d %s", e.index, e.code))
			continue
		}
		r.Codes[strconv.Itoa(e.index)] = code
	}
	r.Grouping = []int{3}
	if _, country, _ := strings.Cut(r.Tag, "-"); country == "IN" || country == "BT" {
		// LibreOffice groups by three then two in India and Bhutan, by
		// three elsewhere, whatever the format codes say
		// (unotools/source/i18n/localedatawrapper.cxx, loadDigitGrouping).
		r.Grouping = []int{3, 2}
	}
	r.Defaults = map[string]int{}
	for _, usage := range []string{"DATE", "TIME", "DATE_TIME", "CURRENCY"} {
		best, rank := -1, 0
		for _, e := range elems {
			if e.usage != usage || !e.def {
				continue
			}
			// The medium default wins, then the long one, then the short
			// one (svl zforlist.cxx, ImpAdjustFormatCodeDefault).
			rk := map[string]int{"medium": 3, "long": 2, "short": 1}[e.typ]
			if rk > rank {
				best, rank = e.index, rk
			}
		}
		if best < 0 {
			continue
		}
		r.Defaults[usage] = best
		for _, e := range elems {
			if e.index != best {
				continue
			}
			code, err := tr.translate(e.code)
			if err != nil {
				r.Unsupported = append(r.Unsupported, fmt.Sprintf("%s %s", usage, e.code))
				break
			}
			r.Codes[usage] = code
		}
	}
	return nil
}

// definePattern reads "#define LANGUAGE_X LanguageType(0x0407)".
var definePattern = regexp.MustCompile(`(?m)^#define\s+(LANGUAGE_\w+)\s+LanguageType\(\s*(0x[0-9A-Fa-f]+)\s*\)`)

// entryPattern reads a row of isolang.cxx's tables: an identifier, a
// language (with a script) and a country: { LANGUAGE_GERMAN, "de", "DE", k0 }
// or { LANGUAGE_CATALAN_VALENCIAN, "ca-ES-valencia", "ES", "ca-valencia", k0 }.
var entryPattern = regexp.MustCompile(`\{\s*(LANGUAGE_\w+)\s*,\s*"([^"]*)"\s*,\s*"([^"]*)"`)

// lcids returns the Windows locale identifiers of each lower-case tag,
// in the order isolang.cxx lists them (MS-LCID), from the defines of
// lang.h and the tables of isolang.cxx. LibreOffice's own identifiers,
// from 0x0610 and in the user range, are kept: LibreOffice writes them in
// the tags of the format codes it saves.
func lcids(langH, isolang string) (map[string][]uint16, error) {
	values := map[string]uint16{}
	for _, m := range definePattern.FindAllStringSubmatch(langH, -1) {
		v, err := strconv.ParseUint(m[2], 0, 16)
		if err != nil {
			return nil, err
		}
		values[m[1]] = uint16(v)
	}
	out := map[string][]uint16{}
	for _, m := range entryPattern.FindAllStringSubmatch(isolang, -1) {
		v, ok := values[m[1]]
		if !ok || v == 0 || v >= 0xFFF0 {
			continue
		}
		lang, country := m[2], m[3]
		tag := lang
		switch {
		case strings.Count(lang, "-") >= 2:
			// A full BCP 47 tag of aImplBcp47CountryEntries.
		case country != "":
			tag = lang + "-" + country
		}
		// A Unicode extension (es-ES-u-co-trad, the traditional sort) does
		// not change the locale's data.
		tag, _, _ = strings.Cut(strings.ToLower(tag), "-u-")
		if !slices.Contains(out[tag], v) {
			out[tag] = append(out[tag], v)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("isolang.cxx: no entries read")
	}
	return out, nil
}
