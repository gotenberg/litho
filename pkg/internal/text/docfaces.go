package text

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
)

// ErrNoLocalFace reports a local() source no indexed face answers to.
var ErrNoLocalFace = errors.New("no indexed face has that full or PostScript name")

// FaceStyle is the font-style an @font-face rule declares (CSS Fonts 4,
// 4.4).
type FaceStyle uint8

// The font-style values of a face.
const (
	FaceNormal FaceStyle = iota
	FaceItalic
	FaceOblique
)

// FaceDescriptor is what an @font-face rule says of the face it loads:
// the family it joins and the aspect and characters it claims (CSS Fonts 4,
// 4.4 to 4.7). Matching reads these, not the font's own tables.
type FaceDescriptor struct {
	// Family is the family name the rule defines.
	Family string
	// WeightMin and WeightMax bound the weights the face covers, from 1 to
	// 1000. Zero values mean 400.
	WeightMin, WeightMax int
	// Style is the face's style; ObliqueMin and ObliqueMax bound the slant
	// angles, in degrees, of an oblique face.
	Style                  FaceStyle
	ObliqueMin, ObliqueMax float64
	// StretchMin and StretchMax bound the widths the face covers, in
	// percent of the normal width. Zero values mean 100.
	StretchMin, StretchMax float64
	// UnicodeRange lists the inclusive character ranges the face is used
	// for, nil for every character.
	UnicodeRange [][2]rune
}

// normalized returns d with its zero values defaulted and its ranges in
// increasing order.
func (d FaceDescriptor) normalized() FaceDescriptor {
	if d.WeightMin == 0 && d.WeightMax == 0 {
		d.WeightMin, d.WeightMax = 400, 400
	}
	d.WeightMin, d.WeightMax = min(d.WeightMin, d.WeightMax), max(d.WeightMin, d.WeightMax)
	if d.StretchMin == 0 && d.StretchMax == 0 {
		d.StretchMin, d.StretchMax = 100, 100
	}
	d.StretchMin, d.StretchMax = min(d.StretchMin, d.StretchMax), max(d.StretchMin, d.StretchMax)
	d.ObliqueMin, d.ObliqueMax = min(d.ObliqueMin, d.ObliqueMax), max(d.ObliqueMin, d.ObliqueMax)
	d.UnicodeRange = slices.Clone(d.UnicodeRange)
	return d
}

// inRange reports whether the face is used for r (CSS Fonts 4, 4.7).
func (d *FaceDescriptor) inRange(r rune) bool {
	if d.UnicodeRange == nil {
		return true
	}
	for _, ur := range d.UnicodeRange {
		if r >= ur[0] && r <= ur[1] {
			return true
		}
	}
	return false
}

// Chromium's thresholds for synthesising a document face
// (CSSSegmentedFontFace, FontSelectionValue): bold for a requested weight
// of at least 600 on a face whose weight range stops below it; italic on a
// face whose slant stops below 20 degrees, the slope an italic face
// counts as.
const (
	syntheticBoldThreshold  = 600
	syntheticItalicMinSlope = 20
)

// synthesis returns the stroke width and skew that synthesise what req
// asks for and the face lacks, at size points (CSS Fonts 4, 5.5
// font-synthesis, weight and style allowed by default), as Chromium decides
// for a document face: by the rule's descriptors, not the font's tables.
// Bold is synthesised when the requested weight is at least 600 and the
// rule's weight range ends below 600; italic when an italic face is asked
// for and the rule declares a normal face or an oblique one whose angles
// stop below 20 degrees, so oblique 10deg is slanted again as Chromium
// slants it.
func (d *FaceDescriptor) synthesis(req request, size float64) (stroke, skew float64) {
	if req.weight >= syntheticBoldThreshold && d.WeightMax < syntheticBoldThreshold {
		stroke = fakeBoldStroke(size)
	}
	upright := d.Style == FaceNormal || (d.Style == FaceOblique && d.ObliqueMax < syntheticItalicMinSlope)
	if req.slant != slantNormal && upright {
		skew = syntheticItalicSkew
	}
	return stroke, skew
}

// memFile serves a font program held in memory as a [sfntFile].
type memFile struct {
	*bytes.Reader
}

// Close does nothing.
func (memFile) Close() error { return nil }

