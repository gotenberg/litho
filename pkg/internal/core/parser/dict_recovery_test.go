package parser

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestParseDictKeepsKeysAfterStrayDelimiter pins that a stray delimiter among a
// dictionary's keys costs only that token.
//
// A stray keyword was already tolerated; a stray delimiter was not, and the
// asymmetry cost the whole dictionary. Damaged files produce both.
//
// # Corpus evidence
//
//   - pdfjs poppler-742-0-fuzzed.pdf: closes its /AcroForm with `/Q 0 > >>`
//     and lost every form field. qpdf and poppler read it.
//   - pdfjs poppler-937-0-fuzzed.pdf: writes `/Kids 3 0 R]` and lost its page
//     tree. qpdf and poppler read it.
func TestParseDictKeepsKeysAfterStrayDelimiter(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"stray closing angle", "<< /A 1 > /B 2 >>"},
		{"stray closing bracket", "<< /A 1 ] /B 2 >>"},
		{"stray bracket after a reference", "<< /A 3 0 R] /B 2 >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := parseOne(t, tc.src)
			dict, ok := obj.(ast.PdfDict)
			if !ok {
				t.Fatalf("ParseObject(%q) = %T, want ast.PdfDict", tc.src, obj)
			}
			if _, ok := dict["A"]; !ok {
				t.Errorf("ParseObject(%q) lost /A: %v", tc.src, dict)
			}
			if got, ok := dict["B"]; !ok {
				t.Errorf("ParseObject(%q) lost the key after the stray delimiter: %v", tc.src, dict)
			} else if got != ast.PdfInt(2) {
				t.Errorf("ParseObject(%q) /B = %v, want 2", tc.src, got)
			}
		})
	}
}

// TestParseDictStillClosesOnDoubleAngle checks the tolerance did not swallow a
// real dictionary terminator, which would run one dictionary into the next.
func TestParseDictStillClosesOnDoubleAngle(t *testing.T) {
	obj := parseOne(t, "<< /A 1 >> << /B 2 >>")
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("ParseObject = %T, want ast.PdfDict", obj)
	}
	if _, taken := dict["B"]; taken {
		t.Errorf("first dictionary absorbed the second: %v", dict)
	}
	if got := dict["A"]; got != ast.PdfInt(1) {
		t.Errorf("/A = %v, want 1", got)
	}
}

// checkDictEntries fails t unless src parses to a dictionary holding exactly
// want.
func checkDictEntries(t *testing.T, src string, want ast.PdfDict) {
	t.Helper()
	obj := parseOne(t, src)
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("ParseObject(%q) = %T, want ast.PdfDict", src, obj)
	}
	if len(dict) != len(want) {
		t.Errorf("ParseObject(%q) = %v, want %v", src, dict, want)
		return
	}
	for k, v := range want {
		if got, ok := dict[k]; !ok || got.String() != v.String() {
			t.Errorf("ParseObject(%q) = %v, want %v", src, dict, want)
			return
		}
	}
}

// TestParseDictKeepsEntriesAroundMalformedValue pins that a keyword or a stray
// delimiter where a value belongs costs the entry it damages, not every entry
// after it. It used to close the dictionary there, so a catalog whose /Type key
// held a NUL came back empty and took its /Pages with it.
func TestParseDictKeepsEntriesAroundMalformedValue(t *testing.T) {
	ref := ast.PdfIndirectRef{ObjectNum: 2, GenNum: 0}
	for _, tc := range []struct {
		name string
		src  string
		want ast.PdfDict
	}{
		{
			// The NUL ends the name, so pe is a keyword and /Catalog is the
			// value of the split key, not a key of its own.
			"NUL splits the first key",
			"<< /Ty\x00pe /Catalog /Pages 2 0 R >>",
			ast.PdfDict{"Pages": ref},
		},
		{
			"NUL splits a key after an entry",
			"<< /Count 1 /Ty\x00pe /Catalog /Pages 2 0 R >>",
			ast.PdfDict{"Count": ast.PdfInt(1), "Pages": ref},
		},
		{
			"space splits a key before name pairs",
			"<< /Type /Font /Base Font /Helvetica /Subtype /Type1 >>",
			ast.PdfDict{"Type": ast.PdfName("Font"), "Subtype": ast.PdfName("Type1")},
		},
		{
			"split key closes the dictionary",
			"<< /A 1 /Ty\x00pe /Catalog >>",
			ast.PdfDict{"A": ast.PdfInt(1)},
		},
		{
			// PDF.js reads the keyword as the value and stays in step here.
			"keyword replaces a value before a number",
			"<< /A junk /B 2 >>",
			ast.PdfDict{"B": ast.PdfInt(2)},
		},
		{
			"keyword replaces a value before a name pair",
			"<< /A junk /Type /Font >>",
			ast.PdfDict{"Type": ast.PdfName("Font")},
		},
		{
			"keyword replaces the last value",
			"<< /A 1 /B junk >>",
			ast.PdfDict{"A": ast.PdfInt(1)},
		},
		{
			"two keywords in a row",
			"<< /A junk more /B 2 >>",
			ast.PdfDict{"B": ast.PdfInt(2)},
		},
		{
			"stray bracket replaces a value",
			"<< /A ] /B 2 >>",
			ast.PdfDict{"B": ast.PdfInt(2)},
		},
		{
			"stray parenthesis replaces a value",
			"<< /A ) /B [1] >>",
			ast.PdfDict{"B": ast.PdfArray{ast.PdfInt(1)}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkDictEntries(t, tc.src, tc.want)
		})
	}
}

