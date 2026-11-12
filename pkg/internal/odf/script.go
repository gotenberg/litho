package odf

import (
	"context"
	"slices"
	"unicode"

	"golang.org/x/text/unicode/bidi"

	"github.com/gotenberg/litho/pkg/internal/text"
)

// Script is the set of text properties a character is drawn with: an
// OpenDocument style gives Western, Asian and complex text each its own
// font, size, weight, posture and language (ODF 1.3, Part 3, 20.192,
// 20.193, 20.194, 20.196, 20.198 and their -asian and -complex
// counterparts, 20.269 to 20.281).
type Script uint8

// The scripts.
const (
	// Western is the script of the fo: attributes: Latin, Greek, Cyrillic
	// and the scripts drawn like them.
	Western Script = iota
	// Asian is the script of the -asian attributes: Han, kana, Hangul,
	// Bopomofo, Yi and their punctuation.
	Asian
	// Complex is the script of the -complex attributes: the right-to-left
	// scripts, the Indic and South-East Asian scripts, and every other
	// script.
	Complex
)

// charClass is the script class of a character before its context
// decides: a script, or weak, which takes the script around it.
type charClass uint8

// The classes.
const (
	classWestern charClass = iota
	classAsian
	classComplex
	classWeak
)

// blockClass is a range of code points that takes one class whatever the
// script of each character: the punctuation, digits and symbols of a
// script's blocks belong to that script.
type blockClass struct {
	lo, hi rune
	class  charClass
}

// classBlocks are the ranges whose characters take the class of the
// range, by Unicode block, in code point order. A character outside them
// takes the class of its script ([charScriptClass]).
//
// # Corpus evidence
//
//   - probe, odt: through gotenberg/gotenberg:9.4.1, each of 15,792 code
//     points of every block, every assigned character of the Basic
//     Multilingual Plane but the ideographs, Hangul syllables and Yi
//     syllables (one in 64 or 256 of them), one in 32 of the
//     Supplementary Multilingual Plane and one in 2048 of planes 2 and
//     3, set between two ideographs and between two Latin letters in
//     Western, Asian and complex sizes of 10, 20 and 30 points, prints
//     at the size of the class these ranges and [charScriptClass] give it.
var classBlocks = []blockClass{
	{0x0000, 0x02FF, classWestern},   // Basic Latin to Spacing Modifier Letters
	{0x0370, 0x052F, classWestern},   // Greek and Coptic, Cyrillic, Cyrillic Supplement
	{0x0590, 0x08FF, classComplex},   // Hebrew to Arabic Extended-A
	{0x0900, 0x0DFF, classComplex},   // Devanagari to Sinhala
	{0x0E00, 0x0FFF, classComplex},   // Thai, Lao, Tibetan
	{0x10A0, 0x10FF, classWestern},   // Georgian
	{0x16A0, 0x16FF, classWestern},   // Runic
	{0x1800, 0x18AF, classComplex},   // Mongolian
	{0x2150, 0x218F, classWeak},      // Number Forms
	{0x2E80, 0x2FFF, classAsian},     // CJK Radicals Supplement to Ideographic Description Characters
	{0x3000, 0x33FF, classAsian},     // CJK Symbols and Punctuation to CJK Compatibility
	{0xA720, 0xA7FF, classWestern},   // Latin Extended-D
	{0xFB50, 0xFDFF, classComplex},   // Arabic Presentation Forms-A
	{0xFE30, 0xFE4F, classAsian},     // CJK Compatibility Forms
	{0xFF00, 0xFFEF, classAsian},     // Halfwidth and Fullwidth Forms
	{0x1EC70, 0x1ECBF, classComplex}, // Indic Siyaq Numbers
}

// weakLatin are the characters of the Latin blocks that take the script
// around them: the space, the no-break space, the superscript digits and
// the tone marks Bopomofo shares.
var weakLatin = [...]rune{0x0020, 0x00A0, 0x00B2, 0x00B3, 0x00B9, 0x02C7, 0x02CA, 0x02CB, 0x02D9}

// westernScripts and asianScripts are the scripts drawn with the Western
// and the Asian text properties outside [classBlocks]; every other
// script is complex.
var (
	westernScripts = []*unicode.RangeTable{
		unicode.Armenian, unicode.Braille, unicode.Canadian_Aboriginal, unicode.Cherokee, unicode.Coptic,
		unicode.Cyrillic, unicode.Georgian, unicode.Glagolitic, unicode.Gothic, unicode.Greek, unicode.Latin,
		unicode.Ogham, unicode.Old_Italic, unicode.Osmanya, unicode.Runic, unicode.Shavian,
	}
	asianScripts = []*unicode.RangeTable{
		unicode.Bopomofo, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana, unicode.Tangut, unicode.Yi,
	}
)

// charScriptClass returns the class of r alone: its range's in
// [classBlocks], else its script's, weak for the characters of no script
// (Common and Inherited: punctuation, symbols and marks shared by
// several scripts) and for unassigned code points.
func charScriptClass(r rune) charClass {
	if r < 0x20 || r >= 0x7F && r < 0xA0 {
		return classWeak
	}
	if r < 0x0300 {
		for _, w := range weakLatin {
			if r == w {
				return classWeak
			}
		}
	}
	lo, hi := 0, len(classBlocks)
	for lo < hi {
		m := (lo + hi) / 2
		switch b := classBlocks[m]; {
		case r < b.lo:
			hi = m
		case r > b.hi:
			lo = m + 1
		default:
			return b.class
		}
	}
	switch {
	case unicode.In(r, unicode.Common, unicode.Inherited):
		return classWeak
	case unicode.In(r, westernScripts...):
		return classWestern
	case unicode.In(r, asianScripts...):
		return classAsian
	case !unicode.IsLetter(r) && !unicode.IsMark(r) && !unicode.IsNumber(r) && !unicode.IsPunct(r) && !unicode.IsSymbol(r):
		return classWeak
	}
	return classComplex
}

