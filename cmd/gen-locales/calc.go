package main

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// The LibreOffice core files that state how Calc's import maps the
// built-in number formats of a workbook, relative to a checkout.
const (
	numberFormatsCxx   = "sc/source/filter/oox/numberformatsbuffer.cxx"
	numberFormatIdxIDL = "offapi/com/sun/star/i18n/NumberFormatIndex.idl"
)

// calcTable is one table of Calc's built-in formats (numberformatsbuffer.cxx,
// spBuiltinFormatTables): the formats a system locale gives the built-in
// ids, over those of its parent table.
type calcTable struct {
	// Locale is the system locale the table is for, a BCP 47 tag, or "*"
	// and "*CJK" for the two tables the others derive from.
	Locale string `json:"locale"`
	// Parent is the table this one derives from, "" for the base table.
	Parent string `json:"parent"`
	// Formats are the table's entries, in the order the source lists them.
	Formats []calcFormat `json:"formats"`
}

// calcFormat is one entry of a table (BuiltinFormat): exactly one of Code,
// Predef and Reuse is set.
type calcFormat struct {
	// ID is the built-in format id (ECMA-376 Part 1, 18.8.30).
	ID int `json:"id"`
	// Code is a format code in Calc's en-US syntax (NUMFMT_STRING).
	Code string `json:"code,omitempty"`
	// Predef is the index of one of the locale's own formats, an
	// i18n NumberFormatIndex (NUMFMT_PREDEF).
	Predef *int `json:"predef,omitempty"`
	// Reuse is the id whose format this id takes (NUMFMT_REUSE).
	Reuse *int `json:"reuse,omitempty"`
}

// maxCalcID bounds a built-in id of the tables: Calc's base table runs to
// 81, ids 82 to 163 "must not occur in a file".
const maxCalcID = 81

// ctok is a token of the C++ source: an identifier, a number, a string
// literal (its value, quotes and prefix gone), or one punctuation
// character.
type ctok struct {
	kind byte // 'i' identifier, 'n' number, 's' string, 'p' punctuation
	text string
}

// cTokens reads the tokens of C++ source, comments and preprocessor lines
// left out. A string literal's encoding prefix (u, u8, L) and its
// user-defined suffix (_ustr) are dropped.
func cTokens(src string) ([]ctok, error) {
	var out []ctok
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				return out, nil
			}
			i += j
		case strings.HasPrefix(src[i:], "/*"):
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				return nil, errors.New("unclosed comment")
			}
			i += j + 4
		case c == '"' || c == '\'':
			s, n, err := cString(src[i:])
			if err != nil {
				return nil, err
			}
			i += n
			for i < len(src) && isIdent(src[i]) {
				i++
			}
			if c == '"' {
				out = append(out, ctok{'s', s})
			} else {
				out = append(out, ctok{'n', s})
			}
		case isIdent(c) && (c < '0' || c > '9'):
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			if j < len(src) && src[j] == '"' && (src[i:j] == "u" || src[i:j] == "u8" || src[i:j] == "L" || src[i:j] == "U") {
				i = j
				continue
			}
			out = append(out, ctok{'i', src[i:j]})
			i = j
		case c >= '0' && c <= '9':
			j := i
			for j < len(src) && isIdent(src[j]) {
				j++
			}
			out = append(out, ctok{'n', src[i:j]})
			i = j
		case strings.HasPrefix(src[i:], "::"):
			out = append(out, ctok{'p', "::"})
			i += 2
		default:
			out = append(out, ctok{'p', string(c)})
			i++
		}
	}
	return out, nil
}

