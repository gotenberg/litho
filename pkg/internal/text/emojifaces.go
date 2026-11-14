package text

import (
	"github.com/go-text/typesetting/language"
	"github.com/go-text/typesetting/shaping"
)

// need is what a face must draw beyond the character it is resolved for:
// the other characters of an emoji sequence, and the presentation the
// sequence asks for. The zero need asks nothing more.
type need struct {
	chars []rune
	pres  presentation
}

// colourTables returns the colour glyph tables of the face, those of the
// face it is an instance of for a variable instance.
func (fc *Face) colourTables() uint8 {
	switch {
	case fc.base != nil:
		return fc.base.colour
	case fc.varBase != nil:
		return fc.varBase.colour
	}
	return fc.colour
}

// draws reports whether fc, which covers the first character of n, draws
// the rest of n as n asks: it covers every other character, and, for a
// sequence asked to draw as emoji, it is a colour face or maps the
// sequence's first character with VS16 to a glyph of its own (OpenType,
// cmap format 14); for one asked to draw as text, it is no colour face or
// maps the character with VS15.
func (st *layoutState) draws(fc *Face, n need) bool {
	for _, r := range n.chars[min(1, len(n.chars)):] {
		if !st.covers(fc, r) {
			return false
		}
	}
	switch n.pres { //nolint:exhaustive // a sequence of no presentation, or of emoji by default, takes any face
	case presentEmoji:
		return fc.colourTables() != 0 || st.mapsVariation(fc, n.chars[0], variationEmoji)
	case presentText:
		return fc.colourTables() == 0 || st.mapsVariation(fc, n.chars[0], variationText)
	}
	return true
}

// mapsVariation reports whether the font of fc maps r followed by the
// variation selector vs to a glyph of its own.
func (st *layoutState) mapsVariation(fc *Face, r, vs rune) bool {
	ft := st.font(fc)
	if ft == nil {
		return false
	}
	gid, ok := ft.VariationGlyph(r, vs)
	return ok && gid != 0
}

// seqKey identifies one resolution of an emoji sequence.
type seqKey struct {
	style  int
	script language.Script
	pres   presentation
	chars  string
}

// drawnChars returns the characters of an emoji sequence a face must
// cover: all but the variation selectors and the joiners, which a font
// that forms the sequence maps or ignores.
func drawnChars(seq []rune) []rune {
	out := make([]rune, 0, len(seq))
	for _, r := range seq {
		if r != variationEmoji && r != variationText && r != zeroWidthJoin {
			out = append(out, r)
		}
	}
	return out
}

// resolveSeq returns the face that draws the emoji sequence seq, whose
// presentation is pres, in the run of style index style: the first face
// that covers each of its characters and draws it in pres (see
// [layoutState.draws]), looked for in the family list, then, for an emoji
// presentation, among the colour faces, then by the fallback order. A
// sequence no face draws so is resolved as its first character alone.
//
// One face draws the whole sequence, as a browser shapes a cluster in the
// first font that holds every one of its characters (CSS Fonts 4, 5.4,
// cluster matching), so a font that forms a ZWJ sequence, a flag or a
// keycap into one glyph gets all of it.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 of gotenberg/gotenberg:9.4.1 draws U+1F600
//     under font-family: DejaVu Sans in DejaVu Sans, and in Noto Color
//     Emoji followed by VS16; under sans-serif, which Liberation Sans
//     draws, in Noto Color Emoji though DejaVu Sans covers it; U+263A,
//     U+2764 and U+2660 in a text face, and in Noto Color Emoji followed
//     by VS16 or under font-variant-emoji: emoji, which also draws the
//     digits of the line in it.
//   - probe, html: the same Chromium draws U+1F600 and U+1F44D followed by
//     VS15, and under font-variant-emoji: text, in a text face, and
//     U+26BD and U+1F680, which no text face covers, in Noto Color Emoji.
//   - litho/docx: intl-scripts: Word draws the emoji of a run in Calibri
//     and of one in Segoe UI Emoji in colour, in Segoe UI Emoji; litho
//     lists DejaVu Sans after a run's font as the stand-in of a missing
//     face, which drew them as outlines before the colour faces.
//   - litho/pptx: text-complex-scripts: PowerPoint draws the emoji in
//     colour, in Segoe UI Emoji.
func (st *layoutState) resolveSeq(style int, script language.Script, seq []rune, pres presentation) *Face {
	chars := drawnChars(seq)
	if len(chars) == 0 {
		return st.resolve(style, script, seq[0])
	}
	key := seqKey{style: style, script: script, pres: pres, chars: string(chars)}
	if fc, ok := st.seqCache[key]; ok {
		return fc
	}
	s := &st.styles[style]
	n := need{chars: chars, pres: pres}
	r := chars[0]
	var fc *Face
	if s.refs != nil {
		fc = st.resolveRefs(s, r, n)
	}
	if fc == nil && st.covers(s.primary, r) && st.draws(s.primary, n) {
		fc = s.primary
	}
	if fc == nil {
		sc := script
		if l := language.LookupScript(r); l != language.Common && l != language.Inherited && l != language.Unknown {
			sc = l
		}
		fc = st.fallback(s, sc, r, n)
	}
	if fc == nil {
		fc = st.resolve(style, script, r)
	}
	if st.seqCache == nil {
		st.seqCache = make(map[seqKey]*Face)
	}
	st.seqCache[key] = fc
	return fc
}

