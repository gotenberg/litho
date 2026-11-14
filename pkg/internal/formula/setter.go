package formula

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"

	gofont "github.com/go-text/typesetting/font"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/mathtable"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// Setter lays formulas out with the fonts of one conversion. It keeps
// the fonts it has read, so a converter makes one for a conversion and
// lays every formula of it out through it.
//
// A Setter is not safe for concurrent use.
type Setter struct {
	// BrowserRules lays a formula whose first available font has no MATH
	// table out with the fallback constants Chromium uses, where they
	// depart from MathML Core, 5.1 ([chromiumFallback]); off, the
	// fallbacks of 5.1 apply. The HTML converter sets it.
	BrowserRules bool

	fonts *text.Fonts
	// logger receives diagnostics, nil for none.
	logger *slog.Logger
	// faces holds the first available fonts met, by face.
	faces map[*text.Face]*face
	// programs holds the parsed programs of the faces shaped runs are
	// drawn in, for the ink of their glyphs, by face key.
	programs map[string]*gofont.Face
}

// NewSetter returns a Setter that lays formulas out with fonts, logging
// to logger when it is not nil.
func NewSetter(fonts *text.Fonts, logger *slog.Logger) *Setter {
	return &Setter{fonts: fonts, logger: logger, faces: map[*text.Face]*face{}, programs: map[string]*gofont.Face{}}
}

// Box is a formula laid out: its extent about its own baseline and what
// it draws. Lengths are in points.
type Box struct {
	// Width is the inline size of the formula.
	Width float64
	// Ascent and Descent are the distances from the baseline to the top
	// and the bottom of the formula's box, the line-ascent and
	// line-descent of MathML Core, 3.1.1; either may be negative.
	Ascent, Descent float64
	// Warnings lists what the layout could not draw as the formula asks.
	Warnings []typeset.Warning
	root     *box
}

// Draw returns the fragments of the formula drawn with the left end of
// its baseline at x, y in the caller's coordinates, y downward. Each call
// returns new fragments the caller may change.
func (b *Box) Draw(x, y float64) []typeset.Fragment {
	if b == nil || b.root == nil {
		return nil
	}
	var out []typeset.Fragment
	b.root.draw(x, y, &out)
	return out
}

// errNoFace reports a style whose families match no face.
var errNoFace = errors.New("no font face for the formula")

// Layout lays root out, its style being st when root.Style is nil. The
// box depends on the tree, st and the setter's fonts alone. It returns
// ctx's error when ctx ends first, and an error when the fonts hold no
// face at all.
func (s *Setter) Layout(ctx context.Context, root *Element, st Style) (*Box, error) {
	if root == nil {
		return &Box{}, nil
	}
	if root.Style != nil {
		st = *root.Style
	}
	if st.Size <= 0 {
		return nil, fmt.Errorf("laying out a formula at size %g: %w", st.Size, errInvalidStyle)
	}
	l := &layouter{ctx: ctx, s: s, mem: limits.BudgetFrom(ctx)}
	b := l.layoutRoot(root, &st)
	if l.err != nil {
		return nil, l.err
	}
	return &Box{Width: b.w, Ascent: b.asc, Descent: b.desc, Warnings: l.warnings, root: b}, nil
}

// errInvalidStyle reports a style the layout cannot use.
var errInvalidStyle = errors.New("invalid formula style")

// layouter lays one formula out. It does not outlive the [Setter.Layout]
// call that makes it, whose context it holds.
type layouter struct {
	ctx      context.Context
	s        *Setter
	mem      *limits.Budget
	err      error
	warnings []typeset.Warning
	warned   map[string]bool
	// elements counts the elements laid out, against [MaxElements].
	elements int
	// cores and spaces keep what [layouter.core] and
	// [layouter.spaceLike] found of each element.
	cores  map[*Element]*Element
	spaces map[*Element]bool
}

// warn records a warning once per message.
func (l *layouter) warn(code warn.Code, effect warn.Effect, msg string) {
	if l.warned[msg] {
		return
	}
	if l.warned == nil {
		l.warned = map[string]bool{}
	}
	l.warned[msg] = true
	l.warnings = append(l.warnings, typeset.Warning{Code: code, Effect: effect, Message: msg})
}

// fail records the first error that stops the layout.
func (l *layouter) fail(err error) {
	if l.err == nil {
		l.err = err
	}
}

