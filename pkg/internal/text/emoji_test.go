package text

import (
	"bytes"
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
		got := emojiSequences([]rune(c.text), func(int) EmojiVariant { return c.v })
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
// picks its face (CSS Fonts 4, 6.12, and UTS #51, 4): a character drawn
// as emoji by default takes a face of the family list that covers it,
// else a colour face before any other installed face; VS16 and
// font-variant-emoji: emoji skip the faces of the list that are not
// colour faces; VS15 and font-variant-emoji: text skip the colour faces
// while another face covers the character.
func TestLayout_EmojiPresentationPicksTheFace(t *testing.T) {
	f := emojiFonts(t)
	cases := []struct {
		name     string
		families []string
		v        EmojiVariant
		text     string
		r        rune
		want     string
	}{
		{"default emoji, fallback", []string{"Test Sans"}, EmojiNormal, "A⌚", 0x231A, "colour.ttf"},
		{"text by default, fallback", []string{"Test Sans"}, EmojiNormal, "A☺", 0x263A, "mono.ttf"},
		{"VS16", []string{"Test Sans"}, EmojiNormal, "A☺️", 0x263A, "colour.ttf"},
		{"default emoji, named face", []string{"Test Sans", "Test Mono Emoji"}, EmojiNormal, "A⌚", 0x231A, "mono.ttf"},
		{"VS16 skips the named text face", []string{"Test Sans", "Test Mono Emoji"}, EmojiNormal, "A⌚️", 0x231A, "colour.ttf"},
		{"VS15 skips the named colour face", []string{"Test Colour Emoji", "Test Mono Emoji"}, EmojiNormal, "⌚︎", 0x231A, "mono.ttf"},
		{"font-variant-emoji: text", []string{"Test Sans"}, EmojiText, "A⌚", 0x231A, "mono.ttf"},
		{"font-variant-emoji: emoji", []string{"Test Sans", "Test Mono Emoji"}, EmojiEmoji, "A☺", 0x263A, "colour.ttf"},
		{"text asked, only a colour face covers", []string{"Test Sans"}, EmojiText, "A⚽", 0x26BD, "colour.ttf"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := Style{Families: c.families, Size: 10}
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
