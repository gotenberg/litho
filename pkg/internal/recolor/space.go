package recolor

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/function"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// maxSpaceDepth bounds how deep colour space definitions nest: a Pattern
// space over an Indexed space over a Separation over its alternate is three.
// Anti-DoS guard, not spec-mandated: ISO 32000-1, 8.6.6 forbids every deeper
// nesting, and the bound only stops a definition that names itself.
const maxSpaceDepth = 4

// maxPlanKeyBytes bounds the key a colour space's plan is cached under.
// Anti-DoS guard, not spec-mandated: the key spells the definition, which for
// an inline palette runs to 768 bytes, and a definition whose key would pass
// it is read again at each selection instead.
const maxPlanKeyBytes = 4096

// planKind is what recolouring does to the colour of one colour space.
type planKind int

const (
	// planIdentity leaves the space's colour and definition as they are:
	// the space already is the target, or paints no colorant.
	planIdentity planKind = iota
	// planConvert converts the space's colour to the target, and the space
	// is replaced by the target wherever it is selected.
	planConvert
	// planKeep keeps the space and its colour values, and rewrites its
	// definition: an Indexed space's palette, a spot colour's alternate.
	planKeep
	// planPattern is a Pattern space, whose patterns convert on their own and
	// whose base space, if any, plans the colour of an uncoloured pattern.
	planPattern
	// planUnknown is a space recolouring cannot read; its colour is left.
	planUnknown
)

// spacePlan is how recolouring treats one colour space.
type spacePlan struct {
	kind planKind
	// n is the number of components a colour in the space carries.
	n int
	// conv converts the space's components to the target's (planConvert).
	conv Converter
	// initial is the space's initial colour, which cs and CS set (ISO
	// 32000-1, 8.6.8, Table 74).
	initial []float64
	// def is the rewritten definition (planKeep, planPattern), or nil when
	// the definition stays as it is.
	def ast.PdfObject
	// base plans the base space of a Pattern space; nil for a coloured
	// pattern's space.
	base *spacePlan
	// spot marks a Separation or DeviceN space kept under the spot policy.
	spot bool
	// fixed marks a Separation or DeviceN space over All or None, whose
	// tints stay under either policy and no conversion carries.
	fixed bool
	// spotConverted marks a Separation or DeviceN space converted to
	// process colour, and cmyk a space overprint mode 1 governs, DeviceCMYK
	// or a four-component ICCBased space (see [contentRewriter.judgeColor]).
	spotConverted, cmyk bool
	// deviceGray and deviceRGB mark the plans of DeviceGray and DeviceRGB
	// read as the device space, no default space standing in (ISO 32000-1,
	// 8.6.5.6), whose solid colours [Options.KOnlyBlack] converts to the
	// black colorant alone ([session.solidPlan]), and kOnly the plans it
	// converts them with.
	deviceGray, deviceRGB, kOnly bool
	// err is why a planUnknown space could not be read.
	err error
}

// convertible reports a plan whose colour values change.
func (p *spacePlan) convertible() bool {
	return p.kind == planConvert
}

// planName returns the plan of the colour space a cs or CS operand names: a
// family name, or a /ColorSpace resource of res (ISO 32000-1, 8.6.3).
func (s *session) planName(ctx context.Context, name ast.PdfName, res ast.PdfDict) *spacePlan {
	switch string(name) {
	case graphics.FamilyDeviceGray, graphics.FamilyDeviceRGB, graphics.FamilyDeviceCMYK, graphics.FamilyPattern:
		return s.planDefinition(ctx, name, res)
	}
	spaces, _ := s.resolveDict(ctx, res["ColorSpace"])
	def, ok := spaces[name]
	if !ok {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: no /ColorSpace resource %q", ErrUnsupportedSpace, string(name))}
	}
	return s.planDefinition(ctx, def, res)
}