// isIdent reports a character of a C identifier or number.
func isIdent(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// cString reads the string or character literal at the start of s and
// returns its value and length: the escapes \" \' \\ \? \n and \t.
func cString(s string) (string, int, error) {
	q := s[0]
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case q:
			return b.String(), i + 1, nil
		case '\n':
			return "", 0, errors.New("newline in a literal")
		case '\\':
			if i+1 >= len(s) {
				return "", 0, errors.New("unclosed literal")
			}
			i++
			switch e := s[i]; e {
			case '"', '\'', '\\', '?':
				b.WriteByte(e)
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			default:
				return "", 0, fmt.Errorf("escape \\%c", e)
			}
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errors.New("unclosed literal")
}

// cMacro is a #define: params nil for an object-like macro.
type cMacro struct {
	params []string
	body   []ctok
	fn     bool
}

// definePrefix starts a macro definition: its name, then a parameter list
// when "(" follows the name at once.
var definePrefix = regexp.MustCompile(`^#\s*define\s+(\w+)(\([^)]*\))?`)

// cMacros reads the #define lines of src, joining continued lines.
func cMacros(src string) (map[string]cMacro, error) {
	out := map[string]cMacro{}
	lines := strings.Split(strings.ReplaceAll(src, "\\\n", " "), "\n")
	for _, l := range lines {
		m := definePrefix.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		body, err := cTokens(strings.TrimSpace(l)[len(m[0]):])
		if err != nil {
			return nil, fmt.Errorf("macro %s: %w", m[1], err)
		}
		mac := cMacro{body: body, fn: m[2] != ""}
		if mac.fn {
			for p := range strings.SplitSeq(strings.Trim(m[2], "()"), ",") {
				if p = strings.TrimSpace(p); p != "" {
					mac.params = append(mac.params, p)
				}
			}
		}
		out[m[1]] = mac
	}
	return out, nil
}

// maxExpansionDepth bounds the nesting of macro expansions. Anti-DoS
// guard for a build-time tool, not a language limit: the tables nest
// three deep.
const maxExpansionDepth = 32

// expand replaces the macros of toks, arguments expanded before they are
// substituted and the result scanned again, a macro never expanding
// inside its own expansion (C11, 6.10.3.4).
func expand(toks []ctok, macros map[string]cMacro, hidden map[string]bool, depth int) ([]ctok, error) {
	if depth > maxExpansionDepth {
		return nil, errors.New("macros nested too deep")
	}
	var out []ctok
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		mac, ok := macros[t.text]
		if t.kind != 'i' || !ok || hidden[t.text] {
			out = append(out, t)
			continue
		}
		inner := map[string]bool{t.text: true}
		for k := range hidden {
			inner[k] = true
		}
		if !mac.fn {
			e, err := expand(mac.body, macros, inner, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, e...)
			continue
		}
		if i+1 >= len(toks) || toks[i+1].text != "(" {
			out = append(out, t)
			continue
		}
		args, end, err := macroArgs(toks, i+1)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t.text, err)
		}
		if len(args) != len(mac.params) {
			return nil, fmt.Errorf("%s: %d arguments, want %d", t.text, len(args), len(mac.params))
		}
		bound := map[string][]ctok{}
		for k, p := range mac.params {
			a, err := expand(args[k], macros, hidden, depth+1)
			if err != nil {
				return nil, err
			}
			bound[p] = a
		}
		var body []ctok
		for _, b := range mac.body {
			if a, ok := bound[b.text]; ok && b.kind == 'i' {
				body = append(body, a...)
				continue
			}
			body = append(body, b)
		}
		e, err := expand(body, macros, inner, depth+1)
		if err != nil {
			return nil, err
		}
		out = append(out, e...)
		i = end
	}
	return out, nil
}

// macroArgs reads the arguments of a macro call whose "(" is toks[open]:
// the token lists between the commas at its own level, and the index of
// its ")".
func macroArgs(toks []ctok, open int) ([][]ctok, int, error) {
	var args [][]ctok
	var cur []ctok
	level := 0
	for i := open + 1; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.kind == 'p' && t.text == "(":
			level++
		case t.kind == 'p' && t.text == ")" && level == 0:
			return append(args, cur), i, nil
		case t.kind == 'p' && t.text == ")":
			level--
		case t.kind == 'p' && t.text == "," && level == 0:
			args = append(args, cur)
			cur = nil
			continue
		}
		cur = append(cur, t)
	}
	return nil, 0, errors.New("unclosed call")
}

// formatIndices reads the constants of NumberFormatIndex.idl: each
// "const short NAME = VALUE;" whose value is a number or an earlier name,
// with "+N" added.
func formatIndices(idl string) (map[string]int, error) {
	out := map[string]int{}
	re := regexp.MustCompile(`const\s+short\s+(\w+)\s*=\s*(\w+)\s*(?:\+\s*(\d+))?\s*;`)
	for _, m := range re.FindAllStringSubmatch(idl, -1) {
		v, err := strconv.Atoi(m[2])
		if err != nil {
			base, ok := out[m[2]]
			if !ok {
				return nil, fmt.Errorf("%s: unknown %s", m[1], m[2])
			}
			v = base
		}
		if m[3] != "" {
			n, err := strconv.Atoi(m[3])
			if err != nil {
				return nil, err
			}
			v += n
		}
		out[m[1]] = v
	}
	if len(out) == 0 {
		return nil, errors.New("NumberFormatIndex.idl: no constants read")
	}
	return out, nil
}