// LoadFace parses program, an sfnt font program or collection, into a
// document face with the descriptors d, for [NewFaceSet]. A collection
// yields its first face litho can use.
//
// The face is checked as [NewFonts] checks an indexed face (outlines
// present, no construct go-text cannot parse safely) and parsed once; it
// is never written to an index's cache, so it lives as long as the caller
// holds it. Its [Face.Key] derives from a hash of program, so two
// documents loading the same bytes embed the same font. LoadFace keeps
// program and the caller must not modify it.
//
// It returns an error for a program past the index's per-file bounds, one
// it cannot read and one without a usable face.
func LoadFace(program []byte, d FaceDescriptor) (*Face, error) {
	size := int64(len(program))
	if size > maxFontCollectionBytes {
		return nil, fmt.Errorf("a font of %d bytes past the %d-byte bound", size, maxFontCollectionBytes)
	}
	sum := sha256.Sum256(program)
	src := &fontSource{
		path: "font-face:" + hex.EncodeToString(sum[:]),
		open: func(string) (sfntFile, int64, error) {
			return memFile{bytes.NewReader(program)}, size, nil
		},
	}
	r := &boundedReader{r: memFile{bytes.NewReader(program)}, left: maxFootprintFileBytes}
	faces, err := readFootprints(slog.New(slog.DiscardHandler), src, r, size)
	if err != nil {
		return nil, fmt.Errorf("reading the font: %w", err)
	}
	if len(faces) == 0 {
		return nil, errors.New("the font holds no face litho can use")
	}
	return pinFace(faces[0], d)
}

// pinFace parses the font of fc, a face just read, and makes it a document
// face with the descriptors d.
func pinFace(fc *Face, d FaceDescriptor) (*Face, error) {
	ft, err := fc.parseFont()
	if err != nil {
		return nil, fmt.Errorf("parsing the font: %w", err)
	}
	nd := d.normalized()
	fc.pinned, fc.doc = ft, &nd
	// A document face joins its rule's family alone: no installed family
	// of the same name, and no index fallback, reaches it.
	fc.keys = nil
	return fc, nil
}

// LocalFace returns the indexed face whose full name (name ID 4) or
// PostScript name (name ID 6) is name, compared without regard to case,
// as a document face with the descriptors d: the face a CSS local() source
// names (CSS Fonts 4, 4.3). The face draws and embeds as the indexed one,
// [Face.Key] included, but matches by d. It returns an error wrapping
// [ErrNoLocalFace] when no face has that name, and an error when the face's
// file no longer reads.
func (f *Fonts) LocalFace(name string, d FaceDescriptor) (*Face, error) {
	want := strings.ToLower(strings.TrimSpace(name))
	if want == "" {
		return nil, ErrNoLocalFace
	}
	for _, ix := range f.faces {
		if strings.ToLower(strings.TrimSpace(ix.fullName)) != want && strings.ToLower(strings.TrimSpace(ix.postScript)) != want {
			continue
		}
		fc := &Face{
			src: ix.src, index: ix.index, dir: ix.dir, order: ix.order,
			family: ix.family, postScript: ix.postScript, fullName: ix.fullName,
			weight: ix.weight, slant: ix.slant, width: ix.width,
			ascent: ix.ascent, descent: ix.descent, lineGap: ix.lineGap,
			xHeight: ix.xHeight, upem: ix.upem, coverage: ix.coverage, size: ix.size,
		}
		return pinFace(fc, d)
	}
	return nil, fmt.Errorf("%w: %q", ErrNoLocalFace, name)
}

// FaceSet is the document faces of one conversion, grouped by family, for
// [Fonts.WithFaces]. It is immutable and safe for concurrent use.
type FaceSet struct {
	// families maps a family key to its faces in definition order.
	families map[string][]*Face
	faces    int
}

// NewFaceSet groups faces, document faces from [LoadFace] and
// [Fonts.LocalFace] in the order their rules appear, by the family their
// descriptors name. Faces of the index are skipped. Every family a face
// names is declared: a family whose faces all failed to load is declared
// by the caller with [FaceSet.Declare].
func NewFaceSet(faces []*Face) *FaceSet {
	s := &FaceSet{families: make(map[string][]*Face)}
	for _, fc := range faces {
		if fc == nil || fc.doc == nil {
			continue
		}
		k := familyKey(fc.doc.Family)
		if k == "" {
			continue
		}
		s.families[k] = append(s.families[k], fc)
		s.faces++
	}
	return s
}

// Declare records family as defined by the document even though none of
// its faces loaded. CSS Fonts 4, 5 (Font Matching Algorithm), looks a
// family name up among the @font-face rules first; Chromium then never
// falls back to an installed family of the same name, so a declared
// family without a face moves on to the next family of the list. Declare
// returns s.
func (s *FaceSet) Declare(family string) *FaceSet {
	if k := familyKey(family); k != "" {
		if _, ok := s.families[k]; !ok {
			s.families[k] = nil
		}
	}
	return s
}

// Len returns the number of faces in s.
func (s *FaceSet) Len() int {
	if s == nil {
		return 0
	}
	return s.faces
}

