package html

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

func TestRootLanguage(t *testing.T) {
	tests := []struct {
		src, want string
	}{
		{`<html lang=" ja "><head><meta http-equiv="Content-Language" content="ko"></head></html>`, "ja"},
		{`<html><head><meta http-equiv="content-language" content="zh-Hans"></head></html>`, "zh-Hans"},
		{`<html><head><meta http-equiv="content-language" content="en, fr"><meta http-equiv="content-language" content="ko"></head></html>`, "ko"},
		{`<html><head><meta name="language" content="ja"></head></html>`, ""},
		{`<html lang=""><head><meta http-equiv="content-language" content="ja"></head></html>`, ""},
	}
	for _, tt := range tests {
		p := prep(t, tt.src, Options{})
		if p.root.Lang != tt.want || p.layout.Lang != tt.want {
			t.Errorf("%s: root %q, document %q, want %q", tt.src, p.root.Lang, p.layout.Lang, tt.want)
		}
	}
}

// TestFontUnits_MeasureTheFaceOfTheLanguage locks that the ch and ic units
// measure the face a generic family draws in the element's language (CSS
// Values 4, 6.1.1: the first available font; CSS Fonts 4, 2.1.5: a
// generic family may depend on the language), as the layout draws it:
// sans-serif in Japanese is Noto Sans CJK JP, whose "0" here is 0.6 em
// wide and whose "水" is 1 em, where Liberation Sans has a "0" of 0.5 em and
// no ideograph; monospace in Korean is DejaVu Sans Mono, as Chromium's
// fallback rule picks it, not the Noto Sans Mono CJK KR fontconfig would
// prefer for the language.
func TestFontUnits_MeasureTheFaceOfTheLanguage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, f := range []struct {
		file, ps, family string
		zero             uint16
		ideograph        bool
	}{
		{"sans.ttf", "LiberationSans", "Liberation Sans", 500, false},
		{"cjk.ttf", "NotoSansCJKjp-Regular", "Noto Sans CJK JP", 600, true},
		{"mono.ttf", "DejaVuSansMono", "DejaVu Sans Mono", 700, false},
		{"monokr.ttf", "NotoSansMonoCJKkr-Regular", "Noto Sans Mono CJK KR", 400, true},
	} {
		glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: 250, Empty: true}, {Name: "zero", Unicode: '0', Advance: f.zero}, {Name: "a", Unicode: 'a', Advance: 500}}
		if f.ideograph {
			glyphs = append(glyphs, fontbuild.Glyph{Name: "uni6C34", Unicode: '水', Advance: 1000})
		}
		prog := fontbuild.TrueType{PostScriptName: f.ps, Family: f.family, Subfamily: "Regular", NotdefAdvance: 1000, Glyphs: glyphs}.Build()
		if err := os.WriteFile(filepath.Join(dir, f.file), prog, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	units := fontUnits(context.Background(), fonts, false)
	for _, tt := range []struct {
		family, lang string
		zero         float64
		ideograph    bool
	}{
		{"sans-serif", "ja", 0.6, true},
		{"sans-serif", "en", 0.5, false},
		// Chromium draws monospace in Korean with DejaVu Sans Mono.
		{"monospace", "ko", 0.7, false},
	} {
		u := units(&css.Style{FontFamilies: []string{tt.family}, FontWeight: 400}, tt.lang)
		if !u.ZeroMeasured || u.Zero != tt.zero || u.IdeographMeasured != tt.ideograph {
			t.Errorf("%s in %q: %+v, want a measured 0 of %v em, ideograph measured %v", tt.family, tt.lang, u, tt.zero, tt.ideograph)
		}
	}
}
