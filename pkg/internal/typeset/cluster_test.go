package typeset_test

import (
	"bytes"
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/models"
)

// markFace builds a face with the letters a, b and c, a combining acute
// accent (U+0301, a nonspacing mark) with no advance, and a glyph no cmap
// entry reaches, as a shaper's contextual form of the accent is. It returns
// the face and the glyph of each, the unmapped one under "alt".
func markFace(t *testing.T) (*typeset.Face, map[string]uint16) {
	t.Helper()
	glyphs := []fontbuild.Glyph{
		{Name: "a", Unicode: 'a', Advance: 500},
		{Name: "b", Unicode: 'b', Advance: 500},
		{Name: "c", Unicode: 'c', Advance: 500},
		{Name: "space", Unicode: ' ', Advance: 250, Empty: true},
		{Name: "acutecomb", Unicode: 0x0301},
		{Name: "acutecomb.alt"},
	}
	prog := fontbuild.TrueType{PostScriptName: "TypesetMarks", Family: "TypesetMarks", Subfamily: "Regular", Glyphs: glyphs, NotdefAdvance: 500}.Build()
	face := &typeset.Face{Program: prog, Key: "TypesetMarks"}
	f, err := embed.Load(t.Context(), prog)
	if err != nil {
		t.Fatalf("embed.Load: %v", err)
	}
	gids := map[string]uint16{}
	for _, r := range "abc ́" {
		gid, ok := f.GID(r)
		if !ok {
			t.Fatalf("face lacks %q", r)
		}
		gids[string(r)] = gid
	}
	// The unmapped glyph follows the mapped accent in the program.
	gids["alt"] = gids["́"] + 1
	return face, gids
}

// shaped is one glyph of a hand-shaped run: its glyph name in markFace,
// its advance in points at size 10, and its Glyph.Text.
type shaped struct {
	glyph   string
	advance float64
	text    string
}

func shapedRun(face *typeset.Face, gids map[string]uint16, gs []shaped) *typeset.Text {
	out := &typeset.Text{Face: face, Size: 10, Color: typeset.Black, Origin: typeset.Point{X: 20, Y: 40}}
	x := 0.0
	for _, g := range gs {
		out.Glyphs = append(out.Glyphs, typeset.Glyph{GID: gids[g.glyph], X: x, Text: g.text})
		x += g.advance
	}
	return out
}

// TestWriteClusterSpreadsItsText locks how the backend writes a cluster of
// several glyphs: each glyph's code maps to the characters of the cluster
// the font's cmap gives it, so a reader that gives a glyph holding a
// nonspacing mark no advance still reads the word whole; a cluster whose
// glyphs do not spell its text in paint order is wrapped in an ActualText
// span (ISO 32000-1, 14.9.4).
func TestWriteClusterSpreadsItsText(t *testing.T) {
	t.Parallel()
	face, gids := markFace(t)
	tests := []struct {
		name   string
		glyphs []shaped
		// want is what the text extractor reads; actual the ActualText
		// spans the content stream holds, in order.
		want   string
		actual []string
	}{
		{
			name: "base and mark in order",
			glyphs: []shaped{
				{"b", 5, "b"}, {"a", 5, "á"}, {"́", 0, ""}, {"c", 5, "c"},
			},
			want: "bác",
		},
		{
			name: "marked letters after a word space",
			glyphs: []shaped{
				{"b", 5, "b"}, {" ", 2.5, " "}, {"a", 5, "a\u0301"}, {"\u0301", 0, ""}, {"c", 5, "c\u0301"}, {"\u0301", 0, ""}, {"a", 5, "a"},
			},
			want: "b a\u0301c\u0301a",
		},
		{
			name: "contextual form takes the character left over",
			glyphs: []shaped{
				{"b", 5, "b"}, {"a", 5, "á"}, {"alt", 0, ""}, {"c", 5, "c"},
			},
			want: "bác",
		},
		{
			name: "glyphs drawn out of text order",
			glyphs: []shaped{
				{"a", 5, "a"}, {"c", 5, "bc"}, {"b", 5, ""}, {"a", 5, "a"},
			},
			// The extractor reads the span's ActualText, the glyphs'
			// characters in text order.
			want:   "abca",
			actual: []string{"bc"},
		},
		{
			name: "no glyph the cmap maps",
			glyphs: []shaped{
				{"a", 5, "a"}, {"alt", 0, "xy"}, {"alt", 0, ""}, {"a", 5, "a"},
			},
			want: "axya",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := &typeset.Document{Pages: []*typeset.Page{{Width: 200, Height: 100, Content: []typeset.Fragment{shapedRun(face, gids, tt.glyphs)}}}}
			out := write(t, doc)
			text, err := open(t, out).ExtractText(context.Background(), 1)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if got := strings.TrimSpace(text); got != tt.want {
				t.Errorf("ExtractText = %q, want %q", got, tt.want)
			}
			content := inflated(t, out)
			var got []string
			for _, chunk := range bytes.Split(content, []byte("/Span <</ActualText <FEFF"))[1:] {
				hex, _, _ := bytes.Cut(chunk, []byte(">"))
				got = append(got, utf16Hex(t, string(hex)))
			}
			if strings.Join(got, "|") != strings.Join(tt.actual, "|") {
				t.Errorf("ActualText spans = %q, want %q", got, tt.actual)
			}
			if bytes.Count(content, []byte(" BDC\n")) != bytes.Count(content, []byte("EMC\n")) {
				t.Errorf("marked content unbalanced:\n%s", content)
			}
		})
	}
}

