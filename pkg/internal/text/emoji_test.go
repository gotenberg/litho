package text

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func TestEmojiSequences(t *testing.T) {
	cases := []struct {
		text string
		v    EmojiVariant
		want []emojiSeq
	}{
		{"a😀b", EmojiNormal, []emojiSeq{{1, 2, presentDefaultEmoji}}},
		{"☺", EmojiNormal, nil},
		{"☺️", EmojiNormal, []emojiSeq{{0, 2, presentEmoji}}},
		{"😀︎", EmojiNormal, []emojiSeq{{0, 2, presentText}}},
		{"1️⃣", EmojiNormal, []emojiSeq{{0, 3, presentEmoji}}},
		{"1⃣", EmojiNormal, nil},
		{"1⃣", EmojiEmoji, []emojiSeq{{0, 2, presentEmoji}}},
		{"👨‍👩‍👧", EmojiNormal, []emojiSeq{{0, 5, presentDefaultEmoji}}},
		{"🇫🇷🇯🇵", EmojiNormal, []emojiSeq{{0, 2, presentDefaultEmoji}, {2, 4, presentDefaultEmoji}}},
		{"👍🏽 ✋", EmojiNormal, []emojiSeq{{0, 2, presentDefaultEmoji}, {3, 4, presentDefaultEmoji}}},
		{"🏴\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F", EmojiNormal, []emojiSeq{{0, 7, presentDefaultEmoji}}},
		{"😀", EmojiText, []emojiSeq{{0, 1, presentText}}},
		{"☺7", EmojiEmoji, []emojiSeq{{0, 1, presentEmoji}, {1, 2, presentEmoji}}},
		{"☺😀", EmojiUnicode, []emojiSeq{{0, 1, presentText}, {1, 2, presentEmoji}}},
		{"😀︎", EmojiEmoji, []emojiSeq{{0, 2, presentText}}},
		{"abc", EmojiNormal, nil},
	}
	for _, c := range cases {
		got := emojiSequences([]rune(c.text), func(int) EmojiVariant { return c.v }, nil)
		if !slices.Equal(got, c.want) {
			t.Errorf("%q under %d: %v, want %v", c.text, c.v, got, c.want)
		}
	}
}

// emojiFonts indexes Test Sans, "Test Mono Emoji", a text face covering
// U+263A and U+231A, and "Test Colour Emoji", a colour bitmap face
// covering both and U+26BD.
func emojiFonts(t *testing.T) *Fonts {
	t.Helper()
	var png1 bytes.Buffer
	if err := png.Encode(&png1, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	mono := fontbuild.TrueType{
		PostScriptName: "TestMonoEmoji", Family: "Test Mono Emoji", Subfamily: "Regular", NotdefAdvance: 500,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: spaceAdvance, Empty: true},
			{Name: "smile", Unicode: 0x263A, Advance: 800},
			{Name: "watch", Unicode: 0x231A, Advance: 800},
		},
	}.Build()
	bitmap := fontbuild.BitmapGlyph{PNG: png1.Bytes(), Width: 4, Height: 4, Top: 4, Advance: 4}
	cblc, cbdt := fontbuild.ColorBitmaps(4, map[uint16]fontbuild.BitmapGlyph{1: bitmap, 2: bitmap, 3: bitmap})
	colour := fontbuild.TrueType{
		PostScriptName: "TestColourEmoji", Family: "Test Colour Emoji", Subfamily: "Regular", NotdefAdvance: 1000,
		Glyphs: []fontbuild.Glyph{
			{Name: "smile", Unicode: 0x263A, Advance: 1000, Empty: true},
			{Name: "watch", Unicode: 0x231A, Advance: 1000, Empty: true},
			{Name: "ball", Unicode: 0x26BD, Advance: 1000, Empty: true},
			{Name: "space", Unicode: ' ', Advance: 1000, Empty: true},
		},
		Extra: map[string][]byte{"CBLC": cblc, "CBDT": cbdt},
	}.Build()
	return newTestFonts(t, append(sansFamily(),
		fontFile{name: "mono.ttf", data: mono}, fontFile{name: "colour.ttf", data: colour})...)
}

// faceOf returns the key of the face that draws the glyph standing for
// the text starting with r.
func faceOf(t *testing.T, lines []Line, r rune) string {
	t.Helper()
	for _, l := range lines {
		for _, run := range l.Runs {
			for _, g := range run.Glyphs {
				if strings.HasPrefix(g.Text, string(r)) {
					return run.Face.Key
				}
			}
		}
	}
	t.Fatalf("no glyph stands for %q", r)
	return ""
}

