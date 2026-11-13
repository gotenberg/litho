package text

import (
	"math"
	"slices"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/gotenberg/litho/pkg/internal/numeric"
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
	switch script { //nolint:exhaustive // every other script takes its ISO 15924 code below
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

// faceInput applies the face of in to in, shaped in style: the features
// of its @font-face rule go before those of the style, so that the
// style's override them (CSS Fonts 4, 7.2), and [layoutState.faceScale]
// scales the size.
func (st *layoutState) faceInput(in *shaping.Input, style *Style) {
	if in.Face == nil {
		return
	}
	fc := st.byFace[in.Face]
	if fc == nil {
		return
	}
	if len(fc.features) > 0 {
		in.FontFeatures = append(slices.Clone(fc.features), in.FontFeatures...)
	}
	if k := faceScale(style, fc, in.Face); k != 1 {
		in.Size = fixed.Int26_6(math.Round(float64(in.Size) * k))
	}
}

// faceScale returns the factor the face fc, parsed as ft, draws text of
// style at: its rule's size-adjust (CSS Fonts 5, 4.12) times the
// font-size-adjust of style (CSS Fonts 5, 2.6).
func faceScale(style *Style, fc *Face, ft *font.Face) float64 {
	return fc.doc.sizeAdjust() * adjustFactor(style, fc, ft)
}

// AdjustMetric is the font metric CSS font-size-adjust holds constant
// (CSS Fonts 5, 2.6).
type AdjustMetric uint8

// The font-size-adjust metrics.
const (
	AdjustExHeight AdjustMetric = iota
	AdjustCapHeight
	AdjustChWidth
	AdjustIcWidth
	AdjustIcHeight
)

// SizeAdjust is CSS font-size-adjust (CSS Fonts 5, 2.6): each face draws
// at the size that gives its Metric, as a fraction of the font size, the
// value Value. A Value of 0 or less adjusts nothing, unless Zero is set.
type SizeAdjust struct {
	Metric AdjustMetric
	Value  float64
	// Zero draws the text at size 0: a value of 0 gives every face a
	// used size of 0 (CSS Fonts 5, 2.6).
	Zero bool
}

// maxAdjustFactor bounds the factor font-size-adjust scales a face by.
// Overflow guard, not spec-mandated: a face whose x-height is a hundredth
// of its em would otherwise draw a hundred times larger than asked.
const maxAdjustFactor = 10

// adjustFactor returns the factor style's font-size-adjust scales the
// face fc, parsed as ft, by, 1 for none: Value over the face's aspect
// value.
func adjustFactor(style *Style, fc *Face, ft *font.Face) float64 {
	sa := style.settings().SizeAdjust
	if sa.Zero {
		return 0
	}
	v := sa.Value
	if !(v > 0) {
		return 1
	}
	aspect := faceAspect(fc, ft, sa.Metric)
	if !(aspect > 0) || !numeric.Finite(aspect) {
		return 1
	}
	return min(max(v/aspect, 1/float64(maxAdjustFactor)), maxAdjustFactor)
}

// faceAspect returns the metric of the face fc, parsed as ft, as a
// fraction of its em: its aspect value (CSS Fonts 5, 2.6). A face that
// records no value for the metric takes the fallback CSS Values 4, 6.1.1
// gives the font-relative units; 0 for no face.
func faceAspect(fc *Face, ft *font.Face, metric AdjustMetric) float64 {
	if fc == nil || fc.upem <= 0 {
		return 0
	}
	switch metric {
	case AdjustExHeight:
	case AdjustCapHeight:
		if fc.capHeight > 0 {
			return fc.capHeight / fc.upem
		}
		return fc.ascent / fc.upem
	case AdjustChWidth:
		return advanceOf(ft, '0', fc.upem, 0.5)
	case AdjustIcWidth, AdjustIcHeight:
		return advanceOf(ft, '水', fc.upem, 1)
	}
	if fc.xHeight > 0 {
		return fc.xHeight / fc.upem
	}
	return 0.5
}

// Aspect returns the aspect value of metric of the face style matches
// first ([Fonts.Match]), the value CSS font-size-adjust: from-font takes
// (CSS Fonts 5, 2.6); 0 when no face matches.
func (f *Fonts) Aspect(style Style, metric AdjustMetric) float64 {
	fc := f.Match(style)
	if fc == nil {
		return 0
	}
	return faceAspect(fc, fc.parsedFace(), metric)
}

// advanceOf returns the advance of r in ft as a fraction of the em,
// fallback when ft has no glyph for it.
func advanceOf(ft *font.Face, r rune, upem, fallback float64) float64 {
	if ft == nil {
		return fallback
	}
	gid, ok := ft.NominalGlyph(r)
	if !ok {
		return fallback
	}
	return float64(ft.HorizontalAdvance(gid)) / upem
}

// parsedFace returns the parsed font of fc for its metrics, nil when it
// does not load.
func (fc *Face) parsedFace() *font.Face {
	ft := fc.pinned
	if ft == nil && fc.idx != nil {
		var err error
		if ft, err = fc.idx.loadFont(fc); err != nil {
			return nil
		}
	}
	if ft == nil {
		return nil
	}
	return font.NewFace(ft)
}
