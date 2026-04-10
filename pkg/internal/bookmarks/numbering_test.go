package bookmarks

import (
	"testing"
)

func TestParseNumbering_Decimal(t *testing.T) {
	tests := []struct {
		in    string
		depth int
		title string
	}{
		{"1 Introduction", 1, "Introduction"},
		{"1.1 Overview", 2, "Overview"},
		{"1.2.3 Subsection", 3, "Subsection"},
		{"10.5.2.1 Deep Section", 4, "Deep Section"},
	}
	for _, tc := range tests {
		got := parseNumbering(tc.in)
		if got == nil {
			t.Errorf("parseNumbering(%q) = nil, want match", tc.in)
			continue
		}
		if got.Depth != tc.depth {
			t.Errorf("parseNumbering(%q).Depth = %d, want %d", tc.in, got.Depth, tc.depth)
		}
		if got.Title != tc.title {
			t.Errorf("parseNumbering(%q).Title = %q, want %q", tc.in, got.Title, tc.title)
		}
	}
}

func TestParseNumbering_NonHeading(t *testing.T) {
	// "1.5 kg" should not match (next token is non-alphabetic).
	if got := parseNumbering("1.5 kg"); got != nil {
		t.Errorf("parseNumbering(\"1.5 kg\") = %+v, want nil", got)
	}
	if got := parseNumbering("Plain text without numbering"); got != nil {
		t.Errorf("parseNumbering for plain text should be nil, got %+v", got)
	}
}

func TestParseNumbering_Roman(t *testing.T) {
	got := parseNumbering("IV. Conclusions")
	if got == nil {
		t.Fatal("parseNumbering(\"IV. Conclusions\") = nil")
	}
	if got.Depth != 1 {
		t.Errorf("Roman depth: got %d, want 1", got.Depth)
	}
	if got.Title != "Conclusions" {
		t.Errorf("title: got %q, want %q", got.Title, "Conclusions")
	}
}

func TestParseNumbering_Section(t *testing.T) {
	got := parseNumbering("§2.3 Memory model")
	if got == nil {
		t.Fatal("section symbol should parse")
	}
	if got.Depth != 2 {
		t.Errorf("section depth: got %d, want 2", got.Depth)
	}
}

func TestParseNumbering_Chapter(t *testing.T) {
	got := parseNumbering("Chapter 5: The Beginning")
	if got == nil {
		t.Fatal("chapter should parse")
	}
	if got.Title != "The Beginning" {
		t.Errorf("title: got %q, want %q", got.Title, "The Beginning")
	}
}

func TestLooksLikeTOCEntry(t *testing.T) {
	tocLines := []string{
		"Introduction ......... 5",
		"Chapter 1 ............ 12",
		"1.1 Overview . . . . . . . 18",
	}
	for _, l := range tocLines {
		if !looksLikeTOCEntry(l) {
			t.Errorf("expected TOC: %q", l)
		}
	}

	nonTOC := []string{
		"Introduction",
		"Chapter 1",
		"1.1 Overview",
	}
	for _, l := range nonTOC {
		if looksLikeTOCEntry(l) {
			t.Errorf("not a TOC entry: %q", l)
		}
	}
}