// planDefinition returns the plan of the colour space definition def as it
// is selected for painting under the resources res, whose /DefaultGray,
// /DefaultRGB and /DefaultCMYK stand in for a device space so selected (ISO
// 32000-1, 8.6.5.6). res may be nil for a definition no resources govern.
// Plans are cached per definition and default spaces.
func (s *session) planDefinition(ctx context.Context, def ast.PdfObject, res ast.PdfDict) *spacePlan {
	var defaults ast.PdfDict
	if res != nil {
		defaults, _ = s.resolveDict(ctx, res["ColorSpace"])
	}
	key, cacheable := s.planKey(ctx, def, defaults)
	if cacheable {
		if p, ok := s.plans[key]; ok {
			return p
		}
	}
	p := s.plan(ctx, def, defaults, 0, pdfimage.RoleSpace)
	if cacheable && ctx.Err() == nil {
		s.plans[key] = p
	}
	return p
}

// planKey spells what a plan depends on: the definition, the default spaces
// that may stand in for it, and a black point compensation other than the
// option's.
func (s *session) planKey(ctx context.Context, def ast.PdfObject, defaults ast.PdfDict) (string, bool) {
	var sb strings.Builder
	resolved, _ := s.resolve(ctx, def)
	// The resolved definition alone: a reference and the object it names are
	// one space, and planned once.
	fmt.Fprintf(&sb, "%v", resolved)
	for _, name := range []ast.PdfName{"DefaultGray", "DefaultRGB", "DefaultCMYK"} {
		if d, ok := defaults[name]; ok {
			fmt.Fprintf(&sb, "|%s=%v", name, d)
		}
	}
	sb.WriteString(s.variantKey())
	return sb.String(), sb.Len() <= maxPlanKeyBytes
}

// plan reads the definition def at nesting depth, in role: the space
// selected, an Indexed base or a spot colour's alternate, which limits the
// /Alternate an unusable ICCBased profile gives way to there. defaults is the
// /ColorSpace resource dictionary whose default spaces stand in for a device
// space selected directly; a base or alternate space is read as written.
func (s *session) plan(ctx context.Context, def ast.PdfObject, defaults ast.PdfDict, depth int, role pdfimage.SpaceRole) *spacePlan {
	unknown := func(err error) *spacePlan {
		return &spacePlan{kind: planUnknown, err: err}
	}
	if depth > maxSpaceDepth {
		return unknown(fmt.Errorf("%w: definitions nest deeper than %d", ErrUnsupportedSpace, maxSpaceDepth))
	}
	resolved, err := s.resolve(ctx, def)
	if err != nil {
		return unknown(fmt.Errorf("%w: %w", ErrUnsupportedSpace, err))
	}
	var arr ast.PdfArray
	var family ast.PdfName
	switch v := resolved.(type) {
	case ast.PdfName:
		family = v
	case ast.PdfArray:
		if len(v) == 0 {
			return unknown(fmt.Errorf("%w: empty array", ErrUnsupportedSpace))
		}
		arr = v
		family, _ = s.resolveName(ctx, v[0])
	default:
		return unknown(fmt.Errorf("%w: %T", ErrUnsupportedSpace, resolved))
	}

	switch string(family) {
	case graphics.FamilyDeviceGray, "G":
		return s.planDevice(ctx, SpaceGray, "DefaultGray", defaults)
	case graphics.FamilyDeviceRGB, "RGB":
		return s.planDevice(ctx, SpaceRGB, "DefaultRGB", defaults)
	case graphics.FamilyDeviceCMYK, "CMYK":
		return markCMYK(s.planDevice(ctx, SpaceCMYK, "DefaultCMYK", defaults))
	case graphics.FamilyCalGray, graphics.FamilyCalRGB, graphics.FamilyLab:
		return s.planCIE(ctx, string(family), arr)
	case graphics.FamilyICCBased:
		p := s.planICC(ctx, arr, depth, role)
		if p.n == 4 {
			return markCMYK(p)
		}
		return p
	case graphics.FamilyIndexed, "I":
		return s.planIndexed(ctx, arr, depth)
	case graphics.FamilySeparation, graphics.FamilyDeviceN:
		p := s.planSpot(ctx, string(family), arr, depth, s.opts.ConvertSpots)
		if p.kind == planConvert {
			q := *p
			q.spotConverted = true
			return &q
		}
		return p
	case graphics.FamilyPattern:
		p := &spacePlan{kind: planPattern}
		if len(arr) > 1 {
			p.base = s.plan(ctx, arr[1], nil, depth+1, pdfimage.RoleSpace)
			p.n = p.base.n
			switch {
			case p.base.kind == planConvert:
				p.def = ast.PdfArray{ast.PdfName(graphics.FamilyPattern), s.t.spaceObject()}
			case p.base.def != nil:
				p.def = ast.PdfArray{ast.PdfName(graphics.FamilyPattern), p.base.def}
			}
		}
		return p
	}
	return unknown(fmt.Errorf("%w: family %q", ErrUnsupportedSpace, string(family)))
}

