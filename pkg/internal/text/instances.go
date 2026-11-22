package text

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font/opentype/tables"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typography/varfont"
)

// maxFvarTableBytes bounds the 'fvar' table the index reads. Anti-DoS
// guard, not spec-mandated: an 'fvar' of the 64 axes litho reads and a
// thousand named instances weighs about 270 KiB, and fonts in use define
// a handful of axes and a few dozen instances, a few KiB.
const maxFvarTableBytes = 1 << 20

// maxNamedInstances bounds the named instances the index lists for one
// variable face, each a face of the index. Anti-DoS guard, not
// spec-mandated: 'fvar' counts its instances in a uint16, and the
// largest families in use name about forty (nine weights, upright and
// italic, two or three widths).
const maxNamedInstances = 256

// The registered axis that selects italic glyphs (OpenType specification,
// Registered design-variation axis tags, 'ital').
const axisItalic = "ital"

// errNoConversion reports an installed variable face laid out on an
// index that holds no conversion's instances (see [Fonts.ForConversion]).
var errNoConversion = errors.New("laid out outside a conversion, which instances belong to")

// namedInstance is one named instance of a variable face the index lists:
// its style name and user coordinates.
type namedInstance struct {
	name   string
	coords []float64
}

// readDesignSpace reads the 'fvar' table of a face of the index, when it
// has one: the face keeps its axes, and the named instances that differ
// from the default instance and from each other, at most
// [maxNamedInstances], with their style names from names. It instances
// nothing. A table that does not read leaves the face static.
func (fc *Face) readDesignSpace(raw []byte, names tables.Name) {
	axes, err := varfont.ParseFvar(raw)
	if err != nil {
		return
	}
	fc.axes = axes
	seen := [][]float64{axes.Defaults()}
	for _, in := range axes.Instances {
		if len(fc.namedInstances) >= maxNamedInstances {
			break
		}
		if containsCoords(seen, in.Coords) {
			continue
		}
		seen = append(seen, in.Coords)
		fc.namedInstances = append(fc.namedInstances, namedInstance{
			name:   strings.TrimSpace(names.Name(tables.NameID(in.SubfamilyNameID))),
			coords: in.Coords,
		})
	}
}

// containsCoords reports whether list holds coords.
func containsCoords(list [][]float64, coords []float64) bool {
	for _, c := range list {
		if len(c) != len(coords) {
			continue
		}
		same := true
		for i := range c {
			if c[i] != coords[i] {
				same = false
				break
			}
		}
		if same {
			return true
		}
	}
	return false
}

// namedFaces returns a face of the index for each named instance of fc: the
// footprint of fc, the aspect of the instance's coordinates and its full
// name, the family and the style name joined, which a CSS local() source
// names. It shares fc's file, coverage and metrics, and draws through it.
//
// The named instances that repeat the default instance's coordinates, or
// an earlier instance's, are left out, as fontconfig lists them once.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) finds local("Roboto
//     Flex Light") and local("SourceSans3VF Semibold") among the installed
//     fonts and not local("RobotoFlex-Light") nor
//     local("SourceSans3VF-Semibold"), the instances' PostScript names.
//   - probe, html and odt: fontconfig in the same image lists
//     FontStyleTest-ital-VF, whose Italic instance repeats its Regular
//     one's coordinates, as one Regular face.
func (fc *Face) namedFaces() []*Face {
	out := make([]*Face, 0, len(fc.namedInstances))
	for _, in := range fc.namedInstances {
		nf := &Face{
			src: fc.src, index: fc.index, dir: fc.dir,
			family: fc.family, keys: fc.keys,
			weight: fc.weight, slant: fc.slant, width: fc.width,
			ascent: fc.ascent, descent: fc.descent, lineGap: fc.lineGap,
			xHeight: fc.xHeight, capHeight: fc.capHeight, avgWidth: fc.avgWidth, maxWidth: fc.maxWidth, typoAscent: fc.typoAscent, typoDescent: fc.typoDescent,
			upem: fc.upem, revision: fc.revision, coverage: fc.coverage, size: fc.size,
			axes: fc.axes, named: in.coords, varBase: fc,
		}
		if in.name != "" {
			nf.fullName = fc.family + " " + in.name
		}
		nf.setInstanceAspect()
		out = append(out, nf)
	}
	return out
}

