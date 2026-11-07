package compose_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/typography/embed"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// markFace builds a face with the letters a, b and c, a combining acute
// accent (U+0301, a nonspacing mark) with no advance, and a glyph no cmap
// entry reaches, as a shaper's contextual form of the accent is. It returns
// the face and the glyph of each, the unmapped one under "alt".
func markFace(t *testing.T) (*compose.Face, map[string]uint16) {
	t.Helper()
	glyphs := []fontbuild.Glyph{
		{Name: "a", Unicode: 'a', Advance: 500},
		{Name: "b", Unicode: 'b', Advance: 500},
		{Name: "c", Unicode: 'c', Advance: 500},
		{Name: "space", Unicode: ' ', Advance: 250, Empty: true},
		{Name: "acutecomb", Unicode: 0x0301},
		{Name: "acutecomb.alt"},
	}
	prog := fontbuild.TrueType{PostScriptName: "ComposeMarks", Family: "ComposeMarks", Subfamily: "Regular", Glyphs: glyphs, NotdefAdvance: 500}.Build()
	face := &compose.Face{Program: prog, Key: "ComposeMarks"}
	f, err := embed.Load(prog)
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

func shapedRun(face *compose.Face, gids map[string]uint16, gs []shaped) *compose.Text {
	out := &compose.Text{Face: face, Size: 10, Color: compose.Black, Origin: compose.Point{X: 20, Y: 40}}
	x := 0.0
	for _, g := range gs {
		out.Glyphs = append(out.Glyphs, compose.Glyph{GID: gids[g.glyph], X: x, Text: g.text})
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
			doc := &compose.Document{Pages: []*compose.Page{{Width: 200, Height: 100, Content: []compose.Fragment{shapedRun(face, gids, tt.glyphs)}}}}
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