// markCMYK returns a copy of p marked as a space overprint mode 1 governs.
func markCMYK(p *spacePlan) *spacePlan {
	q := *p
	q.cmyk = true
	return &q
}

// planDevice plans a device space: the default space of the resources when
// they define one (ISO 32000-1, 8.6.5.6), else the profile the options give
// the family, the target's for the target's own family, or the built-in one.
func (s *session) planDevice(ctx context.Context, space, defaultName string, defaults ast.PdfDict) *spacePlan {
	if d, ok := defaults[ast.PdfName(defaultName)]; ok {
		// A default space that is no CIE-based space of the family's
		// component count is ignored, as 8.6.5.6 requires it to be one.
		p := s.plan(ctx, d, nil, 1, pdfimage.RoleSpace)
		if p.kind == planConvert || p.kind == planIdentity {
			if p.n == (Endpoint{Space: space}).components() {
				return s.notingPlan(p, space, ProfileDefaultSpace)
			}
		}
	}
	var profile []byte
	switch space {
	case SpaceGray:
		profile = s.opts.DeviceGray
	case SpaceRGB:
		profile = s.opts.DeviceRGB
	case SpaceCMYK:
		profile = s.opts.DeviceCMYK
	}
	provenance := ProfileGiven
	if profile == nil {
		provenance = ProfileBuiltin
		if s.t.device() && s.t.end.Space == space {
			profile, provenance = s.t.end.Profile, ProfileTarget
		}
	}
	end := Endpoint{Space: space, Profile: defaultProfile(space, profile)}
	n := end.components()
	initial := make([]float64, n)
	if space == SpaceCMYK {
		initial[3] = 1
	}
	p := s.planEndpoint(end, nil, initial)
	if p.kind == planUnknown && profile != nil && ctx.Err() == nil {
		// A profile the engine builds no transform from, such as an output
		// intent's printer profile without an AToB table, gives way to the
		// built-in one, as the renderer reads the device space.
		p = s.planEndpoint(Endpoint{Space: space, Profile: defaultProfile(space, nil)}, nil, initial)
		provenance = ProfileBuiltinFallback
	}
	p = s.notingPlan(p, space, provenance)
	if space == SpaceGray || space == SpaceRGB {
		q := *p
		q.deviceGray, q.deviceRGB = space == SpaceGray, space == SpaceRGB
		return &q
	}
	return p
}

// planEndpoint plans a space whose colour, mapped by pre to the endpoint
// end (nil when the components already are its values), converts through
// the engine. It is the identity when end is the target's own endpoint.
func (s *session) planEndpoint(end Endpoint, pre func(in, out []float64), initial []float64) *spacePlan {
	n := len(initial)
	if pre == nil && end.same(s.t.end) {
		return &spacePlan{kind: planIdentity, n: n, initial: initial}
	}
	tr, err := s.transform(end)
	if err != nil {
		return &spacePlan{kind: planUnknown, n: n, err: err}
	}
	if pre == nil {
		return &spacePlan{kind: planConvert, n: n, conv: tr, initial: initial}
	}
	return &spacePlan{kind: planConvert, n: n, conv: chain{pre: pre, mid: end.components(), next: tr}, initial: initial}
}