// setInstanceAspect sets the weight, width and slant of a named instance
// face from its coordinates, as fontconfig does for a named instance:
// the weight is the wght value, the width the usWidthClass nearest the
// wdth value, and a negative slnt or an ital of 0.5 or more makes it
// italic (OpenType specification, Registered design-variation axis tags).
func (fc *Face) setInstanceAspect() {
	for i, ax := range fc.axes.Axes {
		v := fc.axes.Clamp(i, fc.named[i])
		switch ax.Tag {
		case axisWeight:
			fc.weight = int(min(max(math.Round(v), 1), 1000))
		case axisWidth:
			fc.width = widthClassOf(v)
		case axisSlant:
			if v < 0 {
				fc.slant = slantItalic
			}
		case axisItalic:
			if v >= 0.5 {
				fc.slant = slantItalic
			}
		}
	}
}

// widthClassOf returns the usWidthClass whose font-stretch percentage is
// nearest pct, the narrower on a tie.
func widthClassOf(pct float64) int {
	class, best := widthNormal, math.Inf(1)
	for c := 1; c < len(widthPercents); c++ {
		if d := math.Abs(widthPercents[c] - pct); d < best {
			class, best = c, d
		}
	}
	return class
}

// fileFace returns the face of the file a face of the index draws
// through: its default instance for a named instance face, the face
// itself otherwise.
func (fc *Face) fileFace() *Face {
	if fc.varBase != nil {
		return fc.varBase
	}
	return fc
}

// fontconfigWeights are the steps of fontconfig's weight scale, the
// OpenType weights and fontconfig's values for them, between which it
// interpolates linearly (fontconfig, FcWeightFromOpenTypeDouble).
var fontconfigWeights = [...][2]float64{
	{0, 0},
	{100, 0},
	{200, 40},
	{300, 50},
	{350, 55},
	{380, 75},
	{400, 80},
	{500, 100},
	{600, 180},
	{700, 200},
	{800, 205},
	{900, 210},
	{1000, 215},
}

// fontconfigWeight returns fontconfig's weight for the OpenType weight w.
func fontconfigWeight(w float64) float64 {
	steps := fontconfigWeights[:]
	if w >= steps[len(steps)-1][0] {
		return steps[len(steps)-1][1]
	}
	for i := 1; i < len(steps); i++ {
		if w <= steps[i][0] {
			a, b := steps[i-1], steps[i]
			return a[1] + (w-a[0])*(b[1]-a[1])/(b[0]-a[0])
		}
	}
	return 0
}

// fontconfigDistance returns how far a face of weight have lies from a
// request of weight want in fontconfig's weight scale, in 1/64 units: the
// request's value truncated to a whole one, as Chromium hands it to
// fontconfig, against the face's.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1, on screen and in
//     print, draws an installed static family of 400, 500, 600 and 700
//     faces (Fira Sans) at font-weight 401 to 449 with the 400 face, 450
//     to 551.5 with the 500 one, 552 to 649 with the 600 one and from 650
//     with the 700 one: the request at 551 is 140.8 in fontconfig's scale,
//     as far from 100 as from 180 once truncated, at 552 141.6, nearer
//     180.
func fontconfigDistance(want, have int) int {
	return int(math.Round(math.Abs(fontconfigWeight(float64(have))-math.Trunc(fontconfigWeight(float64(want)))) * 64))
}