// TestLayout_EmojiPresentationPicksTheFace locks how an emoji sequence
// picks its face (CSS Fonts 4, 6.12, and UTS #51, 4): under Chromium's
// rule a character drawn as emoji by default takes a face of the family
// list that covers it, else a colour face before any other installed
// face, and under the other rules a colour face right after the primary
// face's family; VS16 and font-variant-emoji: emoji skip the faces of the
// list that are not colour faces; VS15 and font-variant-emoji: text skip
// the colour faces while another face covers the character.
func TestLayout_EmojiPresentationPicksTheFace(t *testing.T) {
	f := emojiFonts(t)
	cases := []struct {
		name     string
		families []string
		v        EmojiVariant
		rule     Fallback
		text     string
		r        rune
		want     string
	}{
		{"default emoji, fallback", []string{"Test Sans"}, EmojiNormal, FallbackChromium, "A⌚", 0x231A, "colour.ttf"},
		{"text by default, fallback", []string{"Test Sans"}, EmojiNormal, FallbackChromium, "A☺", 0x263A, "mono.ttf"},
		{"VS16", []string{"Test Sans"}, EmojiNormal, FallbackChromium, "A☺️", 0x263A, "colour.ttf"},
		{"default emoji, named face", []string{"Test Sans", "Test Mono Emoji"}, EmojiNormal, FallbackChromium, "A⌚", 0x231A, "mono.ttf"},
		{"default emoji, second family under an Office rule", []string{"Test Sans", "Test Mono Emoji"}, EmojiNormal, FallbackFontconfig, "A⌚", 0x231A, "colour.ttf"},
		{"VS16 skips the named text face", []string{"Test Sans", "Test Mono Emoji"}, EmojiNormal, FallbackChromium, "A⌚️", 0x231A, "colour.ttf"},
		{"VS15 skips the named colour face", []string{"Test Colour Emoji", "Test Mono Emoji"}, EmojiNormal, FallbackChromium, "⌚︎", 0x231A, "mono.ttf"},
		{"font-variant-emoji: text", []string{"Test Sans"}, EmojiText, FallbackChromium, "A⌚", 0x231A, "mono.ttf"},
		{"font-variant-emoji: emoji", []string{"Test Sans", "Test Mono Emoji"}, EmojiEmoji, FallbackChromium, "A☺", 0x263A, "colour.ttf"},
		{"text asked, only a colour face covers", []string{"Test Sans"}, EmojiText, FallbackChromium, "A⚽", 0x26BD, "colour.ttf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Style{Families: c.families, Size: 10, Fallback: c.rule}
			if c.v != EmojiNormal {
				s.Settings = &FontSettings{Emoji: c.v}
			}
			lines, _ := layout(t, f, c.text, s, 1000, Collapse)
			if got := faceOf(t, lines, c.r); !strings.Contains(got, c.want) {
				t.Errorf("face %q, want %s", got, c.want)
			}
		})
	}
}

// TestLayout_OfficeTextEmojiIgnoresTheSelector checks that an Office run
// asking for text presentation draws an emoji followed by VS16 in a text
// face, as Word draws the emoji of a coloured run, where any other run,
// a web page's under font-variant-emoji: text among them, lets the
// selector pick the colour face (CSS Fonts 4, 6.12).
func TestLayout_OfficeTextEmojiIgnoresTheSelector(t *testing.T) {
	f := emojiFonts(t)
	for _, c := range []struct {
		name   string
		office bool
		want   string
	}{
		{"office", true, "mono.ttf"},
		{"web", false, "colour.ttf"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := Style{Families: []string{"Test Sans"}, Size: 10, Fallback: FallbackFontconfig, OfficeEmoji: c.office, Settings: &FontSettings{Emoji: EmojiText}}
			p := shape(t, f, []ParagraphRun{{Text: "A\u263A\uFE0F", Style: s}}, ParagraphOptions{})
			var got string
			for _, sp := range p.Line(0, 3, false).Spans {
				if sp.Start == 1 && sp.Text != nil {
					got = sp.Text.Face.Key
				}
			}
			if !strings.Contains(got, c.want) {
				t.Errorf("face %q, want %s", got, c.want)
			}
		})
	}
}

// TestLayout_FiguresAsTextSplitAsPlainText locks that a line of figures
// under font-variant-emoji: text, as every cell of an Excel sheet is laid
// out, costs what it costs without the property: the digits, # and * are
// Emoji characters asked to draw as text, which the face of the line
// already does ([layoutState.plainText]), so none takes an input of its
// own: no sequence is recorded for them, the lines are equal and the
// layout allocates no more. The allocations are not compared under the
// race detector, where sync.Pool drops a share of what it is given at
// random and two runs of one layout allocate differently.
func TestLayout_FiguresAsTextSplitAsPlainText(t *testing.T) {
	f := newTestFonts(t, fontFile{name: "serif.ttf", data: familyFace("Liberation Serif", "Regular", 400, false, '0', '1', '2', '3', '4', '5', '6', '7', '8', '9', '.', '#', '*')})
	const figures = "1234567.89 #5*0 31.4159 2718.28 1024"
	plain := Style{Families: []string{"Liberation Serif"}, Size: 10}
	asText := plain
	asText.Settings = &FontSettings{Emoji: EmojiText}
	text := func(int) EmojiVariant { return EmojiText }
	if seqs := emojiSequences([]rune(figures), text, func(int) bool { return true }); len(seqs) != 0 {
		t.Errorf("%d sequences for figures their face draws as text, want none", len(seqs))
	}
	if seqs := emojiSequences([]rune(figures), text, nil); len(seqs) == 0 {
		t.Error("no sequence for figures asked as text with no face to ask: the case is not exercised")
	}
	want, _ := layout(t, f, figures, plain, 1000, Collapse)
	got, _ := layout(t, f, figures, asText, 1000, Collapse)
	if len(got) != len(want) || got[0].Width != want[0].Width || len(got[0].Runs) != len(want[0].Runs) {
		t.Fatalf("as text: %d lines, width %v, %d runs; plain: %d lines, width %v, %d runs",
			len(got), got[0].Width, len(got[0].Runs), len(want), want[0].Width, len(want[0].Runs))
	}
	if raceEnabled {
		return
	}
	allocs := func(s Style) float64 {
		return testing.AllocsPerRun(50, func() {
			if _, _, err := f.Layout(context.Background(), []Run{{Text: figures, Style: s}}, 1000, Options{Whitespace: Collapse}); err != nil {
				t.Fatal(err)
			}
		})
	}
	// A sequence a digit costs some fifty allocations on this line; two
	// leave room for a collection that empties a pool during one batch.
	if base, text := allocs(plain), allocs(asText); text > base+2 {
		t.Errorf("figures as text allocate %v a layout, plain %v", text, base)
	}
}
