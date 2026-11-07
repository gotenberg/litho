package css

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// suiteDir holds the css-parsing-tests suite testdata/fetch-css-parsing-tests.sh
// downloads.
const suiteDir = "testdata/css-parsing-tests"

// knownDivergences lists the suite cases litho answers differently on
// purpose, by file and input, with the reason.
var knownDivergences = map[string]string{
	// The suite tokenizes unicode-range tokens, which CSS Syntax 3 dropped
	// from the tokenizer (it breaks selectors such as u+a); litho reads
	// <urange> from ordinary tokens in the unicode-range descriptor (CSS
	// Syntax 3, 7.1).
	"component_value_list.json:u+1 U+10 U+100 U+1000 U+10000 U+100000 U+1000000":        "urange",
	"component_value_list.json:u+? u+1? U+10? U+100? U+1000? U+10000? U+100000?":        "urange",
	"component_value_list.json:u+?? U+1?? U+10?? U+100?? U+1000?? U+10000??":            "urange",
	"component_value_list.json:u+??? U+1??? U+10??? U+100??? U+1000???":                 "urange",
	"component_value_list.json:u+???? U+1???? U+10???? U+100????":                       "urange",
	"component_value_list.json:u+????? U+1????? U+10?????":                              "urange",
	"component_value_list.json:u+?????? U+1??????":                                      "urange",
	"component_value_list.json:u+1-2 U+100000-2 U+1000000-2 U+10-200000":                "urange",
	"component_value_list.json:\u00f9+12 \u00dc+12 u +12 U+ 12 U+12 - 20 U+1?2 U+1?-50": "urange",
	// The suite tokenizes the match operators and || as single tokens,
	// which CSS Syntax 3 dropped; litho reads them as delims in the
	// attribute selector parser (Selectors 4, 6).
	"component_value_list.json:~=|=^=$=*=||<!------> |/**/| ~/**/=":                                              "match tokens",
	"component_value_list.json:a:not([href^=http\\:],  [href ^=\t'https\\:'\n]) { color: rgba(0%, 100%, 50%); }": "match tokens",
}

// serializeList renders component values in the suite's JSON shape. A
// string or url cut short by the end of the input is followed by the
// suite's eof error marker.
func serializeList(l []token) []any {
	out := []any{}
	for _, t := range l {
		out = append(out, serializeToken(t))
		if t.eof {
			switch t.typ {
			case tString:
				out = append(out, []any{"error", "eof-in-string"})
			case tURL:
				out = append(out, []any{"error", "eof-in-url"})
			}
		}
	}
	return out
}

func numType(t token) string {
	if t.integer {
		return "integer"
	}
	return "number"
}

func serializeToken(t token) any {
	switch t.typ {
	case tIdent:
		return []any{"ident", t.value}
	case tAtKeyword:
		return []any{"at-keyword", t.value}
	case tHash:
		k := "unrestricted"
		if t.id {
			k = "id"
		}
		return []any{"hash", t.value, k}
	case tString:
		return []any{"string", t.value}
	case tBadString:
		return []any{"error", "bad-string"}
	case tURL:
		return []any{"url", t.value}
	case tBadURL:
		return []any{"error", "bad-url"}
	case tDelim:
		return string(t.delim)
	case tNumber:
		return []any{"number", t.repr, t.num, numType(t)}
	case tPercentage:
		return []any{"percentage", t.repr, t.num, numType(t)}
	case tDimension:
		return []any{"dimension", t.repr, t.num, numType(t), t.value}
	case tWhitespace:
		return " "
	case tCDO:
		return "<!--"
	case tCDC:
		return "-->"
	case tColon:
		return ":"
	case tSemicolon:
		return ";"
	case tComma:
		return ","
	case tRParen:
		return []any{"error", ")"}
	case tRBracket:
		return []any{"error", "]"}
	case tRBrace:
		return []any{"error", "}"}
	case tLBrace:
		return append([]any{"{}"}, serializeList(t.children)...)
	case tLBracket:
		return append([]any{"[]"}, serializeList(t.children)...)
	case tLParen:
		return append([]any{"()"}, serializeList(t.children)...)
	case tFunction:
		return append([]any{"function", t.value}, serializeList(t.children)...)
	}
	return nil
}

