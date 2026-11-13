package text

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/shaping"
	"golang.org/x/image/math/fixed"

	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/varfont"
)

// Variation is one axis value of a variable font: CSS font-variation-settings
// (CSS Fonts 4, 8.2) and its @font-face descriptor (4.6).
type Variation struct {
	// Tag is the axis tag, four characters from U+20 to U+7E; a tag the
	// face has no axis for is ignored.
	Tag string
	// Value is the axis value in the axis's own units, clamped to the
	// axis's range.
	Value float64
}

// maxDocumentInstances caps the instances of variable faces, document and
// installed ones, one conversion creates: 128. Anti-DoS guard, not
// spec-mandated: every instance is a font program as large as its face's,
// kept for the conversion and embedded, and a page uses one per weight,
// width, slant and optical size it sets; nine weights at a handful of
// sizes stay far below it. Past it, text draws in the face's default
// instance and the layout reports it.
const maxDocumentInstances = 128

// The registered axes the font properties set (OpenType specification,
// Registered design-variation axis tags).
const (
	axisWeight      = "wght"
	axisWidth       = "wdth"
	axisSlant       = "slnt"
	axisOpticalSize = "opsz"
)

// italicSlope is the slant, in degrees, font-style: italic sets the slnt
// axis to on a variable face, and the angle oblique without one stands
// for (CSS Fonts 4, 2.4).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1), in print and in
//     screenshots, on four fonts: FontStyleTest-slnt-VF and Inter-VF (slnt
//     alone) draw font-style: italic and oblique with the outlines of
//     font-variation-settings: "slnt" -14 (clamped to the axis),
//     unslanted; FontStyleTest-ital-VF (ital alone) draws italic at its
//     default ital 0, slanted under a rule without font-style and upright
//     under font-style: italic; FontStyleTest-slnt_ital-VF (both axes)
//     draws italic as "slnt" -14 with ital at its default. It never sets
//     the ital axis.
//   - wpt, css/css-fonts/variations/ital-variable: Chromium 156 fails it
//     in print and in screenshots, on all three lines, for that reason.
const italicSlope = 14

// cssPixelsPerPoint converts points to CSS pixels (CSS Values 4, 6.2).
const cssPixelsPerPoint = 4.0 / 3

// instanceCount counts the instances a set of document faces created.
type instanceCount struct {
	mu sync.Mutex
	n  int
}

// take counts one more instance and reports whether the bound allowed it.
func (c *instanceCount) take() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n >= maxDocumentInstances {
		return false
	}
	c.n++
	return true
}

// errInstanceLimit reports a face past [maxDocumentInstances].
var errInstanceLimit = fmt.Errorf("more than %d instances of variable fonts", maxDocumentInstances)

// readVariations reads the design space of a document face's program,
// when it is variable, and sets the descriptor ranges the rule left at
// auto from the face's axes (CSS Fonts 4, 4.4: an auto descriptor selects
// as normal and clamps no axis): an auto weight covers the wght axis, so
// that no weight it reaches is synthesised, and an auto style on a face
// with a slnt axis slants by the axis and never synthetically.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws RobotoExtremo-VF
//     at font-weight: 900 with the same outlines whether its rule leaves
//     font-weight out or sets 100 900, and synthesises bold when the rule
//     sets 400; Inter-VF at font-style: italic is slanted by its axis
//     alone under a rule without font-style, and by its axis and a
//     synthetic skew under font-style: oblique 0deg 10deg.
func (fc *Face) readVariations(program []byte, d *FaceDescriptor) {
	vf, err := varfont.Parse(program)
	switch {
	case errors.Is(err, truetype.ErrTableNotFound):
		return
	case err != nil:
		fc.vfErr = err
		return
	}
	fc.vf = vf
	axes := vf.Axes()
	if i := axes.Index(axisWeight); i >= 0 && d.autoWeight {
		ax := axes.Axes[i]
		d.WeightMin = int(min(max(math.Ceil(ax.Min), 1), 1000))
		d.WeightMax = int(min(max(math.Floor(ax.Max), float64(d.WeightMin)), 1000))
	}
	if i := axes.Index(axisWidth); i >= 0 && d.autoStretch {
		d.StretchMin, d.StretchMax = axes.Axes[i].Min, axes.Axes[i].Max
	}
	d.slantAxis = d.AutoStyle && axes.Index(axisSlant) >= 0
}