// calcTables reads the tables of numberformatsbuffer.cxx: each array of
// BuiltinFormat its macros expand, then spBuiltinFormatTables, which names
// each table's locale and parent.
func calcTables(cxx, idl string) ([]calcTable, error) {
	indices, err := formatIndices(idl)
	if err != nil {
		return nil, err
	}
	macros, err := cMacros(cxx)
	if err != nil {
		return nil, err
	}
	// The tables end where spBuiltinFormatTables does: what follows is
	// code, not data.
	end := strings.Index(cxx, "spBuiltinFormatTables[]")
	if end < 0 {
		return nil, errors.New("no spBuiltinFormatTables")
	}
	if j := strings.Index(cxx[end:], "};"); j >= 0 {
		end += j + 2
	}
	var data []string
	for _, l := range strings.Split(strings.ReplaceAll(cxx[:end], "\\\n", " "), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			data = append(data, l)
		}
	}
	toks, err := cTokens(strings.Join(data, "\n"))
	if err != nil {
		return nil, err
	}
	arrays := map[string][]calcFormat{}
	var tables []calcTable
	for i := 0; i < len(toks); i++ {
		// const BuiltinFormat NAME[] = { ... };
		if toks[i].text != "BuiltinFormat" && toks[i].text != "BuiltinFormatTable" || i+5 >= len(toks) ||
			toks[i+1].kind != 'i' || toks[i+2].text != "[" || toks[i+3].text != "]" || toks[i+4].text != "=" || toks[i+5].text != "{" {
			continue
		}
		name := toks[i+1].text
		close, err := matching(toks, i+5)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		body, err := expand(toks[i+6:close], macros, map[string]bool{}, 0)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		items, err := braceItems(body)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		if toks[i].text == "BuiltinFormat" {
			for _, it := range items {
				f, err := calcEntry(it, indices)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", name, err)
				}
				arrays[name] = append(arrays[name], f)
			}
		} else {
			for _, it := range items {
				if len(it) != 3 || len(it[0]) != 1 || len(it[1]) != 1 || len(it[2]) != 1 || it[0][0].kind != 's' || it[1][0].kind != 's' {
					return nil, fmt.Errorf("%s: an entry is not locale, parent, table", name)
				}
				f, ok := arrays[it[2][0].text]
				if !ok {
					return nil, fmt.Errorf("%s: unknown table %s", name, it[2][0].text)
				}
				tables = append(tables, calcTable{Locale: it[0][0].text, Parent: it[1][0].text, Formats: f})
			}
		}
		i = close
	}
	if len(tables) == 0 {
		return nil, errors.New("numberformatsbuffer.cxx: no tables read")
	}
	return tables, nil
}

// matching returns the index of the "}" that closes the "{" at open.
func matching(toks []ctok, open int) (int, error) {
	level := 0
	for i := open; i < len(toks); i++ {
		switch {
		case toks[i].kind != 'p':
		case toks[i].text == "{":
			level++
		case toks[i].text == "}":
			level--
			if level == 0 {
				return i, nil
			}
		}
	}
	return 0, errors.New("unclosed brace")
}

// braceItems reads a list of braced initializers, "{ a, b }, { c, d }",
// into the fields of each, a field being its tokens.
func braceItems(toks []ctok) ([][][]ctok, error) {
	var out [][][]ctok
	for i := 0; i < len(toks); i++ {
		if toks[i].text == "," {
			continue
		}
		if toks[i].text != "{" {
			return nil, fmt.Errorf("unexpected %q", toks[i].text)
		}
		close, err := matching(toks, i)
		if err != nil {
			return nil, err
		}
		var fields [][]ctok
		var cur []ctok
		level := 0
		for _, t := range toks[i+1 : close] {
			switch {
			case t.kind == 'p' && t.text == "{":
				level++
			case t.kind == 'p' && t.text == "}":
				level--
			case t.kind == 'p' && t.text == "," && level == 0:
				fields = append(fields, cur)
				cur = nil
				continue
			}
			cur = append(cur, t)
		}
		out = append(out, append(fields, cur))
		i = close
	}
	return out, nil
}