// WithFaces returns an index that matches and lays out with the document
// faces of set ahead of f's faces, for one conversion. f is not modified,
// and set's faces never join it, so they never reach another conversion.
//
// A family the document declares shadows the installed family of the same
// name and its metric-compatible aliases: the face is chosen among the
// rule's faces only, by their descriptors (CSS Fonts 4, 5.2). A character
// outside every unicode-range of the chosen faces, or that none of them
// maps, falls to the next family of the list, then to f's fallback.
// Generic families and the aliases of other families resolve against f
// alone, as Chromium resolves them against the installed fonts.
//
// The result shares f's footprints and cache of parsed fonts without
// copying them, and is safe for concurrent use. A nil or empty set returns
// f itself.
func (f *Fonts) WithFaces(set *FaceSet) *Fonts {
	if set == nil || len(set.families) == 0 {
		return f
	}
	base := f
	if f.base != nil {
		base = f.base
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
	}
}

// familyRef is one entry of a family list with document families in
// place: the faces of a document family, or the key of an installed one.
type familyRef struct {
	// doc is true for a document family, whose faces are faces; key is
	// the installed family's key otherwise.
	doc   bool
	faces []*Face
	key   string
	// selected are the faces of a document family that match the
	// request, last defined first (see [selectRefs]).
	selected []*Face
}

// familyRefs returns the families style asks for with the document
// families of f in place, nil when f has no document face or style names
// no document family. Installed families expand as [requestedFamilies]
// expands them; a document family is not expanded.
func (f *Fonts) familyRefs(style Style) []familyRef {
	if f.set == nil {
		return nil
	}
	named := false
	for _, name := range style.Families {
		if genericOf(name) == genericNone {
			if _, ok := f.set.families[familyKey(name)]; ok {
				named = true
				break
			}
		}
	}
	if !named {
		return nil
	}
	region := cjkRegion(style.Lang)
	var refs []familyRef
	seenDoc := map[string]bool{}
	var l familyList
	flush := func(from int) {
		for _, k := range l.keys[from:] {
			refs = append(refs, familyRef{key: k})
		}
	}
	for _, name := range style.Families {
		if g := genericOf(name); g != genericNone {
			n := len(l.keys)
			l.addGeneric(g, region)
			flush(n)
			continue
		}
		k := familyKey(name)
		if faces, ok := f.set.families[k]; ok {
			if !seenDoc[k] {
				seenDoc[k] = true
				refs = append(refs, familyRef{doc: true, faces: faces})
			}
			continue
		}
		n := len(l.keys)
		l.addWithAliases(name)
		flush(n)
	}
	return refs
}

// indexKeys returns the keys of the installed families of refs, in order.
func indexKeys(refs []familyRef) []string {
	var keys []string
	for _, r := range refs {
		if !r.doc {
			keys = append(keys, r.key)
		}
	}
	return keys
}

// selectRefs fills the selected faces of every document family of refs for
// req.
func selectRefs(refs []familyRef, req request) {
	for i := range refs {
		if refs[i].doc {
			refs[i].selected = selectFaces(refs[i].faces, req)
		}
	}
}

// matchRefs is [Fonts.Match] for a style that names a document family: the
// first face of the list whose unicode-range includes U+0020 SPACE, the
// first available font of CSS Fonts 4, 5 (a document face need not map
// it), then the sans-serif list and the first indexed face.
func (f *Fonts) matchRefs(style Style, refs []familyRef) *Face {
	req := requestOf(style)
	for _, ref := range refs {
		if ref.doc {
			for _, fc := range selectFaces(ref.faces, req) {
				if fc.doc.inRange(' ') {
					return fc
				}
			}
			continue
		}
		if fc := f.bestInFamily(ref.key, req, ' '); fc != nil {
			return fc
		}
	}
	var l familyList
	l.addGeneric(genericSansSerif, cjkRegion(style.Lang))
	for _, key := range l.keys {
		if fc := f.bestInFamily(key, req, ' '); fc != nil {
			return fc
		}
	}
	return f.faces[0]
}

// resolveRefs returns the face of the first family of s's list that draws
// r: a selected document face whose unicode-range includes r and whose
// font maps it (CSS Fonts 4, 4.7 and 5.2, step 6), or the installed face of
// a family closest in aspect that maps it. It loads at most
// [maxFallbackLoads] installed faces; nil means none draws r.
func (st *layoutState) resolveRefs(s *styleState, r rune) *Face {
	loads := 0
	for _, ref := range s.refs {
		if ref.doc {
			for _, fc := range ref.selected {
				if fc.doc.inRange(r) && st.covers(fc, r) {
					return fc
				}
			}
			continue
		}
		fc := st.fonts.bestInFamily(ref.key, s.req, r)
		if fc == nil {
			continue
		}
		if loads >= maxFallbackLoads {
			return nil
		}
		loads++
		if st.covers(fc, r) {
			return fc
		}
	}
	return nil
}