// transform returns the engine's transform from end to the target, built once
// per endpoint and black point compensation.
func (s *session) transform(end Endpoint) (Converter, error) {
	sum := sha256.Sum256(end.Profile)
	key := end.Space + string(sum[:]) + s.variantKey()
	if c, ok := s.transforms[key]; ok {
		return c.conv, c.err
	}
	conv, err := s.opts.engine().Transform(end, s.t.end, s.convOptions())
	if err != nil {
		err = fmt.Errorf("building the %q transform: %w", end.Space, err)
	} else {
		s.noteFallbacks(end, conv)
	}
	s.transforms[key] = transformEntry{conv: conv, err: err}
	return conv, err
}

type transformEntry struct {
	conv Converter
	err  error
}

// chain converts through pre into mid components, then through next.
type chain struct {
	pre  func(in, out []float64)
	mid  int
	next Converter
}

func (c chain) Convert(in, out []float64) {
	var buf [maxComponents]float64
	c.pre(in, buf[:c.mid])
	c.next.Convert(buf[:c.mid], out)
}

// maxComponents is the most components a colour carries: DeviceN's 32
// colorants (ISO 32000-1, Annex C, Table C.1).
const maxComponents = 32

// planCIE plans a CalGray, CalRGB or Lab space (ISO 32000-1, 8.6.5.2 to
// 8.6.5.4), whose colour converts to XYZ relative to D50 by the formulas of
// those clauses and the Bradford adaptation from the space's white point. XYZ
// rather than Lab carries it to the engine: a CalRGB primary may lie outside
// the visible gamut, past the a* and b* the Lab encoding holds, and the
// target clips it where the renderer does, in the device space.
// The black point is not applied: 8.6.5.2 lets a conversion ignore it.
func (s *session) planCIE(ctx context.Context, family string, arr ast.PdfArray) *spacePlan {
	if len(arr) < 2 {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: %s without parameters", ErrUnsupportedSpace, family)}
	}
	params, err := s.resolveDict(ctx, arr[1])
	if err != nil || params == nil {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: %s parameters: %v", ErrUnsupportedSpace, family, err)}
	}
	white, ok := s.numbers(ctx, params["WhitePoint"])
	// 8.6.5.2 to 8.6.5.4 require /WhitePoint with Y = 1.
	if !ok || len(white) != 3 || white[1] <= 0 || white[0] <= 0 || white[2] <= 0 {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: %s /WhitePoint", ErrUnsupportedSpace, family)}
	}
	wp := [3]float64{white[0], white[1], white[2]}
	pcs := Endpoint{Space: SpaceXYZ}
	toPCS := func(xyz [3]float64, out []float64) {
		encodeXYZ(adaptToD50(xyz, wp), out)
	}
	switch family {
	case graphics.FamilyCalGray:
		g := 1.0
		// 8.6.5.2, Table 63: a CalGray /Gamma is one positive number.
		if o, err := s.resolve(ctx, params["Gamma"]); err == nil {
			if v, ok := document.Number(o); ok && v > 0 {
				g = v
			}
		}
		return s.planEndpoint(pcs, func(in, out []float64) {
			y := pow(clamp01(in[0]), g)
			toPCS([3]float64{wp[0] * y, wp[1] * y, wp[2] * y}, out)
		}, []float64{0})
	case graphics.FamilyCalRGB:
		gamma := [3]float64{1, 1, 1}
		if v, ok := s.numbers(ctx, params["Gamma"]); ok && len(v) == 3 {
			copy(gamma[:], v)
		}
		m := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
		if v, ok := s.numbers(ctx, params["Matrix"]); ok && len(v) == 9 {
			copy(m[:], v)
		}
		return s.planEndpoint(pcs, func(in, out []float64) {
			a, b, c := pow(clamp01(in[0]), gamma[0]), pow(clamp01(in[1]), gamma[1]), pow(clamp01(in[2]), gamma[2])
			// 8.6.5.3: X = XA·A^GR + XB·B^GG + XC·C^GB, the Matrix columns.
			toPCS([3]float64{
				m[0]*a + m[3]*b + m[6]*c,
				m[1]*a + m[4]*b + m[7]*c,
				m[2]*a + m[5]*b + m[8]*c,
			}, out)
		}, []float64{0, 0, 0})
	}
	rng := []float64{-100, 100, -100, 100}
	if v, ok := s.numbers(ctx, params["Range"]); ok && len(v) == 4 && v[0] <= v[1] && v[2] <= v[3] {
		rng = v
	}
	initial := []float64{0, clamp(0, rng[0], rng[1]), clamp(0, rng[2], rng[3])}
	return s.planEndpoint(pcs, func(in, out []float64) {
		l := clamp(in[0], 0, 100)
		a := clamp(in[1], rng[0], rng[1])
		b := clamp(in[2], rng[2], rng[3])
		toPCS(labToXYZ(l, a, b, wp), out)
	}, initial)
}