// calcEntry reads one BuiltinFormat initializer: { id, code, predef,
// reuse }, the code a run of string literals or {}.
func calcEntry(fields [][]ctok, indices map[string]int) (calcFormat, error) {
	var f calcFormat
	if len(fields) != 4 {
		return f, fmt.Errorf("an entry of %d fields", len(fields))
	}
	id, err := cSum(fields[0])
	if err != nil {
		return f, err
	}
	if id < 0 || id > maxCalcID {
		return f, fmt.Errorf("id %d", id)
	}
	f.ID = id
	var code strings.Builder
	literal := false
	for _, t := range fields[1] {
		switch {
		case t.kind == 's':
			code.WriteString(t.text)
			literal = true
		case t.text == "{" || t.text == "}":
		default:
			return f, fmt.Errorf("id %d: code %q", id, t.text)
		}
	}
	predef := -1
	if p := fields[2]; len(p) > 0 && p[len(p)-1].kind == 'i' {
		v, ok := indices[p[len(p)-1].text]
		if !ok {
			return f, fmt.Errorf("id %d: unknown index %s", id, p[len(p)-1].text)
		}
		predef = v
	} else if predef, err = cSum(p); err != nil {
		return f, err
	}
	reuse, err := cSum(fields[3])
	if err != nil {
		return f, err
	}
	switch {
	case literal && code.Len() > 0:
		f.Code = code.String()
	case predef >= 0:
		f.Predef = &predef
	case reuse >= 0 && reuse <= maxCalcID:
		f.Reuse = &reuse
	default:
		return f, fmt.Errorf("id %d: no code, index or reuse", id)
	}
	return f, nil
}

// cSum evaluates a sum of integers: "5 + 2", "-1".
func cSum(toks []ctok) (int, error) {
	sum, sign, want := 0, 1, true
	for _, t := range toks {
		switch {
		case t.kind == 'p' && t.text == "-" && want:
			sign = -sign
		case t.kind == 'p' && t.text == "+" && !want:
			want = true
		case t.kind == 'n' && want:
			n, err := strconv.Atoi(t.text)
			if err != nil {
				return 0, err
			}
			sum += sign * n
			sign, want = 1, false
		default:
			return 0, fmt.Errorf("not a sum: %q", t.text)
		}
	}
	if want {
		return 0, errors.New("empty sum")
	}
	return sum, nil
}

// ownFormats are the formats of NumberFormatIndex no locale states, which
// svl writes the same for every locale (zforlist.cxx,
// ImpGenerateFormats): FRACTION_1, FRACTION_2 and TEXT.
var ownFormats = map[int]string{10: "# ?/?", 11: "# ??/??", 49: "@"}

// calcBuiltins resolves Calc's tables into the generated table.
type calcBuiltins struct {
	tables map[string]calcTable
	tr     translator
	// slots map a NumberFormatIndex to its place among a locale's codes
	// (codeOrder); uniform holds the code of an index that has none, the
	// same in every locale.
	slots   map[int]int
	uniform map[int]string
	base0   map[int]calcFormat
}

// newCalcBuiltins checks the tables against the locales: every index a
// table names is a code the generated table holds for each locale, or the
// same code in every locale.
func newCalcBuiltins(locs []localeRecord, tables []calcTable) (*calcBuiltins, error) {
	cb := &calcBuiltins{tables: map[string]calcTable{}, slots: map[int]int{}, uniform: maps.Clone(ownFormats)}
	for _, t := range tables {
		if _, dup := cb.tables[t.Locale]; dup {
			return nil, fmt.Errorf("two tables %s", t.Locale)
		}
		cb.tables[t.Locale] = t
	}
	for _, l := range locs {
		if l.Tag == "en-US" {
			// Calc's tables are written in en-US syntax.
			cb.tr = translator{kw: keywordsFor(l.Tag), general: "General", decimal: l.Decimal, group: l.Group, time100: l.Time100Sep, dowSep: l.DayOfWeekSep}
		}
	}
	if cb.tr.decimal == "" {
		return nil, errors.New("no en-US locale")
	}
	for k, key := range codeOrder {
		if n, err := strconv.Atoi(key); err == nil {
			cb.slots[n] = k
		}
	}
	for _, t := range tables {
		for _, f := range t.Formats {
			if f.Predef == nil {
				continue
			}
			n := *f.Predef
			if _, ok := cb.slots[n]; ok {
				for _, l := range locs {
					if l.Codes[strconv.Itoa(n)] == "" && !slices.ContainsFunc(l.Unsupported, func(u string) bool { return strings.HasPrefix(u, strconv.Itoa(n)+" ") }) {
						return nil, fmt.Errorf("table %s names format index %d, which locale %s does not state", t.Locale, n, l.Tag)
					}
				}
				continue
			}
			if _, ok := cb.uniform[n]; ok {
				continue
			}
			code := ""
			for _, l := range locs {
				c := l.Codes[strconv.Itoa(n)]
				if c == "" || code != "" && c != code {
					return nil, fmt.Errorf("table %s names format index %d, which locale %s states as %q", t.Locale, n, l.Tag, c)
				}
				code = c
			}
			cb.uniform[n] = code
		}
	}
	var err error
	if cb.base0, err = cb.resolve("*"); err != nil {
		return nil, err
	}
	return cb, nil
}