// descRank orders a face for a request, lower first, field by field.
type descRank [2]float64

// less reports whether a ranks before b.
func (a descRank) less(b descRank) bool {
	return a[0] < b[0] || (a[0] == b[0] && a[1] < b[1])
}

// stretchRank ranks d for a normal width (CSS Fonts 4, 5.2, step 4a,
// font-stretch 100%): a range including it, then narrower faces, nearest
// first, then wider ones.
func stretchRank(d *FaceDescriptor) descRank {
	const want = 100
	switch {
	case d.StretchMin <= want && want <= d.StretchMax:
		return descRank{0, 0}
	case d.StretchMax < want:
		return descRank{1, want - d.StretchMax}
	}
	return descRank{2, d.StretchMin - want}
}

// italicAngle is the angle an italic request ranks oblique faces against:
// the default oblique angle (CSS Fonts 4, 2.4).
const italicAngle = 14

// styleRank ranks d for req (CSS Fonts 4, 5.2, step 4b). An italic request
// takes italic faces, then oblique faces with a positive angle, closest to
// 14 degrees first, then normal faces, then the other oblique faces. A
// normal request takes normal faces and oblique ranges including 0, then
// oblique faces with a positive angle, smallest first, then italic faces,
// then oblique faces with a negative angle, closest to 0 first.
func styleRank(d *FaceDescriptor, req request) descRank {
	if req.slant != slantNormal {
		switch {
		case d.Style == FaceItalic:
			return descRank{0, 0}
		case d.Style == FaceOblique && d.ObliqueMax > 0:
			lo, hi := d.ObliqueMin, d.ObliqueMax
			if lo <= italicAngle && italicAngle <= hi {
				return descRank{1, 0}
			}
			return descRank{1, math.Min(math.Abs(lo-italicAngle), math.Abs(hi-italicAngle))}
		case d.Style == FaceNormal:
			return descRank{2, 0}
		}
		return descRank{3, -d.ObliqueMax}
	}
	switch {
	case d.Style == FaceNormal, d.Style == FaceOblique && d.ObliqueMin <= 0 && d.ObliqueMax >= 0:
		return descRank{0, 0}
	case d.Style == FaceOblique && d.ObliqueMin > 0:
		return descRank{1, d.ObliqueMin}
	case d.Style == FaceItalic:
		return descRank{2, 0}
	}
	return descRank{3, -d.ObliqueMax}
}

// weightRank ranks d for req (CSS Fonts 4, 5.2, step 4c): a range
// including the weight first. For a weight from 400 to 500, heavier faces
// up to 500 follow, nearest first, then lighter ones, then heavier ones
// past 500; below 400 lighter faces come first, above 500 heavier ones.
func weightRank(d *FaceDescriptor, req request) descRank {
	w := float64(req.weight)
	lo, hi := float64(d.WeightMin), float64(d.WeightMax)
	if lo <= w && w <= hi {
		return descRank{0, 0}
	}
	switch {
	case w >= 400 && w <= 500:
		switch {
		case lo > w && lo <= 500:
			return descRank{1, lo - w}
		case hi < w:
			return descRank{2, w - hi}
		}
		return descRank{3, lo - w}
	case w < 400:
		if hi < w {
			return descRank{1, w - hi}
		}
		return descRank{2, lo - w}
	}
	if lo > w {
		return descRank{1, lo - w}
	}
	return descRank{2, w - hi}
}

// selectFaces returns the faces of a document family that match req, the
// set CSS Fonts 4, 5.2, step 4 narrows the family to: the faces closest in
// width, then among them in style, then in weight. Several rules with the
// same descriptors and different unicode-ranges form one composite face;
// the last rule defined is checked first (CSS Fonts 4, 4.7), so the result
// runs in reverse definition order.
func selectFaces(faces []*Face, req request) []*Face {
	cands := slices.Clone(faces)
	for _, rank := range []func(*FaceDescriptor) descRank{
		stretchRank,
		func(d *FaceDescriptor) descRank { return styleRank(d, req) },
		func(d *FaceDescriptor) descRank { return weightRank(d, req) },
	} {
		if len(cands) == 0 {
			break
		}
		best := rank(cands[0].doc)
		for _, fc := range cands[1:] {
			if r := rank(fc.doc); r.less(best) {
				best = r
			}
		}
		kept := cands[:0]
		for _, fc := range cands {
			if rank(fc.doc) == best {
				kept = append(kept, fc)
			}
		}
		cands = kept
	}
	slices.Reverse(cands)
	return cands
}