// planICC plans an ICCBased space (ISO 32000-1, 8.6.5.5) in role: its
// profile, or, when litho cannot use the profile, its /Alternate, or the
// device space of its component count, as Table 66 has a reader fall back.
//
// A space that reads as the target itself, its profile the target's or its
// fallback the target's family, still converts, as the identity: the
// operators and images selecting it are written in the target's family, so
// the output names no ICCBased space beside the target.
func (s *session) planICC(ctx context.Context, arr ast.PdfArray, depth int, role pdfimage.SpaceRole) *spacePlan {
	p := s.readICC(ctx, arr, depth, role)
	if p.kind == planIdentity && p.n == s.t.n {
		return &spacePlan{kind: planConvert, n: p.n, conv: identity{}, initial: p.initial}
	}
	return p
}

// readICC is [session.planICC] before an identity becomes a conversion.
//
// Whether the profile is usable is the image package's call
// ([pdfimage.ICCBasedUsable]), and so is the alternate an unusable one gives
// way to ([pdfimage.SubstituteICCAlternate]), its admissible families and
// its nesting bound included: the renderer reads the space through both, so a
// colour converts from the colour it is drawn in.
func (s *session) readICC(ctx context.Context, arr ast.PdfArray, depth int, role pdfimage.SpaceRole) *spacePlan {
	if len(arr) < 2 {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: ICCBased without a stream", ErrUnsupportedSpace)}
	}
	stream, err := s.resolveStream(ctx, arr[1])
	if err != nil {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: ICCBased stream: %w", ErrUnsupportedSpace, err)}
	}
	nv, err := document.ResolveInt(ctx, s.doc, stream.Dict["N"])
	n := intconv.Clamp(nv)
	if err != nil || (n != 1 && n != 3 && n != 4) {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: ICCBased /N %d", ErrUnsupportedSpace, n)}
	}
	fallback := func() *spacePlan {
		p := s.planDevice(ctx, [5]string{1: SpaceGray, 3: SpaceRGB, 4: SpaceCMYK}[n], "", nil)
		if alt, ok := pdfimage.SubstituteICCAlternate(ctx, s.doc, arr, s.intent, role); ok {
			if a := s.planICCAlternate(ctx, alt, depth+1, role); a.n == n && (a.kind == planConvert || a.kind == planIdentity) {
				p = a
			}
		}
		// The space is still ICCBased, whose initial colour is 0 in each
		// component (8.6.8, Table 74), whatever its fallback's is.
		q := *p
		q.initial = make([]float64, n)
		// An ICCBased space read through its device family is no device
		// space: [Options.KOnlyBlack] leaves it to the profile.
		q.deviceGray, q.deviceRGB = false, false
		return &q
	}
	if !pdfimage.ICCBasedUsable(ctx, s.doc, arr[1], n, s.intent) {
		if ctx.Err() != nil {
			return &spacePlan{kind: planUnknown, err: ctx.Err()}
		}
		return fallback()
	}
	data, err := s.doc.DecodeStream(ctx, stream)
	if err != nil {
		if ctx.Err() != nil {
			return &spacePlan{kind: planUnknown, err: ctx.Err()}
		}
		return fallback()
	}
	profile, err := icc.Parse(data)
	if err != nil {
		return fallback()
	}
	rng := []float64{0, 1, 0, 1, 0, 1, 0, 1}[:2*n]
	if v, ok := s.numbers(ctx, stream.Dict["Range"]); ok && len(v) == 2*n {
		rng = v
	}
	initial := make([]float64, n)
	for i := range initial {
		initial[i] = clamp(0, rng[2*i], rng[2*i+1])
	}
	var p *spacePlan
	switch {
	case profile.Space == SpaceLab && n == 3:
		// A Lab profile's components are L*, a* and b*, by default over
		// [0 100 -128 127 -128 127] (8.6.5.5, Table 66).
		p = s.planEndpoint(Endpoint{Space: SpaceLab, Profile: data}, func(in, out []float64) {
			encodeLab(in[0], in[1], in[2], out)
		}, initial)
	case (profile.Space == SpaceGray || profile.Space == SpaceRGB || profile.Space == SpaceCMYK) &&
		(Endpoint{Space: profile.Space}).components() == n:
		p = s.planEndpoint(Endpoint{Space: profile.Space, Profile: data}, func(in, out []float64) {
			for i := range n {
				out[i] = clamp01(in[i])
			}
		}, initial)
	default:
		return fallback()
	}
	if p.kind == planUnknown && ctx.Err() == nil {
		// A profile litho reads that the engine builds no transform from
		// converts from the device space of its component count.
		return fallback()
	}
	return p
}

