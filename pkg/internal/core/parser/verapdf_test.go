package parser

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ioat"
	"github.com/gotenberg/litho/pkg/internal/core/lexer"
)

// veraPDFParser returns a parser over src that reads it as a content stream is
// read for validation: with veraPDF's tokens, noting where veraPDF gives up on
// the stream when containers is set.
func veraPDFParser(src string, containers bool) *Parser {
	lex := lexer.NewLexer(ioat.NewReader(bytes.NewReader([]byte(src)), int64(len(src)), 0)).WithVeraPDFTokens()
	p := NewParser(lex)
	if containers {
		p.WithVeraPDFContainers()
	}
	return p
}

// TestWithVeraPDFContainersNotesWhereVeraPDFGivesUp pins the containers of a
// content stream veraPDF cannot read (COSParser.getArray and getDictionary,
// veraPDF-parser 1.30). Every case was measured with veraPDF 1.30 at PDF/A-1b
// and PDF/A-2b, the container written after operators that break a rule of
// each kind: veraPDF reports none of them where the container is noted here,
// and all of them where it is not.
func TestWithVeraPDFContainersNotesWhereVeraPDFGivesUp(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src       string
		abandoned bool
	}{
		{"[1 yy]", true},
		{"[1e2]", true},
		{"[1.5e2]", true},
		{"[)]", true},
		{"[{]", true},
		{"[>>]", true},
		{"[> ]", true},
		{"[0 R]", true},
		{"[1.0 2 R]", true},
		{"[1 2", true},
		{"[true null false 1 0 R (a) <41> /N [1] <</A 1>>]", false},
		{"[(a) - (b)]", false},
		{"[[1 2] <</A [3]>>]", false},
		{"[(a) %c\n 1]", false},
		{"<</MCID 0 /A xx>>", false},
		{"<</A )>>", false},
		{"<</A ]>>", false},
		{"<</A endobj>>", false},
		{"<</A 1 2>>", false},
		{"<</A 1 2 3>>", false},
		{"<</A 1 2 /B>>", false},
		{"<</MCID 0 /A [1 2] /B <</C /D>> /E (x)>>", false},
		{"<</A 1 xx>>", true},
		{"<</A 1e2>>", true},
		{"<</A>>", true},
		{"<<xx /A 1>>", true},
		{"<</A >x>>", true},
		{"<</A 1 2 /B 3>>", true},
		{"<</A 1 2 3 4>>", true},
		{"<</A 1 0 R 2>>", true},
		{"<</A 1.5 2>>", true},
		{"<</A xx /B>>", true},
		{"<</A [1 xx]>>", true},
		{"<</A <<xx 1>> >>", true},
		{"<</A 1", true},
	} {
		p := veraPDFParser(tc.src, true)
		if _, err := p.ParseObject(context.Background()); err != nil {
			t.Errorf("ParseObject(%q): %v", tc.src, err)
			continue
		}
		if got := p.Abandoned(); got != tc.abandoned {
			t.Errorf("%q: Abandoned() = %v, want %v", tc.src, got, tc.abandoned)
		}
		p = veraPDFParser(tc.src, false)
		if _, err := p.ParseObject(context.Background()); err != nil || p.Abandoned() {
			t.Errorf("%q: without WithVeraPDFContainers the parse gives %v and Abandoned() = %v, want neither", tc.src, err, p.Abandoned())
		}
	}
}

// TestWithVeraPDFContainersInlineImageKeys pins that veraPDF gives up on a
// content stream whose inline image dictionary holds a key that is no name,
// such as the exponent of a value (PDFStreamParser.parseNextToken, veraPDF-parser
// 1.30), and reads on past a value it cannot read. Measured with veraPDF 1.30 at
// PDF/A-1b and PDF/A-2b.
func TestWithVeraPDFContainersInlineImageKeys(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		src       string
		abandoned bool
	}{
		{"/W 1 /H 1 /BPC 8 /CS /G /Foo 1.5e1 ID \x80 EI ", true},
		{"/W 1 /H 1 /BPC 8 /CS /G /D [1 xx] ID \x80 EI ", true},
		{"/W 1 /H 1 /BPC 8 /CS /G /Foo xx ID \x80 EI ", false},
		{"/W 1 /H 1 /BPC 8 /CS /G ID \x80 EI ", false},
	} {
		p := veraPDFParser(tc.src, true)
		if _, err := p.ParseInlineImageDict(context.Background()); err != nil {
			t.Errorf("ParseInlineImageDict(%q): %v", tc.src, err)
			continue
		}
		if got := p.Abandoned(); got != tc.abandoned {
			t.Errorf("%q: Abandoned() = %v, want %v", tc.src, got, tc.abandoned)
		}
	}
}