func serializeItem(it item) any {
	switch it.kind {
	case itemAtRule:
		var block any
		if it.hasBlock {
			block = serializeList(it.block)
		}
		return []any{"at-rule", it.name, serializeList(it.prelude), block}
	case itemQualified:
		return []any{"qualified rule", serializeList(it.prelude), serializeList(it.block)}
	case itemDeclaration:
		return []any{"declaration", it.name, serializeList(it.value), it.important}
	}
	return []any{"error", it.name}
}

func collect(run func(yield func(item) bool)) any {
	out := []any{}
	run(func(it item) bool {
		out = append(out, serializeItem(it))
		return true
	})
	return out
}

// normalize round-trips v through JSON so numbers compare as float64.
func normalize(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return out
}

func oneComponentValue(src string) any {
	p := &parser{tz: newTokenizer(src, maxTokens)}
	for p.peekType() == tWhitespace {
		p.next()
	}
	t := p.next()
	if t.typ == tEOF {
		return []any{"error", "empty"}
	}
	v := p.componentValue(t)
	for p.peekType() == tWhitespace {
		p.next()
	}
	if p.peekType() != tEOF {
		return []any{"error", "extra-input"}
	}
	out := serializeList([]token{v})
	if len(out) == 1 {
		return out[0]
	}
	return out
}

// serializeColor renders a colour as CSS Color 4 serializes it: rgb() or
// rgba() with 0 to 255 channels.
func serializeColor(c colorValue) string {
	if c.current {
		return "currentcolor"
	}
	f := func(v float64) string {
		return strconv.FormatFloat(math.Round(v*255*1e6)/1e6, 'f', -1, 64)
	}
	if c.c.A == 1 {
		return fmt.Sprintf("rgb(%s, %s, %s)", f(c.c.R), f(c.c.G), f(c.c.B))
	}
	return fmt.Sprintf("rgba(%s, %s, %s, %s)", f(c.c.R), f(c.c.G), f(c.c.B), strconv.FormatFloat(math.Round(c.c.A*1e6)/1e6, 'f', -1, 64))
}

// colorsMatch compares two serialized colours channel by channel within
// half a unit of the last digit the suite prints.
func colorsMatch(got, want string) bool {
	nums := func(s string) []float64 {
		i := strings.IndexByte(s, '(')
		if i < 0 || !strings.HasSuffix(s, ")") {
			return nil
		}
		var out []float64
		for _, p := range strings.Split(s[i+1:len(s)-1], ",") {
			v, err := strconv.ParseFloat(strings.TrimSpace(p), 64)
			if err != nil {
				return nil
			}
			out = append(out, v)
		}
		return out
	}
	if got == want {
		return true
	}
	g, w := nums(got), nums(want)
	if g == nil || len(g) != len(w) || got[:3] != want[:3] {
		return false
	}
	for i := range g {
		if math.Abs(g[i]-w[i]) > 1e-3 {
			return false
		}
	}
	return true
}

func parseColorInput(src string) any {
	l, _ := parseComponentValues(src)
	l = trimWS(l)
	if len(l) != 1 {
		return nil
	}
	c, ok := parseColor(l[0])
	if !ok {
		return nil
	}
	return serializeColor(c)
}

func anbInput(src string) any {
	l, _ := parseComponentValues(src)
	a, b, ok := parseAnB(l)
	if !ok {
		return nil
	}
	return []any{a, b}
}