// TestWriteClusterMapsEveryGlyph locks that every glyph a run draws maps to
// text in the ToUnicode CMap, as ISO 19005-2, 6.2.11.7.2, ISO 19005-1,
// 6.3.8 and ISO 14289-1, 7.21.7 ask of every rendered code, an ActualText
// span covering it or not: a glyph left without a character of its cluster
// maps to the character the font's cmap maps to it, or to U+FFFD when the
// cmap maps none, and a character drawn in parts maps to its
// decomposition's characters. The cluster's text stays whole in the
// ActualText span around its glyphs (ISO 32000-1, 14.9.4).
//
// # Corpus evidence
//
//   - litho/html, litho/odt th-line-break, litho/odp indic-thai-slides,
//     litho/ods indic-thai-cells: Noto Sans Thai draws SARA AM (U+0E33) as
//     NIKHAHIT and SARA AA; the SARA AA glyph had no ToUnicode entry, six
//     rendered codes on the HTML page failing PDF/A-2u, 6.2.11.7.2 t1.
//     Chromium's and LibreOffice's references of th-line-break map the two
//     glyphs to U+0E4D and U+0E32 inside an ActualText span of the
//     cluster, as litho does.
//   - probe, html: Noto Sans Tamil and Khmer draw a dotted circle under a
//     mark with no base, Noto Sans Khmer draws U+17BE as two glyphs, Noto
//     Sans Bengali U+09CB and Noto Sans Lao U+0EB3 as two; each second
//     glyph had no entry. veraPDF passes PDF/A-1a, PDF/A-2u and PDF/UA-2 on
//     the output, its U+25CC entries included.
func TestWriteClusterMapsEveryGlyph(t *testing.T) {
	t.Parallel()
	face, gids := markFace(t)
	marks := "a" + strings.Repeat("\u0301", 70)
	long := []shaped{{"a", 5, marks}}
	for range 70 {
		long = append(long, shaped{"\u0301", 0, ""})
	}
	tests := []struct {
		name   string
		glyphs []shaped
		// mapped are the texts the ToUnicode CMap holds, actual the
		// ActualText spans the content stream holds, in order.
		mapped []string
		actual []string
	}{
		{
			name:   "mark the shaper adds",
			glyphs: []shaped{{"b", 5, "b"}, {"\u0301", 0, ""}, {"c", 5, "c"}},
			mapped: []string{"b", "\u0301", "c"},
			actual: []string{"b"},
		},
		{
			name:   "glyph no cmap entry reaches",
			glyphs: []shaped{{"b", 5, "b"}, {"alt", 0, ""}, {"c", 5, "c"}},
			mapped: []string{"b", "\uFFFD", "c"},
			actual: []string{"b"},
		},
		{
			name:   "precomposed letter drawn in parts",
			glyphs: []shaped{{"b", 5, "b"}, {"a", 5, "\u00E1"}, {"\u0301", 0, ""}},
			mapped: []string{"b", "a", "\u0301"},
			actual: []string{"\u00E1"},
		},
		{
			name:   "glyphs that start a run without text",
			glyphs: []shaped{{"\u0301", 0, ""}, {"a", 5, "a"}},
			mapped: []string{"\u0301", "a"},
		},
		{
			name:   "cluster past the cap",
			glyphs: long,
			mapped: []string{marks, "\u0301"},
			actual: []string{marks},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := &typeset.Document{Pages: []*typeset.Page{{Width: 200, Height: 100, Content: []typeset.Fragment{shapedRun(face, gids, tt.glyphs)}}}}
			out := write(t, doc)
			got := toUnicodeTexts(t, out)
			slices.Sort(got)
			want := slices.Clone(tt.mapped)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("ToUnicode texts = %q, want %q", got, want)
			}
			var spans []string
			for _, chunk := range bytes.Split(inflated(t, out), []byte("/Span <</ActualText <FEFF"))[1:] {
				hex, _, _ := bytes.Cut(chunk, []byte(">"))
				spans = append(spans, utf16Hex(t, string(hex)))
			}
			if !slices.Equal(spans, tt.actual) {
				t.Errorf("ActualText spans = %q, want %q", spans, tt.actual)
			}
			violations, err := open(t, out).ValidatePDFAReport(context.Background(), models.PDFA2u)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			for _, v := range violations {
				if v.Clause == "6.2.11.7.2" {
					t.Errorf("violation: %v", v)
				}
			}
		})
	}
}

// bfcharEntry matches one entry of a ToUnicode CMap's bfchar block: a code
// and the UTF-16BE text it maps to.
var bfcharEntry = regexp.MustCompile(`<([0-9A-F]{4})> <([0-9A-F]+)>`)

// toUnicodeTexts returns the texts the bfchar entries of the ToUnicode
// CMaps in out, a file whose CMap streams are not compressed, name, one per
// code.
func toUnicodeTexts(t *testing.T, out []byte) []string {
	t.Helper()
	var texts []string
	for _, block := range bytes.Split(out, []byte("beginbfchar"))[1:] {
		block, _, _ = bytes.Cut(block, []byte("endbfchar"))
		for _, m := range bfcharEntry.FindAllSubmatch(block, -1) {
			texts = append(texts, utf16Hex(t, string(m[2])))
		}
	}
	return texts
}

// utf16Hex decodes hex, UTF-16BE code units as four hex digits each.
func utf16Hex(t *testing.T, hex string) string {
	t.Helper()
	var units []uint16
	for i := 0; i+4 <= len(hex); i += 4 {
		var u uint16
		for _, c := range hex[i : i+4] {
			u <<= 4
			switch {
			case c >= '0' && c <= '9':
				u |= uint16(c - '0')
			case c >= 'A' && c <= 'F':
				u |= uint16(c-'A') + 10
			default:
				t.Fatalf("not hex: %q", hex)
			}
		}
		units = append(units, u)
	}
	var b strings.Builder
	for _, u := range units {
		b.WriteRune(rune(u))
	}
	return b.String()
}