// resolve returns the format of each id under the system locale tag as
// Calc's import builds them (NumberFormatsBuffer::insertBuiltinFormats):
// the table of that exact tag, else the base table, with the tables it
// derives from beneath it; an id the most specific table that names it
// reuses takes that id's format, the reused ids copied in ascending order.
func (cb *calcBuiltins) resolve(tag string) (map[int]calcFormat, error) {
	t, ok := cb.tables[tag]
	if !ok {
		t = cb.tables["*"]
	}
	var chain []calcTable
	for depth := 0; ; depth++ {
		if depth > 8 {
			return nil, fmt.Errorf("table %s: parents nested too deep", tag)
		}
		chain = append(chain, t)
		if t.Parent == "" {
			break
		}
		if t, ok = cb.tables[t.Parent]; !ok {
			return nil, fmt.Errorf("table %s: unknown parent", tag)
		}
	}
	out := map[int]calcFormat{}
	reuse := map[int]int{}
	for i := len(chain) - 1; i >= 0; i-- {
		for _, f := range chain[i].Formats {
			if f.Reuse != nil {
				reuse[f.ID] = *f.Reuse
				delete(out, f.ID)
				continue
			}
			delete(reuse, f.ID)
			out[f.ID] = f
		}
	}
	for id := range maxCalcID + 1 {
		r, ok := reuse[id]
		if !ok {
			continue
		}
		f, ok := out[r]
		if !ok {
			return nil, fmt.Errorf("table %s: id %d reuses %d, which has no format yet", tag, id, r)
		}
		f.ID = id
		out[id] = f
	}
	return out, nil
}

// errUnread reports a code of Calc's that numfmt does not read.
var errUnread = errors.New("a code numfmt does not read")

// literal returns the Go literal of f: its code in SpreadsheetML syntax,
// or the place of the locale's own code; errUnread for a code that holds a
// keyword numfmt does not read (an era, Thai digits).
func (cb *calcBuiltins) literal(f calcFormat) (string, error) {
	if f.Predef != nil {
		if slot, ok := cb.slots[*f.Predef]; ok {
			return fmt.Sprintf("{id: %d, slot: %d}", f.ID, slot), nil
		}
		return fmt.Sprintf("{id: %d, slot: -1, code: %q}", f.ID, cb.uniform[*f.Predef]), nil
	}
	code, err := cb.tr.translate(f.Code)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUnread, err)
	}
	return fmt.Sprintf("{id: %d, slot: -1, code: %q}", f.ID, code), nil
}

// sameFormat reports two entries that give the same format.
func sameFormat(a, b calcFormat) bool {
	return a.Code == b.Code && (a.Predef == nil) == (b.Predef == nil) && (a.Predef == nil || *a.Predef == *b.Predef)
}

// base returns the Go literal of the base table, every id from 0.
func (cb *calcBuiltins) base() (string, error) {
	var b strings.Builder
	b.WriteString("[...]calcBuiltin{\n")
	for id := range maxCalcID + 1 {
		f, ok := cb.base0[id]
		if !ok {
			return "", fmt.Errorf("the base table has no id %d", id)
		}
		lit, err := cb.literal(f)
		if err != nil {
			return "", fmt.Errorf("the base table, id %d: %w", id, err)
		}
		b.WriteString("\t" + lit + ",\n")
	}
	b.WriteString("}")
	return b.String(), nil
}

// overrides returns the Go literal of the formats the table of tag gives
// that the base table does not, in id order, "" when it gives none. A code
// numfmt does not read leaves its id the base table's format.
func (cb *calcBuiltins) overrides(tag string) (string, error) {
	if _, ok := cb.tables[tag]; !ok {
		return "", nil
	}
	res, err := cb.resolve(tag)
	if err != nil {
		return "", err
	}
	var lits []string
	for id := range maxCalcID + 1 {
		f, ok := res[id]
		if !ok || sameFormat(f, cb.base0[id]) {
			continue
		}
		lit, err := cb.literal(f)
		if errors.Is(err, errUnread) {
			continue
		}
		if err != nil {
			return "", err
		}
		lits = append(lits, lit)
	}
	if len(lits) == 0 {
		return "", nil
	}
	return "[]calcBuiltin{" + strings.Join(lits, ", ") + "}", nil
}