// TestCSSParsingTests_Suite runs the css-parsing-tests suite against the
// tokenizer, the parser, the An+B parser and the colour parser.
func TestCSSParsingTests_Suite(t *testing.T) {
	if _, err := os.Stat(suiteDir); err != nil {
		t.Skipf("%s is absent: run testdata/fetch-css-parsing-tests.sh", suiteDir)
	}
	runners := map[string]func(string) any{
		"component_value_list.json": func(s string) any { l, _ := parseComponentValues(s); return serializeList(l) },
		"one_component_value.json":  oneComponentValue,
		"declaration_list.json": func(s string) any {
			return collect((&parser{tz: newTokenizer(s, maxTokens)}).declarations)
		},
		"one_declaration.json": func(s string) any { return serializeItem(parseOneDeclaration(s)) },
		"one_rule.json":        func(s string) any { return serializeItem(parseOneRule(s)) },
		"rule_list.json": func(s string) any {
			p := &parser{tz: newTokenizer(s, maxTokens)}
			return collect(func(y func(item) bool) { p.rules(false, y) })
		},
		"stylesheet.json": func(s string) any {
			p := &parser{tz: newTokenizer(s, maxTokens)}
			return collect(func(y func(item) bool) { p.rules(true, y) })
		},
		"An+B.json":                anbInput,
		"color_keywords_3.json":    parseColorInput,
		"color_hexadecimal_3.json": parseColorInput,
		"color_hsl_3.json":         parseColorInput,
	}
	total, passed, diverged := 0, 0, 0
	for _, name := range []string{
		"component_value_list.json", "one_component_value.json", "declaration_list.json",
		"one_declaration.json", "one_rule.json", "rule_list.json", "stylesheet.json",
		"An+B.json", "color_keywords_3.json", "color_hexadecimal_3.json", "color_hsl_3.json",
	} {
		data, err := os.ReadFile(filepath.Join(suiteDir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		var cases []any
		if err := json.Unmarshal(data, &cases); err != nil {
			t.Fatalf("decoding %s: %v", name, err)
		}
		run := runners[name]
		filePassed := 0
		for i := 0; i+1 < len(cases); i += 2 {
			input, ok := cases[i].(string)
			if !ok {
				continue
			}
			total++
			got := normalize(t, run(input))
			want := cases[i+1]
			match := reflect.DeepEqual(got, want)
			if !match && strings.HasPrefix(name, "color_") {
				gs, gok := got.(string)
				ws, wok := want.(string)
				match = gok && wok && colorsMatch(gs, ws)
			}
			switch {
			case match:
				passed++
				filePassed++
			case knownDivergences[name+":"+input] != "":
				diverged++
			default:
				gj, _ := json.Marshal(got)
				wj, _ := json.Marshal(want)
				t.Errorf("%s: %q\n got  %s\n want %s", name, input, gj, wj)
			}
		}
		t.Logf("%s: %d of %d cases pass", name, filePassed, len(cases)/2)
	}
	t.Logf("css-parsing-tests: %d of %d cases pass, %d known divergences", passed, total, diverged)
}

// peekType returns the type of the next token without consuming it.
func (p *parser) peekType() tokenType {
	t := p.next()
	p.reconsume()
	return t.typ
}

// parseOneDeclaration parses a declaration (CSS Syntax 3, 5.3.6): the
// whole input is its name and value.
func parseOneDeclaration(src string) item {
	p := &parser{tz: newTokenizer(src, maxTokens)}
	for p.peekType() == tWhitespace {
		p.next()
	}
	first := p.next()
	if first.typ == tEOF {
		return item{kind: itemError, name: "empty"}
	}
	if first.typ != tIdent {
		return item{kind: itemError, name: "invalid"}
	}
	list := []token{first}
	for {
		t := p.next()
		if t.typ == tEOF {
			break
		}
		list = append(list, p.componentValue(t))
	}
	it, ok := declaration(list)
	if !ok {
		return item{kind: itemError, name: "invalid"}
	}
	return it
}

// parseOneRule parses a rule (CSS Syntax 3, 5.3.5).
func parseOneRule(src string) item {
	p := &parser{tz: newTokenizer(src, maxTokens)}
	for p.peekType() == tWhitespace {
		p.next()
	}
	var it item
	switch t := p.next(); t.typ { //nolint:exhaustive // the other token types take the default path
	case tEOF:
		return item{kind: itemError, name: "empty"}
	case tAtKeyword:
		it = p.atRule(t)
	default:
		p.reconsume()
		it = p.qualifiedRule()
		if it.kind == itemError {
			return it
		}
	}
	for p.peekType() == tWhitespace {
		p.next()
	}
	if p.peekType() != tEOF {
		return item{kind: itemError, name: "extra-input"}
	}
	return it
}