// planICCAlternate plans alt, the /Alternate an ICCBased space whose profile
// litho cannot use is read through (ISO 32000-1, 8.6.5.5, Table 66), in role.
// A Separation or DeviceN alternate converts to the target through its tint
// transform whatever the spot policy: the space selected is the ICCBased
// one, whose colour components and initial colour are its own (8.6.8, Table
// 74), so it cannot keep the spot's tints as a Separation space would, and
// converting is what leaves the space, and a group blending in it (11.4.7,
// Table 147), in the target.
func (s *session) planICCAlternate(ctx context.Context, alt ast.PdfObject, depth int, role pdfimage.SpaceRole) *spacePlan {
	if arr, err := s.resolveArray(ctx, alt); err == nil && len(arr) > 0 {
		if family, err := s.resolveName(ctx, arr[0]); err == nil && (family == graphics.FamilySeparation || family == graphics.FamilyDeviceN) {
			return s.planSpot(ctx, string(family), arr, depth, true)
		}
	}
	return s.plan(ctx, alt, nil, depth, role)
}

// maxIndexedHival is the largest index of an Indexed space, which ISO
// 32000-1, 8.6.6.3 allows: the colour table holds at most 256 entries.
const maxIndexedHival = 255

// planIndexed plans an Indexed space (ISO 32000-1, 8.6.6.3): its indices
// stay, and its palette converts when its base space does.
func (s *session) planIndexed(ctx context.Context, arr ast.PdfArray, depth int) *spacePlan {
	keep := &spacePlan{kind: planIdentity, n: 1, initial: []float64{0}}
	if len(arr) < 4 {
		return &spacePlan{kind: planUnknown, n: 1, err: fmt.Errorf("%w: Indexed of %d elements", ErrUnsupportedSpace, len(arr))}
	}
	base := s.plan(ctx, arr[1], nil, depth+1, pdfimage.RoleIndexedBase)
	switch base.kind {
	case planIdentity:
		return keep
	case planConvert:
	case planUnknown, planPattern, planKeep:
		if base.kind == planKeep && base.spot {
			keep.kind, keep.def, keep.spot = planKeep, ast.PdfArray{ast.PdfName(graphics.FamilyIndexed), base.def, arr[2], arr[3]}, true
			return keep
		}
		return &spacePlan{kind: planUnknown, n: 1, err: fmt.Errorf("%w: Indexed base: %v", ErrUnsupportedSpace, base.err)}
	}
	hv, err := document.ResolveInt(ctx, s.doc, arr[2])
	hival := intconv.Clamp(hv)
	if err != nil || hival < 0 || hival > maxIndexedHival {
		return &spacePlan{kind: planUnknown, n: 1, err: fmt.Errorf("%w: Indexed hival %d", ErrUnsupportedSpace, hival)}
	}
	lookup, err := s.lookupBytes(ctx, arr[3])
	if err != nil {
		return &spacePlan{kind: planUnknown, n: 1, err: err}
	}
	// 8.6.6.3: the table holds m × (hival + 1) bytes; a shorter one reads
	// its missing entries as 0, as the image decoder does.
	need := base.n * (hival + 1)
	if len(lookup) < need {
		lookup = append(lookup, make([]byte, need-len(lookup))...)
	}
	in := make([]float64, base.n)
	out := make([]float64, s.t.n)
	palette := make([]byte, 0, s.t.n*(hival+1))
	lo, hi := s.baseRange(ctx, arr[1], base.n)
	for i := range hival + 1 {
		for j := range base.n {
			// 8.6.6.3: a byte maps linearly onto the base component's range.
			in[j] = lo[j] + float64(lookup[i*base.n+j])/255*(hi[j]-lo[j])
		}
		base.conv.Convert(in, out)
		for _, v := range out {
			palette = append(palette, byte(clamp01(v)*255+0.5))
		}
	}
	keep.kind = planKeep
	keep.def = ast.PdfArray{ast.PdfName(graphics.FamilyIndexed), s.t.spaceObject(), ast.PdfInt(hival), ast.PdfString(palette)}
	return keep
}