// instanceWeightRank ranks the named instance (or default instance) of a
// variable file of weight have for a request of weight want, lower first.
// Chromium takes the instance fontconfig finds nearest in its weight
// scale, the lighter on a tie. LibreOffice ranks by its weight classes,
// one each hundred up to 900, and takes the heavier on a tie; the other
// rules follow it, Word's choice being unknown. CSS Fonts 4, 5.2, step
// 4c, which [rankFace] follows for static faces, would take a heavier
// face first above 500: both programs depart from it for the instances of
// one file.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws Roboto Flex at
//     font-weight 250, 550, 650, 750 and 950 with the 200, 500, 600, 700
//     and 900 named instances, at 330 and 370 with 300 and 400, at 480,
//     520, 580, 620 and 680 with 500, 500, 600, 600 and 700.
//   - probe, odt: LibreOffice 26.8 in the same image draws fo:font-weight
//     100 to 800 with the named instance of that weight and 900 with the
//     ExtraBlack instance, wght 1000.
func instanceWeightRank(want, have int, rule Fallback) (int, int) {
	if rule == FallbackChromium {
		d := fontconfigDistance(want, have)
		lighter := 0
		if have > want {
			lighter = 1
		}
		return d, lighter
	}
	class := func(w int) int { return min(max((w+50)/100, 1), 9) }
	return intAbs(class(have) - class(want)), -have
}

// intAbs returns the absolute value of v.
func intAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// instanceStore holds the instances of variable faces one conversion
// draws (see [Fonts.ForConversion]): those of installed faces, by face of
// the file and coordinates, and the count every instance of the
// conversion, of document faces too, is bounded by. It never outlives the
// conversion and never reaches another one.
type instanceStore struct {
	count instanceCount

	mu sync.Mutex
	// designs holds the design space of each variable file drawn, read
	// from its program; errs why a file does not instance.
	designs   map[*Face]*varfont.Font
	errs      map[*Face]error
	instances map[instanceKey]*Face
}

// instanceKey names one instance of an installed face.
type instanceKey struct {
	file   *Face
	coords string
}

// ForConversion returns an index for one conversion: it matches and lays
// out as f, and keeps the instances of the variable faces the conversion
// draws, which belong to it alone (see [Fonts.Layout]). f is not
// modified. On an index ForConversion or [Fonts.WithFaces] returned, and
// on nil, it returns f itself.
func (f *Fonts) ForConversion() *Fonts {
	if f == nil || f.inst != nil {
		return f
	}
	return f.overlay(f.set, f.plain)
}

// WithPlainGlyphs returns an index for one conversion that draws no colour
// glyph: it matches and lays out as f, except that every emoji takes text
// presentation, an emoji presentation selector (VS16, U+FE0F) included, so
// a face without colour glyphs draws it where one covers it; that a colour
// face draws a character only from an outline of its own, in the run's
// colour, and counts as not covering a character whose glyph has none, a
// bitmap-only face covering nothing; and that the runs it lays out are
// plain ([typeset.Text.Plain]). A character left without a face is
// reported missing as any other. f is not modified. On an index
// WithPlainGlyphs returned, and on nil, it returns f itself.
func (f *Fonts) WithPlainGlyphs() *Fonts {
	if f == nil || f.plain {
		return f
	}
	return f.overlay(f.set, true)
}

// overlay returns an index of one conversion over the index f shares: its
// document faces are set, its instances those of f when f is itself an
// overlay.
func (f *Fonts) overlay(set *FaceSet, plain bool) *Fonts {
	base := f
	if f.base != nil {
		base = f.base
	}
	inst := f.inst
	if inst == nil {
		inst = &instanceStore{}
	}
	return &Fonts{
		logger:         base.logger,
		open:           base.open,
		faces:          base.faces,
		byFamily:       base.byFamily,
		maxLoaded:      base.maxLoaded,
		maxLoadedBytes: base.maxLoadedBytes,
		base:           base,
		set:            set,
		inst:           inst,
		plain:          plain,
	}
}