// Scripts returns the script of each character of a paragraph's text,
// rtl when the paragraph runs right to left, as LibreOffice assigns them:
//
//   - A character of a script takes that script ([charScriptClass]).
//   - A weak character, a space, a digit of no script's block, a shared
//     punctuation mark or symbol, takes the script of the character before
//     it; at the paragraph's start, the script of the first character
//     that has one; in a paragraph of weak characters alone, Western.
//   - A character that is not Asian takes the complex script where it
//     runs right to left (an odd embedding level, UAX #9), and where it
//     sits in a left-to-right run nested in right-to-left text that holds
//     no strong left-to-right character, the numbers of right-to-left
//     text.
//
// A paragraph of Western characters alone, run left to right, returns
// nil. ctx's error stops the bidirectional resolution of a long text.
//
// # Corpus evidence
//
//   - probe, odt: "漢字123漢字" prints its digits in the Western size,
//     "漢字 abc 漢字" the space after the ideographs in the Asian one,
//     "“漢字”" and "★漢字" their first mark in the Asian one, "€ 100" all
//     Western.
//   - probe, odt: in a left-to-right paragraph, "abc שלום 123 def" and
//     "عربي 123" print their digits in the complex size, "abc 123 שלום"
//     in the Western one; in a right-to-left one, "“漢字”" its quotation
//     marks, "€ 100" every character and "1 2 3 漢字" its digits in the
//     complex size, "abc 123" and the full stop of "שלום 。漢字" keep
//     theirs.
//   - probe, ods and odp: the same paragraphs print alike in a cell and in
//     a text box, a right-to-left sheet's cells running left to right.
func Scripts(ctx context.Context, txt []rune, rtl bool) ([]Script, error) {
	classes := make([]charClass, len(txt))
	plain := !rtl
	for i, r := range txt {
		c := charScriptClass(r)
		classes[i] = c
		if c == classAsian || c == classComplex {
			plain = false
		}
	}
	if plain {
		return nil, nil
	}
	out := make([]Script, len(txt))
	next := Western
	for i := len(classes) - 1; i >= 0; i-- {
		if c := classes[i]; c != classWeak {
			next = Script(c)
		}
		out[i] = next
	}
	prev, seen := Western, false
	for i, c := range classes {
		switch {
		case c != classWeak:
			prev, seen = Script(c), true
		case seen:
			out[i] = prev
		}
	}
	base := text.LeftToRight
	baseLevel := int8(0)
	if rtl {
		base, baseLevel = text.RightToLeft, 1
	}
	levels, err := text.Levels(ctx, txt, base)
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(txt); {
		end := start + 1
		for end < len(txt) && levels[end] == levels[start] {
			end++
		}
		lv := levels[start]
		complexRun := lv%2 == 1
		if !complexRun && lv > baseLevel {
			complexRun = true
			for _, r := range txt[start:end] {
				if p, _ := bidi.LookupRune(r); p.Class() == bidi.L {
					complexRun = false
					break
				}
			}
		}
		if complexRun {
			for i := start; i < end; i++ {
				if classes[i] != classAsian {
					out[i] = Complex
				}
			}
		}
		start = end
	}
	return out, nil
}

// MayMixScripts reports whether a paragraph of the texts, rtl when it runs
// right to left, may hold a character that is not Western: when it does
// not, [SplitScripts] returns nil.
func MayMixScripts(texts []string, rtl bool) bool {
	return rtl || slices.ContainsFunc(texts, beyondLatin)
}

// beyondLatin reports a text holding a character from the Hebrew block
// on, where the first character that is not Western can be: below it,
// every character is Western or weak, and no character runs right to
// left.
func beyondLatin(s string) bool {
	for _, r := range s {
		if r >= 0x0590 {
			return true
		}
	}
	return false
}

// ScriptPart is a piece of a run's text in one script.
type ScriptPart struct {
	Text   string
	Script Script
}

// SplitScripts cuts the texts of a paragraph's runs, in reading order, at
// each change of script ([Scripts]): the parts of each run in order. A
// paragraph of Western characters alone, run left to right, returns nil:
// every run is Western whole.
func SplitScripts(ctx context.Context, runs []string, rtl bool) ([][]ScriptPart, error) {
	if !MayMixScripts(runs, rtl) {
		return nil, nil
	}
	var all []rune
	for _, s := range runs {
		all = append(all, []rune(s)...)
	}
	scripts, err := Scripts(ctx, all, rtl)
	if err != nil || scripts == nil {
		return nil, err
	}
	out := make([][]ScriptPart, len(runs))
	at := 0
	for i, s := range runs {
		rs := []rune(s)
		for k := 0; k < len(rs); {
			sc := scripts[at+k]
			e := k + 1
			for e < len(rs) && scripts[at+e] == sc {
				e++
			}
			out[i] = append(out[i], ScriptPart{Text: string(rs[k:e]), Script: sc})
			k = e
		}
		at += len(rs)
	}
	return out, nil
}