// baseRange returns the range of each component of an Indexed base: [0, 1]
// but for Lab (its /Range, L* over [0, 100]) and an ICCBased /Range.
func (s *session) baseRange(ctx context.Context, def ast.PdfObject, n int) (lo, hi []float64) {
	lo, hi = make([]float64, n), make([]float64, n)
	for i := range hi {
		hi[i] = 1
	}
	arr, _ := s.resolveArray(ctx, def)
	if len(arr) < 2 {
		return lo, hi
	}
	family, _ := s.resolveName(ctx, arr[0])
	var rng []float64
	switch string(family) {
	case graphics.FamilyLab:
		rng = []float64{0, 100, -100, 100, -100, 100}
		if params, _ := s.resolveDict(ctx, arr[1]); params != nil {
			if v, ok := s.numbers(ctx, params["Range"]); ok && len(v) == 4 {
				copy(rng[2:], v)
			}
		}
	case graphics.FamilyICCBased:
		if stream, err := s.resolveStream(ctx, arr[1]); err == nil {
			rng, _ = s.numbers(ctx, stream.Dict["Range"])
		}
	}
	if len(rng) == 2*n {
		for i := range n {
			lo[i], hi[i] = rng[2*i], rng[2*i+1]
		}
	}
	return lo, hi
}

// lookupBytes reads an Indexed palette, a string or a stream (8.6.6.3).
func (s *session) lookupBytes(ctx context.Context, obj ast.PdfObject) ([]byte, error) {
	resolved, err := s.resolve(ctx, obj)
	if err != nil {
		return nil, fmt.Errorf("%w: Indexed lookup: %w", ErrUnsupportedSpace, err)
	}
	switch v := resolved.(type) {
	case ast.PdfString:
		return []byte(v), nil
	case ast.PdfStream:
		data, err := s.doc.DecodeStream(ctx, v)
		if err != nil {
			return nil, fmt.Errorf("%w: Indexed lookup: %w", ErrUnsupportedSpace, err)
		}
		return data, nil
	}
	return nil, fmt.Errorf("%w: Indexed lookup is %T", ErrUnsupportedSpace, resolved)
}