// userCoordinates returns the user coordinates the face is drawn at in
// style, one per axis of its 'fvar' table: the font-weight, font-stretch
// and font-style properties clamped to the rule's descriptors,
// font-optical-sizing, the rule's font-variation-settings, then the
// style's font-variation-settings. Each value is clamped to its axis when
// the coordinates are normalized. CSS Fonts 4, 7.2 applies
// font-optical-sizing after the rule's font-variation-settings; Chromium
// applies it before, so that a rule's opsz holds, and litho follows
// Chromium.
//
// font-style sets the slnt axis to the negated slope (CSS Fonts 4, 2.4:
// slnt counts counter-clockwise), italic as 14 degrees; it never sets the
// ital axis, which CSS Fonts 4, 2.4 sets to 1 for italic: Chromium does
// not (see [italicSlope]). font-optical-sizing: auto sets opsz to the
// font size, in CSS pixels under [FallbackChromium], as Chromium does, and
// in points otherwise, the unit of the OpenType opsz axis.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) embeds RobotoExtremo-VF
//     at 48px with the advances of opsz 48, at 12px of opsz 12 and at
//     100px of opsz 100 (fontTools' instancer); font-variation-settings:
//     "wght" 700 at font-weight: 400 with those of wght 700; a rule's
//     font-variation-settings: "wght" 800 at font-weight: 300 with those
//     of wght 800; font-weight: 900 under font-weight: 300 500 with those
//     of wght 500; a rule's font-variation-settings: "opsz" 144, "wdth" 125
//     at 48px with those of opsz 144 and wdth 125, as the same property.
func (fc *Face) userCoordinates(style *Style) []float64 {
	axes := fc.vf.Axes()
	user := axes.Defaults()
	set := func(tag string, v float64) {
		if i := axes.Index(tag); i >= 0 && !math.IsNaN(v) && !math.IsInf(v, 0) {
			user[i] = v
		}
	}
	d := fc.doc
	weight := float64(style.weight())
	stretch := style.stretch()
	slope := 0.0
	if style.Italic {
		slope = italicSlope
		if style.Oblique != 0 {
			slope = style.Oblique
		}
	}
	if d != nil {
		weight = min(max(weight, float64(d.WeightMin)), float64(d.WeightMax))
		if !d.autoStretch {
			stretch = min(max(stretch, d.StretchMin), d.StretchMax)
		}
		if !d.AutoStyle {
			switch d.Style {
			case FaceNormal:
				slope = 0
			case FaceItalic:
				slope = italicSlope
			case FaceOblique:
				slope = min(max(slope, d.ObliqueMin), d.ObliqueMax)
			}
		}
	}
	set(axisWeight, weight)
	set(axisWidth, stretch)
	set(axisSlant, 0-slope)
	settings := style.settings()
	if !settings.NoOpticalSizing {
		size := style.Size
		if style.Fallback == FallbackChromium {
			size *= cssPixelsPerPoint
		}
		set(axisOpticalSize, size)
	}
	if d != nil {
		for _, v := range d.Variations {
			set(v.Tag, v.Value)
		}
	}
	for _, v := range settings.Variations {
		set(v.Tag, v.Value)
	}
	return user
}

// instanceOf returns the face that draws fc, a document face, in style: fc
// itself for a face that is not variable or a style at the default
// instance; else the instance of fc at the style's coordinates, created on
// first use and counted by count. A face whose variations litho cannot
// apply draws as fc, its default instance, and why is returned with it.
func (fc *Face) instanceOf(ctx context.Context, style *Style, mem *limits.Budget, count *instanceCount) (*Face, error) {
	if fc.doc == nil || fc.base != nil {
		return fc, nil
	}
	if fc.vfErr != nil {
		return fc, fc.vfErr
	}
	if fc.vf == nil {
		return fc, nil
	}
	user := fc.userCoordinates(style)
	coords := fc.vf.Normalize(user)
	if !slicesNonZero(coords) {
		return fc, nil
	}
	key := coordsKey(coords)
	fc.instMu.Lock()
	defer fc.instMu.Unlock()
	if inst, ok := fc.instances[key]; ok {
		return inst, nil
	}
	if fc.instErr != nil {
		return fc, fc.instErr
	}
	if !count.take() {
		return fc, errInstanceLimit
	}
	inst, err := fc.newInstance(ctx, fc.vf, user, coords, key, mem, nil)
	if err != nil {
		if ctx.Err() == nil && !errors.Is(err, limits.ErrMemoryBudget) {
			fc.instErr = err
		}
		return fc, err
	}
	if fc.instances == nil {
		fc.instances = make(map[string]*Face)
	}
	fc.instances[key] = inst
	return inst, nil
}