// face is a first available font of a formula: the face a style matches,
// its program, its glyphs and its MATH table.
type face struct {
	text   *text.Face
	ts     *typeset.Face
	glyphs *gofont.Face
	// math is the face's MATH table, nil when it has none.
	math *mathtable.Table
	upem float64
	// The OS/2 and post values the fallback constants of MathML Core, 5.1
	// read, in font units: xHeight is sxHeight (half an em when the face
	// records none), subOffset and superOffset ySubscriptYOffset and
	// ySuperscriptYOffset, ruleThickness post.underlineThickness.
	xHeight, subOffset, superOffset, ruleThickness float64
	// browser takes Chromium's fallback constants ([Setter.BrowserRules]).
	browser bool
}

// textStyle returns the text style a token of st is shaped in.
func textStyle(st *Style) text.Style {
	return text.Style{
		Families: st.Families, Weight: st.Weight, Italic: st.Italic, Size: min(st.Size, maxSize),
		Lang: st.Lang, Fallback: st.Fallback, Color: st.Color,
	}
}

// maxSize is the largest font size text is shaped at, the bound of
// [text.Style.Size].
const maxSize = 1000

// faceOf returns the first available font of st, nil when none loads.
func (l *layouter) faceOf(st *Style) *face {
	tf := l.s.fonts.Match(textStyle(st))
	if tf == nil {
		return nil
	}
	if f, ok := l.s.faces[tf]; ok {
		return f
	}
	f := l.loadFace(tf)
	l.s.faces[tf] = f
	return f
}

// loadFace reads the program, the glyphs and the MATH table of tf.
func (l *layouter) loadFace(tf *text.Face) *face {
	ts, err := tf.Typeset(l.ctx)
	if err != nil {
		l.fail(err)
		return nil
	}
	g := l.s.program(ts)
	if g == nil {
		return nil
	}
	f := &face{text: tf, ts: ts, glyphs: g, upem: float64(g.Upem()), browser: l.s.BrowserRules}
	if f.upem <= 0 {
		f.upem = 1000
	}
	if t, err := mathtable.Parse(ts.Program); err == nil {
		f.math = t
	} else if !errors.Is(err, mathtable.ErrNoMath) && l.s.logger != nil {
		// A broken MATH table is read as none: the fallback constants of
		// MathML Core, 5.1 apply.
		l.s.logger.Debug("formula: unreadable MATH table", slog.String("face", ts.Key), slog.Any("error", err))
	}
	f.xHeight = f.upem / 2
	records, err := truetype.ParseTableDirectory(ts.Program)
	if err != nil {
		return f
	}
	if os2, err := truetype.TableData(ts.Program, records, "OS/2"); err == nil {
		// OS/2: ySubscriptYOffset at 12, ySuperscriptYOffset at 20, and
		// sxHeight at 86 from version 2 (OpenType 1.9.1, OS/2 table).
		if len(os2) >= 22 {
			f.subOffset = float64(int16(binary.BigEndian.Uint16(os2[12:])))
			f.superOffset = float64(int16(binary.BigEndian.Uint16(os2[20:])))
		}
		if len(os2) >= 88 && binary.BigEndian.Uint16(os2) >= 2 {
			if xh := int16(binary.BigEndian.Uint16(os2[86:])); xh > 0 {
				f.xHeight = float64(xh)
			}
		}
	}
	if post, err := truetype.TableData(ts.Program, records, "post"); err == nil && len(post) >= 12 {
		// post: underlineThickness at 10 (OpenType 1.9.1, post table).
		f.ruleThickness = float64(int16(binary.BigEndian.Uint16(post[10:])))
	}
	return f
}

// program returns the parsed program of ts, nil when it does not parse:
// the font the index keeps parsed for the face of that key, else the
// program parsed here.
func (s *Setter) program(ts *typeset.Face) *gofont.Face {
	if g, ok := s.programs[ts.Key]; ok {
		return g
	}
	if fc := s.fonts.FaceByKey(ts.Key); fc != nil {
		if g := fc.GlyphFace(); g != nil {
			s.programs[ts.Key] = g
			return g
		}
	}
	g, err := gofont.ParseTTF(bytes.NewReader(ts.Program))
	if err != nil {
		// The text stack loaded this program, so it parses; one that does
		// not draws no glyph of its own.
		if s.logger != nil {
			s.logger.Debug("formula: face without glyphs", slog.String("face", ts.Key), slog.Any("error", err))
		}
		g = nil
	}
	s.programs[ts.Key] = g
	return g
}
