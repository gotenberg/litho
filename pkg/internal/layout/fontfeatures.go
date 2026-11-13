package layout

import (
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// fontFeatures returns the OpenType features the font properties of st
// turn on and off, in the order the CSS feature resolution applies them,
// a later setting of a tag winning (CSS Fonts 4, 7.2): the alternates
// @font-feature-values resolves, the ligatures, the East Asian forms,
// the numeric forms, font-feature-settings, then the position. ts is
// the text style st maps to without them, whose face resolves the
// alternates. font-variant-caps is the business of [smallCaps]; kerning
// is [text.Style.NoKerning]. nil when every property is normal.
//
// The order follows Chromium's (FontFeatureRange::FromFontDescription),
// which departs from CSS Fonts 4, 7.2 in one place: it applies the
// subs and sups features of font-variant-position after
// font-feature-settings, so that the position wins over a setting of the
// same feature. With BrowserRules off, letter spacing keeps the
// ligatures.
func (e *engine) fontFeatures(st *css.Style, ts *text.Style) []text.Feature {
	alt := &st.FontVariantAlternates
	lig := st.FontVariantLigatures
	spaced := e.opts.BrowserRules && ts.LetterSpacing != 0
	if alt.IsNormal() && lig == 0 && st.FontVariantEastAsian == 0 && st.FontVariantNumeric == 0 &&
		len(st.FontFeatureSettings) == 0 && st.FontVariantPosition == css.PositionNormal && !spaced {
		return nil
	}
	var out []text.Feature
	add := func(tag string, v int) { out = append(out, text.Feature{Tag: tag, Value: uint32(max(v, 0))}) } //nolint:gosec // v is at most css.maxFeatureValue, below 2^31
	if !alt.IsNormal() {
		var values *css.FontFeatureValues
		if e.opts.FeatureValues != nil {
			if fc := e.fonts.Match(*ts); fc != nil {
				values = e.opts.FeatureValues(fc.DeclaredFamily())
			}
		}
		for _, f := range alt.Resolve(values) {
			add(f.Tag, f.Value)
		}
	}
	// Letter spacing turns off the optional ligatures (CSS Text 3, 8.2;
	// CSS Fonts 4, 7.2), and Chromium the contextual alternates with them.
	if spaced || lig&css.LigaturesCommonOff != 0 {
		add("liga", 0)
		add("clig", 0)
	}
	if !spaced && lig&css.LigaturesDiscretionary != 0 {
		add("dlig", 1)
	}
	if !spaced && lig&css.LigaturesHistorical != 0 {
		add("hlig", 1)
	}
	if spaced || lig&css.LigaturesContextualOff != 0 {
		add("calt", 0)
	}
	out = appendEastAsian(out, st.FontVariantEastAsian)
	out = appendNumeric(out, st.FontVariantNumeric)
	for _, f := range st.FontFeatureSettings {
		add(f.Tag, f.Value)
	}
	switch st.FontVariantPosition {
	case css.PositionSub:
		add("subs", 1)
	case css.PositionSuper:
		add("sups", 1)
	}
	return out
}

// eastAsianForms are the features of the font-variant-east-asian forms
// (CSS Fonts 4, 6.10), by form.
var eastAsianForms = [...]string{
	css.EastAsianJIS78: "jp78", css.EastAsianJIS83: "jp83", css.EastAsianJIS90: "jp90",
	css.EastAsianJIS04: "jp04", css.EastAsianSimplified: "smpl", css.EastAsianTraditional: "trad",
}

// appendEastAsian appends the features of font-variant-east-asian v.
func appendEastAsian(out []text.Feature, v css.EastAsianVariant) []text.Feature {
	if form := v & css.EastAsianFormMask; form != 0 && int(form) < len(eastAsianForms) {
		out = append(out, text.Feature{Tag: eastAsianForms[form], Value: 1})
	}
	switch {
	case v&css.EastAsianFullWidth != 0:
		out = append(out, text.Feature{Tag: "fwid", Value: 1})
	case v&css.EastAsianProportionalWidth != 0:
		out = append(out, text.Feature{Tag: "pwid", Value: 1})
	}
	if v&css.EastAsianRuby != 0 {
		out = append(out, text.Feature{Tag: "ruby", Value: 1})
	}
	return out
}

// numericFeatures are the features of the font-variant-numeric bits (CSS
// Fonts 4, 6.7), in Chromium's order.
var numericFeatures = []struct {
	bit css.NumericVariant
	tag string
}{
	{css.NumericLining, "lnum"}, {css.NumericOldstyle, "onum"},
	{css.NumericProportional, "pnum"}, {css.NumericTabular, "tnum"},
	{css.NumericStackedFractions, "afrc"}, {css.NumericDiagonalFractions, "frac"},
	{css.NumericOrdinal, "ordn"}, {css.NumericSlashedZero, "zero"},
}

// appendNumeric appends the features of font-variant-numeric v.
func appendNumeric(out []text.Feature, v css.NumericVariant) []text.Feature {
	for _, n := range numericFeatures {
		if v&n.bit != 0 {
			out = append(out, text.Feature{Tag: n.tag, Value: 1})
		}
	}
	return out
}

// capsSupport is how a face supports the font-variant-caps value asked for
// (Chromium's OpenTypeCapsSupport::FontSupport).
type capsSupport uint8

const (
	// capsFull: the face has the features of the value.
	capsFull capsSupport = iota
	// capsFallback: the face lacks them but has the small capitals the
	// value falls back to.
	capsFallback
	// capsNone: the face has neither; small capitals are synthesised.
	capsNone
)

// capsSynthesis says which characters synthesised small capitals draw
// (Chromium's OpenTypeCapsSupport::CapsSynthesis).
type capsSynthesis uint8

const (
	synthNone capsSynthesis = iota
	// synthLower draws the lowercase letters as small capitals.
	synthLower
	// synthUpper draws the uppercase letters, and every other character
	// that does not change case, as small capitals.
	synthUpper
	// synthBoth draws every character as a small capital.
	synthBoth
)

// capsPlan is how a run of one font-variant-caps value is drawn in a face.
type capsPlan struct {
	caps    css.FontVariant
	support capsSupport
	synth   capsSynthesis
	// allowed is font-synthesis-small-caps: auto.
	allowed bool
}

// planCaps decides how the caps value of st draws in the face of ts, by
// the features the face has for the script of sample, as Chromium decides
// (OpenTypeCapsSupport::DetermineFontSupport): small-caps needs smcp,
// all-small-caps smcp and c2sc, petite-caps pcap or falls back to smcp,
// all-petite-caps pcap and c2pc or falls back to all-small-caps, unicase
// unic or falls back to smcp of the lowercased capitals, titling-caps
// titl or draws as normal. A value the face cannot draw synthesises
// small capitals (CSS Fonts 4, 6.6), unless font-synthesis-small-caps is
// none. With BrowserRules off, small capitals are always synthesised,
// as the Office formats draw them.
func (e *engine) planCaps(st *css.Style, ts text.Style, sample rune) capsPlan {
	p := capsPlan{caps: st.FontVariant, allowed: !st.NoSynthesizeSmallCaps}
	if p.caps == css.VariantNormal {
		return p
	}
	if !e.opts.BrowserRules {
		p.allowed = true
		p.caps = css.SmallCaps
		p.support, p.synth = capsNone, synthLower
		if st.FontVariant == css.AllSmallCaps {
			p.synth = synthBoth
		}
		return p
	}
	has := func(tag string) bool { return e.fonts.HasSubstitution(ts, sample, tag) }
	switch p.caps { //nolint:exhaustive // normal returned above
	case css.SmallCaps:
		if !has("smcp") {
			p.support, p.synth = capsNone, synthLower
		}
	case css.AllSmallCaps:
		if !has("smcp") || !has("c2sc") {
			p.support, p.synth = capsNone, synthBoth
		}
	case css.PetiteCaps:
		if !has("pcap") {
			p.support, p.synth = capsNone, synthLower
			if has("smcp") {
				p.support = capsFallback
			}
		}
	case css.AllPetiteCaps:
		if !has("pcap") || !has("c2pc") {
			p.support, p.synth = capsNone, synthBoth
			if has("smcp") && has("c2sc") {
				p.support = capsFallback
			}
		}
	case css.Unicase:
		if !has("unic") {
			p.support, p.synth = capsNone, synthUpper
			if has("smcp") {
				p.support = capsFallback
			}
		}
	case css.TitlingCaps:
		if !has("titl") {
			p.support = capsNone
		}
	}
	return p
}

// splits reports whether the run is cut where the case of its characters
// changes (NeedsRunCaseSplitting).
func (p capsPlan) splits() bool {
	return p.support != capsFull && p.caps != css.TitlingCaps && p.allowed
}

// features returns the caps features of a piece whose characters need
// uppercasing or not (FontFeatureToUse).
func (p capsPlan) features(upper bool) []text.Feature {
	c := p.caps
	switch {
	case p.support == capsFull:
	case p.support == capsFallback && c == css.AllPetiteCaps:
		c = css.AllSmallCaps
	case p.support == capsFallback && (c == css.PetiteCaps || (c == css.Unicase && !upper)):
		c = css.SmallCaps
	default:
		return nil
	}
	on := func(tags ...string) []text.Feature {
		out := make([]text.Feature, len(tags))
		for i, t := range tags {
			out[i] = text.Feature{Tag: t, Value: 1}
		}
		return out
	}
	switch c { //nolint:exhaustive // normal has no feature
	case css.SmallCaps:
		return on("smcp")
	case css.AllSmallCaps:
		return on("c2sc", "smcp")
	case css.PetiteCaps:
		return on("pcap")
	case css.AllPetiteCaps:
		return on("c2pc", "pcap")
	case css.Unicase:
		return on("unic")
	case css.TitlingCaps:
		return on("titl")
	}
	return nil
}

// synthetic reports whether a piece is drawn at the size of synthesised
// small capitals (NeedsSyntheticFont).
func (p capsPlan) synthetic(upper bool) bool {
	if p.support != capsNone || p.caps == css.TitlingCaps || !p.allowed {
		return false
	}
	if upper {
		return p.synth == synthLower || p.synth == synthBoth
	}
	return p.synth == synthUpper || p.synth == synthBoth
}

// caseMap returns the case a piece is mapped to: 'U' upper, 'L' lower, 0
// as written (NeedsCaseChange).
func (p capsPlan) caseMap(upper bool) byte {
	if p.support == capsFull || !p.allowed {
		return 0
	}
	if !upper {
		if p.support == capsFallback && (p.synth == synthBoth || p.synth == synthUpper) {
			return 'L'
		}
		return 0
	}
	if p.support != capsFallback && (p.synth == synthLower || p.synth == synthBoth) {
		return 'U'
	}
	return 0
}

// smallCapsPiece is a run of text of one case behaviour: its characters
// after case mapping, whether it is drawn at the small capitals' size,
// the caps features it is shaped with, and the characters it extracts as.
type smallCapsPiece struct {
	text     string
	small    bool
	features []text.Feature
	// source is the piece's text before case mapping, "" when the
	// mapping left it as it was.
	source string
}

// needsUpper reports whether r changes when uppercased, Unicode's
// Changes_When_Uppercased as Chromium's SmallCapsIterator reads it,
// approximated by the simple case mapping.
func needsUpper(r rune) bool {
	return unicode.ToUpper(r) != r && unicode.IsLower(r)
}

// smallCaps cuts s into the pieces plan draws it as: one piece when no
// synthesis is needed, else one per change of case behaviour, a combining
// mark taking the behaviour of the character before it
// (SmallCapsIterator). The caller has a case-mapped piece extract as its
// characters before the mapping under BrowserRules, as Chromium's PDF
// marks each such glyph with its character, and as drawn otherwise, as
// Word's PDF reads small capitals.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws
//     font-variant: small-caps in Liberation Serif at 40px with the
//     capitals of the lowercase letters at 28px, each in a /Span whose
//     ActualText is the lowercase letter; pdftotext reads "Small Caps".
//   - litho/docx: run-effects, Word's reference reads "SMALL CAPS TEXT"
//     for w:smallCaps text typed "Small Caps Text".
func smallCaps(s string, plan capsPlan) []smallCapsPiece {
	if plan.caps == css.VariantNormal {
		return []smallCapsPiece{{text: s}}
	}
	if !plan.splits() {
		return []smallCapsPiece{{text: s, features: plan.features(false)}}
	}
	var out []smallCapsPiece
	var cur []rune
	upper, started := false, false
	flush := func() {
		if len(cur) == 0 {
			return
		}
		src := string(cur)
		t := src
		switch plan.caseMap(upper) {
		case 'U':
			t = string(mapRunes(cur, unicode.ToUpper))
		case 'L':
			t = string(mapRunes(cur, unicode.ToLower))
		}
		if t == src {
			src = ""
		}
		out = append(out, smallCapsPiece{text: t, small: plan.synthetic(upper), features: plan.features(upper), source: src})
		cur = cur[:0]
	}
	for _, r := range s {
		u := upper
		if !unicode.Is(unicode.Mn, r) || !started {
			u = needsUpper(r)
		}
		if started && u != upper {
			flush()
		}
		upper, started = u, true
		cur = append(cur, r)
	}
	flush()
	return out
}

// mapRunes maps every rune of rs by f, in place.
func mapRunes(rs []rune, f func(rune) rune) []rune {
	for i, r := range rs {
		rs[i] = f(r)
	}
	return rs
}