// planSpot plans a Separation or DeviceN space (ISO 32000-1, 8.6.6.4 and
// 8.6.6.5). Under the default policy its colorants stay and its alternate
// space converts: the tint transform becomes a sampled function into the
// target. With [Options.ConvertSpots] its colour converts to the target
// through the tint transform. A space over the colorant None alone paints
// nothing, and one over All marks every separation, registration marks, so
// both stay as they are under either policy.
func (s *session) planSpot(ctx context.Context, family string, arr ast.PdfArray, depth int, convert bool) *spacePlan {
	bad := func(err error) *spacePlan {
		return &spacePlan{kind: planUnknown, err: fmt.Errorf("%w: %s: %w", ErrUnsupportedSpace, family, err)}
	}
	if len(arr) < 4 {
		return bad(fmt.Errorf("%d elements", len(arr)))
	}
	var names []ast.PdfName
	if family == graphics.FamilySeparation {
		name, err := s.resolveName(ctx, arr[1])
		if err != nil {
			return bad(err)
		}
		names = []ast.PdfName{name}
	} else {
		list, err := s.resolveArray(ctx, arr[1])
		if err != nil || len(list) == 0 || len(list) > maxComponents {
			return bad(fmt.Errorf("%d colorants", len(list)))
		}
		for _, o := range list {
			name, err := s.resolveName(ctx, o)
			if err != nil {
				return bad(err)
			}
			names = append(names, name)
		}
	}
	n := len(names)
	initial := make([]float64, n)
	for i := range initial {
		// 8.6.6.4 and 8.6.6.5: the initial tint is 1.0.
		initial[i] = 1
	}
	unchanged := &spacePlan{kind: planIdentity, n: n, initial: initial}
	none := true
	for _, name := range names {
		if name == "All" {
			unchanged.fixed = true
			return unchanged
		}
		none = none && name == "None"
	}
	if none {
		unchanged.fixed = true
		return unchanged
	}
	alt := s.plan(ctx, arr[2], nil, depth+1, pdfimage.RoleTintAlternate)
	switch alt.kind {
	case planIdentity:
		if !convert {
			unchanged.spot = true
			s.keepColorants(names)
			return unchanged
		}
	case planConvert:
	case planKeep, planPattern, planUnknown:
		// 8.6.6.4: the alternate space is a process or CIE-based space.
		return bad(fmt.Errorf("alternate space: %w", errors.Join(alt.err, ErrUnsupportedSpace)))
	}
	fn, err := function.New(ctx, s.doc, arr[3])
	if err != nil {
		return bad(fmt.Errorf("tint transform: %w", err))
	}
	if fn.Inputs() != n || fn.Outputs() != alt.n {
		return bad(fmt.Errorf("tint transform of %d inputs and %d outputs for %d colorants into %d components", fn.Inputs(), fn.Outputs(), n, alt.n))
	}
	altConv := alt.conv
	if altConv == nil {
		// The alternate already is the target: the tint transform's output
		// is the target's colour.
		altConv = identity{}
	}
	tint := &tintConverter{fn: fn, mid: alt.n, next: altConv}
	if convert {
		return &spacePlan{kind: planConvert, n: n, conv: tint, initial: initial}
	}
	domain := make([]float64, 0, 2*n)
	for range n {
		domain = append(domain, 0, 1)
	}
	fnRef, err := s.sampledFunction(ctx, domain, s.t.n, func(in, out []float64) { tint.Convert(in, out) })
	if err != nil {
		return bad(err)
	}
	def := make(ast.PdfArray, len(arr))
	copy(def, arr)
	def[2], def[3] = s.t.spaceObject(), fnRef
	s.report.SpotsKept++
	s.keepColorants(names)
	return &spacePlan{kind: planKeep, n: n, initial: initial, def: def, spot: true}
}

// keepColorants records the colorants of a kept spot colour in the report,
// once each.
func (s *session) keepColorants(names []ast.PdfName) {
	for _, name := range names {
		if name == "None" || slices.Contains(s.report.KeptColorants, string(name)) {
			continue
		}
		s.report.KeptColorants = append(s.report.KeptColorants, string(name))
	}
}

// tintConverter evaluates a tint transform, then converts the alternate
// space's colour to the target.
type tintConverter struct {
	fn   *function.Function
	mid  int
	next Converter
}

func (c *tintConverter) Convert(in, out []float64) {
	alt, err := c.fn.Eval(in[:c.fn.Inputs()])
	if err != nil || len(alt) < c.mid {
		// 8.6.6.4 defines no colour for a transform that fails to
		// evaluate; the conversion takes the alternate colour whose
		// components are all 0.
		alt = make([]float64, c.mid)
	}
	c.next.Convert(alt, out)
}

// pow is math.Pow for a base in [0, 1], 0 at 0.
func pow(b, e float64) float64 {
	if b <= 0 {
		return 0
	}
	return math.Pow(b, e)
}