// plainText reports an emoji sequence the plain split already draws as it
// asks: one character asked to draw as text for which the face found for
// the character alone is no colour face. [layoutState.resolveSeq] tries
// the same faces in the same order and takes the first that covers the
// character and is no colour face, which is that face, so the sequence
// needs no input of its own. Every digit, # and * of a run under
// font-variant-emoji: text is such a sequence (they are Emoji characters,
// the bases of keycaps): a line of figures then splits as it does
// without the property.
func (st *layoutState) plainText(idx int, script language.Script, text []rune, sq emojiSeq) bool {
	if sq.pres != presentText || sq.end-sq.start != 1 {
		return false
	}
	fc := st.resolve(idx, script, text[sq.start])
	return fc != nil && fc.colourTables() == 0
}

// primaryDrawsAsText reports whether the first face of style idx draws r
// as text: it covers r and is no colour face, and the style lists no
// @font-face faces to try before it. A character asked to draw as text
// then takes that face whatever [layoutState.resolveSeq] would try, so it
// is no sequence at all: the figures of a spreadsheet cell, laid out under
// Excel's text presentation, are found here before a sequence is made for
// each digit.
func (st *layoutState) primaryDrawsAsText(idx int, r rune) bool {
	s := &st.styles[idx]
	return s.refs == nil && s.primary != nil && s.primary.colourTables() == 0 && st.covers(s.primary, r)
}

// splitByFace splits in by face as [shaping.SplitByFace] does with the
// fontmap of style index idx, but gives each emoji sequence of seqs that
// in holds the one face [layoutState.resolveSeq] picks for it. Without
// such a sequence the split is go-text's alone.
func (st *layoutState) splitByFace(in shaping.Input, idx int, seqs []emojiSeq) []shaping.Input {
	var out []shaping.Input
	add := func(x shaping.Input) {
		if n := len(out); n > 0 && out[n-1].Face == x.Face && out[n-1].RunEnd == x.RunStart {
			out[n-1].RunEnd = x.RunEnd
			return
		}
		out = append(out, x)
	}
	pos := in.RunStart
	for _, sq := range seqs {
		if sq.end <= in.RunStart || sq.start >= in.RunEnd || st.plainText(idx, in.Script, in.Text, sq) {
			continue
		}
		start, end := max(sq.start, in.RunStart), min(sq.end, in.RunEnd)
		if pos < start {
			sub := in
			sub.RunStart, sub.RunEnd = pos, start
			for _, x := range shaping.SplitByFace(sub, st.fontmap(idx, in.Script)) {
				add(x)
			}
		}
		x := in
		x.RunStart, x.RunEnd = start, end
		x.Face = st.face(st.resolveSeq(idx, in.Script, in.Text[sq.start:sq.end], sq.pres))
		add(x)
		pos = end
	}
	if pos == in.RunStart {
		return shaping.SplitByFace(in, st.fontmap(idx, in.Script))
	}
	if pos < in.RunEnd {
		sub := in
		sub.RunStart, sub.RunEnd = pos, in.RunEnd
		for _, x := range shaping.SplitByFace(sub, st.fontmap(idx, in.Script)) {
			add(x)
		}
	}
	return out
}
