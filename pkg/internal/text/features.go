package text

import (
	"math"
	"slices"

	"golang.org/x/image/math/fixed"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
)

// HasSubstitution reports whether the face style matches first ([Fonts.Match])
// substitutes glyphs by the OpenType feature tag in the script of r: the
// GSUB script table of that script, or of the default script when the font
// has none for it, lists the feature in its default language system. This
// is the test Chromium decides font-variant-caps synthesis by
// (OpenTypeCapsSupport::SupportsOpenTypeFeature, after HarfBuzz's
// hb_ot_layout_table_select_script). A face that does not load has no
// feature.
func (f *Fonts) HasSubstitution(style Style, r rune, tag string) bool {
	want, ok := featureTag(tag)
	if !ok {
		return false
	}
	fc := f.Match(style)
	if fc == nil {
		return false
	}
	ft, err := f.loadFont(fc)
	if err != nil {
		return false
	}
	return gsubHasFeature(&ft.GSUB, language.LookupScript(r), want)
}

// The script tags HarfBuzz falls back to when a font has no script table
// for the text's script (hb_ot_layout_table_select_script).
var (
	tagDFLT = opentype.MustNewTag("DFLT")
	tagDflt = opentype.MustNewTag("dflt")
	tagLatn = opentype.MustNewTag("latn")
)

// gsubHasFeature reports whether gsub's script table for script, or the
// fallback one, lists tag in its default language system.
func gsubHasFeature(gsub *font.GSUB, script language.Script, tag opentype.Tag) bool {
	if len(gsub.Lookups) == 0 {
		return false
	}
	idx := -1
	for _, s := range []opentype.Tag{scriptTag(script), tagDFLT, tagDflt, tagLatn} {
		if idx = gsub.FindScript(s); idx >= 0 {
			break
		}
	}
	if idx < 0 {
		return false
	}
	ls := gsub.Scripts[idx].DefaultLangSys
	if ls == nil {
		return false
	}
	for _, fi := range ls.FeatureIndices {
		if int(fi) < len(gsub.Features) && gsub.Features[fi].Tag == tag {
			return true
		}
	}
	return false
}

// scriptTag returns the OpenType script tag of script: its ISO 15924 code
// with a lowercase first letter, HarfBuzz's old tags, with the exceptions
// the OpenType script tag registry names (hb_ot_old_tag_from_script).
// Common and inherited text takes Latin's.
func scriptTag(script language.Script) opentype.Tag {
	switch script {
	case 0, language.Common, language.Inherited, language.Unknown:
		return tagLatn
	case language.Hiragana:
		return opentype.MustNewTag("kana")
	case language.Lao:
		return opentype.MustNewTag("lao ")
	case language.Yi:
		return opentype.MustNewTag("yi  ")
	case language.Nko:
		return opentype.MustNewTag("nko ")
	case language.Vai:
		return opentype.MustNewTag("vai ")
	}
	return opentype.Tag(script | 0x20000000)
}

// faceInput applies the @font-face rule of in's face to in: its features
// go before those of its style, so that the style's override them (CSS
// Fonts 4, 7.2), and its size adjustment scales the size (CSS Fonts 5,
// 4.12).
func (st *layoutState) faceInput(in *shaping.Input) {
	if in.Face == nil {
		return
	}
	fc := st.byFont[in.Face.Font]
	if fc == nil || fc.doc == nil {
		return
	}
	if len(fc.features) > 0 {
		in.FontFeatures = append(slices.Clone(fc.features), in.FontFeatures...)
	}
	if k := fc.doc.sizeAdjust(); k != 1 {
		in.Size = fixed.Int26_6(math.Round(float64(in.Size) * k))
	}
}