// newInstance writes the instance of fc, whose design space is vf, at the
// user coordinates user, normalized as coords, charges its program to mem
// and reads it as a face that shapes with the variable font at coords and
// embeds the instance. load parses the font of a face of the index; a
// document face has its own.
func (fc *Face) newInstance(ctx context.Context, vf *varfont.Font, user []float64, coords []int16, key string, mem *limits.Budget, load func(*Face) (*font.Font, error)) (*Face, error) {
	program, err := vf.Instance(ctx, user)
	if err != nil {
		return nil, err
	}
	parsed := fc.pinned
	if parsed == nil {
		if parsed, err = load(fc); err != nil {
			return nil, err
		}
	}
	if err := mem.Charge(int64(len(program)), "font instances"); err != nil {
		return nil, err
	}
	src := &fontSource{
		path: fc.src.path + "@" + key,
		open: func(string) (sfntFile, int64, error) {
			return memFile{bytes.NewReader(program)}, int64(len(program)), nil
		},
	}
	r := &boundedReader{r: memFile{bytes.NewReader(program)}, left: maxFootprintFileBytes}
	faces, err := readFootprints(slog.New(slog.DiscardHandler), src, r, int64(len(program)))
	if err != nil {
		return nil, fmt.Errorf("reading the instance: %w", err)
	}
	if len(faces) == 0 {
		return nil, errors.New("the instance holds no face litho can use")
	}
	advances, err := truetype.ReadHMetrics(program)
	if err != nil {
		return nil, fmt.Errorf("reading the instance's advances: %w", err)
	}
	inst := faces[0]
	inst.order, inst.keys = fc.order, fc.keys
	// The instance shapes with a copy of the variable font's parsed
	// tables, which go-text never modifies: the shaper caches one
	// HarfBuzz font, and the coordinates of the face it was made from, per
	// parsed font.
	pinned := *parsed
	inst.pinned, inst.doc, inst.features = &pinned, fc.doc, fc.features
	inst.base, inst.advances = fc, advances
	inst.coords = make([]tables.Coord, len(coords))
	for i, c := range coords {
		inst.coords[i] = tables.Coord(c)
	}
	inst.once.Do(func() {
		inst.typesetFace = &typeset.Face{Program: program, Key: inst.Key()}
	})
	return inst, nil
}

// coordsKey returns a key naming normalized coordinates.
func coordsKey(coords []int16) string {
	b := make([]byte, 0, 2*len(coords))
	for _, c := range coords {
		b = append(b, byte(uint16(c)>>8), byte(c)) //nolint:gosec // the two bytes of a signed 2.14 coordinate
	}
	return hex.EncodeToString(b)
}

// slicesNonZero reports whether a coordinate is not 0, the default
// instance.
func slicesNonZero(coords []int16) bool {
	for _, c := range coords {
		if c != 0 {
			return true
		}
	}
	return false
}

// instanceInput hands the shaper of in the instance of its face in style,
// when the face is variable: a go-text face at the instance's
// coordinates, whose glyph positions follow the axes. Why a face draws at
// its default instance is recorded for the report. It fails only when ctx
// ends or the instance passes the conversion's memory budget.
func (st *layoutState) instanceInput(ctx context.Context, in *shaping.Input, style *Style) error {
	if in.Face == nil {
		return nil
	}
	fc := st.byFace[in.Face]
	if fc == nil {
		return nil
	}
	var inst *Face
	var err error
	switch {
	case fc.doc != nil && st.fonts.inst == nil:
		inst, err = fc, errNoConversion
	case fc.doc != nil:
		inst, err = fc.instanceOf(ctx, style, st.mem, &st.fonts.inst.count)
	default:
		inst, err = st.fonts.installedInstance(ctx, fc, style, st.mem)
	}
	switch {
	case err == nil:
	case errors.Is(err, limits.ErrMemoryBudget):
		// A refused charge also cancels ctx: the budget's error says why.
		return err
	case ctx.Err() != nil:
		return ctx.Err()
	default:
		st.noteDefaultInstance(fc, err)
	}
	if inst != fc {
		in.Face = st.face(inst)
	}
	return nil
}

// noteDefaultInstance records that fc draws at its default instance
// because of err.
func (st *layoutState) noteDefaultInstance(fc *Face, err error) {
	if st.defaultInstances == nil {
		st.defaultInstances = make(map[string]DefaultInstance)
	}
	name := fc.DeclaredFamily()
	if _, ok := st.defaultInstances[name]; ok {
		return
	}
	st.defaultInstances[name] = DefaultInstance{Family: name, Reason: err.Error(), Limit: errors.Is(err, errInstanceLimit)}
}

// instanceAdvances sets the advance of each glyph of out, shaped in an
// instance face at size, to the instance's own: the shaper interpolates
// advances from the variable font without rounding, while the instance,
// and the widths the PDF states for it, hold them rounded once every delta
// is summed. Shaping offsets, kerning included, are kept. A face that is
// not an instance and vertical text are left alone.
func (st *layoutState) instanceAdvances(out *shaping.Output, size fixed.Int26_6) {
	fc := st.byFace[out.Face]
	if fc == nil || fc.base == nil || out.Direction.IsVertical() {
		return
	}
	upem := float64(out.Face.Upem())
	if upem <= 0 {
		return
	}
	changed := false
	for i := range out.Glyphs {
		g := &out.Glyphs[i]
		d := fc.advances.Advance(intconv.Clamp(uint32(g.GlyphID))) - float64(out.Face.HorizontalAdvance(g.GlyphID))
		if d == 0 {
			continue
		}
		g.Advance += fixed.Int26_6(math.Round(d * float64(size) / upem))
		changed = true
	}
	if changed {
		out.RecalculateAll()
	}
}
