package lexer

import (
	"reflect"
	"testing"
)

// testCommands is a command set shaped like the content-stream one: operators
// that are prefixes of others (B and BT, f and f*), one reached only through a
// prefix that is no command (Tf and T* through T), one reached only through
// partial commands (BDC through B, false through f), and keywords.
var testCommands = NewCommandSet("B", "BT", "BDC", "ET", "q", "Q", "Tf", "T*", "f", "f*", "true", "false", "null")

// lexCommands returns every token of src up to the end of the data, read with
// cs, or with no command set when cs is nil.
func lexCommands(t *testing.T, src string, cs *CommandSet) []Token {
	t.Helper()
	lex := newLexer(src).WithCommands(cs)
	var toks []Token
	for range len(src) + 1 {
		tok, err := lex.NextToken()
		if err != nil {
			t.Fatalf("NextToken(%q): %v", src, err)
		}
		if tok.Type == TokenEOF {
			return toks
		}
		toks = append(toks, tok)
	}
	t.Fatalf("NextToken(%q) never reached the end of the data", src)
	return nil
}

// kw is a keyword token.
func kw(v string) Token { return Token{Type: TokenKeyword, Value: v} }

// num is a number token.
func num(v string) Token { return Token{Type: TokenNumber, Value: v} }

// TestWithCommandsSplitsGluedCommands pins the PDF.js reading of a run of
// regular characters: a command ends before the character that would turn it
// into a string that is no command, and the rest is the next token.
func TestWithCommandsSplitsGluedCommands(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		src  string
		want []Token
	}{
		{"two operators", "ETBT", []Token{kw("ET"), kw("BT")}},
		{"two one-letter operators", "qQ", []Token{kw("q"), kw("Q")}},
		{"the text object pair", "BTET", []Token{kw("BT"), kw("ET")}},
		{"an operator then an unknown run", "ETXX", []Token{kw("ET"), kw("XX")}},
		{"an unknown run stays whole", "XXET", []Token{kw("XXET")}},
		{"a prefix that is no command", "Tf", []Token{kw("Tf")}},
		{"its sibling", "T*", []Token{kw("T*")}},
		{"the siblings glued", "T*Tf", []Token{kw("T*"), kw("Tf")}},
		{"the siblings glued the other way", "TfT*", []Token{kw("Tf"), kw("T*")}},
		{"a command that extends a shorter one", "f*", []Token{kw("f*")}},
		{"a command reached through partial commands", "BDC", []Token{kw("BDC")}},
		{"a partial command ends a run as PDF.js ends it", "BDX", []Token{kw("BD"), kw("X")}},
		{"a keyword reached through partial commands", "false", []Token{kw("false")}},
		{"a keyword then a command", "falsef", []Token{kw("false"), kw("f")}},
		{"a command then a number", "q1", []Token{kw("q"), num("1")}},
		{"a number is left alone", "1e2", []Token{num("1e2")}},
		{"a number run into endobj is still split", "7559endobj", []Token{num("7559"), kw("endobj")}},
		{"white space ends a command as before", "ET BT", []Token{kw("ET"), kw("BT")}},
		{"a delimiter ends a command as before", "Tf/F1", []Token{kw("Tf"), {Type: TokenName, Value: "/F1"}}},
	} {
		if got := lexCommands(t, tc.src, testCommands); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: %q lexes as %v, want %v", tc.name, tc.src, got, tc.want)
		}
	}
}

// TestNoCommandsKeepsTokenBoundaries pins the default: without a command set a
// run of regular characters ends only at white space or a delimiter (ISO
// 32000-1, 7.2.2), which is how the objects of a file body are read.
func TestNoCommandsKeepsTokenBoundaries(t *testing.T) {
	t.Parallel()

	for _, src := range []string{"ETBT", "qQ", "BTET", "ETXX", "q1", "falsef"} {
		if got, want := lexCommands(t, src, nil), []Token{kw(src)}; !reflect.DeepEqual(got, want) {
			t.Errorf("%q lexes as %v, want the one keyword", src, got)
		}
	}
}

// TestNewCommandSetAddsPartialCommands pins the prefixes NewCommandSet adds: the
// ones between a word and a shorter member it is read through, and no others.
func TestNewCommandSetAddsPartialCommands(t *testing.T) {
	t.Parallel()

	for _, s := range []string{"BD", "fa", "fal", "fals"} {
		if !testCommands.has([]byte(s)) {
			t.Errorf("%q is not a member, so the greedy read stops before it", s)
		}
	}
	for _, s := range []string{"T", "E", "t", "tr", "tru", "n", "nu", "BDCX", ""} {
		if testCommands.has([]byte(s)) {
			t.Errorf("%q is a member, which no word needs", s)
		}
	}
	var none *CommandSet
	if none.has([]byte("ET")) {
		t.Error("a nil set has a member")
	}
}