// installedUserCoordinates returns the user coordinates an installed face
// of a variable file is drawn at in style: those of its named instance, or
// of the default instance; then, except under [FallbackChromium], opsz at
// the font size in points, clamped to the axis when the coordinates are
// normalized.
//
// CSS Fonts 4, 7.2 applies font-weight, font-stretch, font-style,
// font-optical-sizing and font-variation-settings to the axes of any
// variable font; Chromium departs from it for an installed one, in print
// and on screen alike, and HTML follows Chromium.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (Gotenberg 9.4.1) draws an installed Roboto
//     Flex at the outlines of a named instance whatever font-stretch (75%,
//     125%), font-variation-settings ("wght" 650; "opsz" 48, "wdth" 75) and
//     optical sizing (24px, 48px, 96px) say, at opsz 14, its default;
//     screenshots differ by 0 pixels between "wght" 650 and font-weight
//     400, and between font-optical-sizing auto and none at 96px.
//   - probe, odt: LibreOffice 26.8 in the same image embeds Roboto Flex at
//     opsz 9, 11.5, 18 and 48 for text of those sizes in points, and at
//     144, the axis's maximum, for 200 points, beside the wght and slnt
//     of the named instance it matched.
func (fc *Face) installedUserCoordinates(style *Style) []float64 {
	user := fc.named
	if user == nil {
		user = fc.axes.Defaults()
	} else {
		user = append([]float64(nil), user...)
	}
	if style.Fallback.rule() != FallbackChromium {
		if i := fc.axes.Index(axisOpticalSize); i >= 0 {
			user[i] = style.Size
		}
	}
	return user
}

// installedInstance returns the face that draws fc, a face of the index,
// in style: fc itself for a static face; the instance of its file at the
// coordinates [Face.installedUserCoordinates] gives, created on first use
// in the conversion; or the face of its file, its default instance, at the
// default coordinates, except for a file with CFF2 outlines, which draws
// its instance there too (see [Face.instanceOf]). A file litho cannot
// instance draws at its default instance, returned with why.
func (f *Fonts) installedInstance(ctx context.Context, fc *Face, style *Style, mem *limits.Budget) (*Face, error) {
	if fc.axes == nil {
		return fc, nil
	}
	file := fc.fileFace()
	store := f.inst
	if store == nil {
		if fc.named == nil && style.Fallback.rule() == FallbackChromium {
			return fc, nil
		}
		return file, errNoConversion
	}
	user := fc.installedUserCoordinates(style)
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.errs[file]; err != nil {
		return file, err
	}
	vf, err := store.design(ctx, file)
	if err != nil {
		return file, err
	}
	coords := vf.Normalize(user)
	if !slicesNonZero(coords) && !vf.HasCFF2() {
		return file, nil
	}
	key := instanceKey{file: file, coords: coordsKey(coords)}
	if inst, ok := store.instances[key]; ok {
		return inst, nil
	}
	if !store.count.take() {
		return file, errInstanceLimit
	}
	inst, err := file.newInstance(ctx, vf, user, coords, key.coords, mem, f.loadFont)
	if err != nil {
		if ctx.Err() == nil && !errors.Is(err, limits.ErrMemoryBudget) {
			store.fail(file, err)
		}
		return file, err
	}
	// The instance keeps the aspect the face was matched by: an instance
	// program states no italic in its OS/2 table, and synthesis must not
	// slant a slanted instance again.
	inst.weight, inst.slant, inst.width = fc.weight, fc.slant, fc.width
	if store.instances == nil {
		store.instances = make(map[instanceKey]*Face)
	}
	store.instances[key] = inst
	return inst, nil
}

// fail records that file does not instance, because of err. s.mu is
// held.
func (s *instanceStore) fail(file *Face, err error) {
	if s.errs == nil {
		s.errs = make(map[*Face]error)
	}
	s.errs[file] = err
}

// design returns the design space of file, a variable face of the index,
// read from its program on its first use in the conversion. s.mu is
// held.
func (s *instanceStore) design(ctx context.Context, file *Face) (*varfont.Font, error) {
	if vf, ok := s.designs[file]; ok {
		return vf, nil
	}
	tf, err := file.Typeset(ctx)
	if err != nil {
		return nil, err
	}
	vf, err := varfont.Parse(tf.Program)
	if err != nil {
		err = fmt.Errorf("reading the font variations: %w", err)
		s.fail(file, err)
		return nil, err
	}
	if s.designs == nil {
		s.designs = make(map[*Face]*varfont.Font)
	}
	s.designs[file] = vf
	return vf, nil
}