// TestParseDictResyncsAfterMalformedKey pins how the entries after a keyword
// or a stray value in key position pair up. The stray token is dropped either
// way, and the names after it pair up in whichever alignment fits what
// follows them.
func TestParseDictResyncsAfterMalformedKey(t *testing.T) {
	ref := ast.PdfIndirectRef{ObjectNum: 2, GenNum: 0}
	for _, tc := range []struct {
		name string
		src  string
		want ast.PdfDict
	}{
		{
			// A space inside a name value leaves its tail where a key belongs.
			"space splits a name value",
			"<< /BaseFont /Arial,Unicode MS /Type /Font >>",
			ast.PdfDict{"BaseFont": ast.PdfName("Arial,Unicode"), "Type": ast.PdfName("Font")},
		},
		{
			// A key written without its solidus is a keyword, and its value
			// follows it.
			"key without its solidus",
			"<< Type /Catalog /Pages 2 0 R >>",
			ast.PdfDict{"Pages": ref},
		},
		{
			"stray number before a name pair",
			"<< /A 1 2 /B /C >>",
			ast.PdfDict{"A": ast.PdfInt(1), "B": ast.PdfName("C")},
		},
		{
			"stray string before a pair",
			"<< /A 1 (x) /B 3 >>",
			ast.PdfDict{"A": ast.PdfInt(1), "B": ast.PdfInt(3)},
		},
		{
			"stray reference before a pair",
			"<< /A 1 3 0 R /B 2 >>",
			ast.PdfDict{"A": ast.PdfInt(1), "B": ast.PdfInt(2)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkDictEntries(t, tc.src, tc.want)
		})
	}
}

// TestParseDictResyncLookaheadIsBounded checks that a run of names longer than
// maxDictResyncLookahead is read the way PDF.js reads it, its first name a
// key, rather than buffered to its end.
func TestParseDictResyncLookaheadIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("<< /A junk")
	// Read to its end, an odd run followed by >> would make its first name a
	// value.
	for i := range maxDictResyncLookahead + 1 {
		fmt.Fprintf(&b, " /N%d", i)
	}
	b.WriteString(" >>")
	dict, ok := parseOne(t, b.String()).(ast.PdfDict)
	if !ok {
		t.Fatal("ParseObject did not return a dictionary")
	}
	if got := dict["N0"]; got != ast.PdfName("N1") {
		t.Errorf("/N0 = %v, want /N1", got)
	}
}

// TestParseDictRecoveryStopsAtObjectEnd checks that recovering from a
// malformed entry never runs past the end of the object: an endobj or a
// stream keyword closes the dictionary as it does when its >> is missing.
func TestParseDictRecoveryStopsAtObjectEnd(t *testing.T) {
	t.Run("endobj after a malformed value", func(t *testing.T) {
		p := newParser("1 0 obj << /A 1 /B junk endobj 2 0 obj << /C 3 >> endobj")
		obj, err := p.ParseObject(context.Background())
		if err != nil {
			t.Fatalf("ParseObject: %v", err)
		}
		dict, ok := obj.(ast.PdfDict)
		if !ok {
			t.Fatalf("ParseObject = %T, want ast.PdfDict", obj)
		}
		if _, ok := dict["C"]; ok || dict["A"] != ast.PdfInt(1) {
			t.Errorf("object 1 = %v, want only /A 1", dict)
		}
		next, err := p.ParseObject(context.Background())
		if err != nil {
			t.Fatalf("ParseObject of object 2: %v", err)
		}
		if d, ok := next.(ast.PdfDict); !ok || d["C"] != ast.PdfInt(3) {
			t.Errorf("object 2 = %v, want << /C 3 >>", next)
		}
	})

	for _, tc := range []struct {
		name string
		src  string
	}{
		{"stream after a key with no value", "<< /Length 5 /X stream\nHello\nendstream"},
		{"stream after a malformed value", "<< /Length 5 /X junk stream\nHello\nendstream"},
		{"stream with the dictionary unclosed", "<< /Length 5 stream\nHello\nendstream"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream, ok := parseOne(t, tc.src).(ast.PdfStream)
			if !ok {
				t.Fatalf("ParseObject(%q) did not return a stream", tc.src)
			}
			if got := stream.Dict["Length"]; got != ast.PdfInt(5) {
				t.Errorf("/Length = %v, want 5", got)
			}
			data, err := stream.Bytes(context.Background())
			if err != nil {
				t.Fatalf("stream data: %v", err)
			}
			if string(data) != "Hello" {
				t.Errorf("stream data = %q, want %q", data, "Hello")
			}
		})
	}
}
