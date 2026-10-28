package graphics

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// Colour space families, the names ISO 32000-1, 8.6.3, Table 62 gives them.
const (
	FamilyDeviceGray = "DeviceGray"
	FamilyDeviceRGB  = "DeviceRGB"
	FamilyDeviceCMYK = "DeviceCMYK"
	FamilyCalGray    = "CalGray"
	FamilyCalRGB     = "CalRGB"
	FamilyLab        = "Lab"
	FamilyICCBased   = "ICCBased"
	FamilyIndexed    = "Indexed"
	FamilySeparation = "Separation"
	FamilyDeviceN    = "DeviceN"
	FamilyPattern    = "Pattern"
)

// ColorSpace is a colour space as a cs or CS operator selected it.
//
// It is immutable once set on a [State]: q copies a State shallowly, so a
// handler replaces a ColorSpace rather than editing one in place.
type ColorSpace struct {
	// Family is the colour space family (Table 62), or "" when the operand
	// named a resource the machine could not resolve.
	Family string
	// Resource is the /ColorSpace resource name the operand named, or "" when
	// the operand named a family directly (ISO 32000-1, 8.6.3).
	Resource string
	// Object is the resolved definition: the array or name the /ColorSpace
	// resource holds. It is nil for a family named directly and for an
	// unresolved resource. A caller converting colour reads the family's
	// parameters from it.
	Object ast.PdfObject
	// Components is how many numbers a colour in the space carries: 1 for
	// Indexed and Separation, the number of colorant names for DeviceN, /N for
	// ICCBased (8.6.5.5, 8.6.6), and 0 for a Pattern space and an unresolved
	// one. For a Pattern space with an underlying space it is that space's
	// count, which an uncoloured pattern's colour carries (8.7.3.3).
	Components int
	// Base is the underlying colour space of a Pattern space written as
	// [/Pattern base], the space an uncoloured tiling pattern is painted in
	// (ISO 32000-1, 8.7.3.3). It is nil otherwise.
	Base *ColorSpace
}

// Color is a colour as the graphics state holds it: the space, the
// components and, in a Pattern space, the pattern's resource name.
//
// It is immutable once set on a [State], for the reason [ColorSpace] is.
type Color struct {
	Space      ColorSpace
	Components []float64
	// Pattern is the /Pattern resource name scn or SCN selected in a Pattern
	// space, "" before one is selected, which paints nothing (ISO 32000-1,
	// 8.6.8).
	Pattern string
	// PatternType classifies Pattern by its /PatternType (8.7.3.1, Table 75):
	// "Tiling" for 1, "Shading" for 2, and "Pattern" when the resource is
	// missing or carries another value. It is resolved when scn or SCN selects
	// the pattern, against the resources in force there, and is "" whenever
	// Pattern is.
	PatternType string
}

// deviceGrayBlack is the initial colour of every graphics state (ISO 32000-1,
// 8.4.1, Table 52: DeviceGray, black).
func deviceGrayBlack() Color {
	return Color{Space: ColorSpace{Family: FamilyDeviceGray, Components: 1}, Components: []float64{0}}
}

// deviceColor builds a colour in a device family from the operands of g, rg
// or k, which select the family and set the colour at once (ISO 32000-1,
// 8.6.8, Table 74).
func deviceColor(family string, components []float64) Color {
	return Color{Space: ColorSpace{Family: family, Components: len(components)}, Components: components}
}

// resolveColorSpace reads the operand of cs or CS. Per ISO 32000-1, 8.6.8 the
// operand names DeviceGray, DeviceRGB, DeviceCMYK or Pattern directly, or a
// resource in the /ColorSpace subdictionary; the families that need parameters
// (CalRGB, ICCBased, ...) can only be named through a resource (8.6.3). A
// direct family name wins over a resource of the same name, which is how
// 8.6.3 reads: those names "may be specified directly".
//
// Default colour spaces (8.6.5.6) are not substituted here: the machine
// records what the stream selected, and the caller converting colour applies
// /DefaultGray, /DefaultRGB and /DefaultCMYK from the resources it renders
// with.
func (m *Machine) resolveColorSpace(ctx context.Context, name ast.PdfName) ColorSpace {
	switch name {
	case FamilyDeviceGray, FamilyDeviceRGB, FamilyDeviceCMYK, FamilyPattern:
		return familyColorSpace(string(name))
	}
	cs := ColorSpace{Resource: string(name)}
	if m.Resources == nil {
		return cs
	}
	dict, err := document.ResolveDict(ctx, m.Doc, m.resolvable(m.Resources["ColorSpace"]))
	if err != nil || dict == nil {
		return cs
	}
	obj, err := document.ResolveObject(ctx, m.Doc, m.resolvable(dict[name]))
	if err != nil || obj == nil {
		return cs
	}
	def := m.describeColorSpace(ctx, obj, true)
	def.Resource = string(name)
	return def
}

// describeColorSpace reads a colour space definition: a family name, or an
// array whose first element is the family (ISO 32000-1, 8.6.3). allowPattern
// is false for a Pattern space's base, which shall not itself be a Pattern
// space (8.7.3.3); that also bounds the recursion to one level.
func (m *Machine) describeColorSpace(ctx context.Context, obj ast.PdfObject, allowPattern bool) ColorSpace {
	switch v := obj.(type) {
	case ast.PdfName:
		cs := familyColorSpace(string(v))
		if cs.Family == FamilyPattern && !allowPattern {
			return ColorSpace{}
		}
		cs.Object = v
		return cs
	case ast.PdfArray:
		if len(v) == 0 {
			return ColorSpace{}
		}
		family, err := document.ResolveName(ctx, m.Doc, m.resolvable(v[0]))
		if err != nil {
			return ColorSpace{}
		}
		cs := ColorSpace{Family: string(family), Object: v}
		switch cs.Family {
		case FamilyDeviceGray, FamilyCalGray, FamilyIndexed, FamilySeparation:
			cs.Components = 1
		case FamilyDeviceRGB, FamilyCalRGB, FamilyLab:
			cs.Components = 3
		case FamilyDeviceCMYK:
			cs.Components = 4
		case FamilyICCBased:
			cs.Components = m.iccComponents(ctx, v)
		case FamilyDeviceN:
			if len(v) > 1 {
				names, err := document.ResolveArray(ctx, m.Doc, m.resolvable(v[1]))
				if err == nil {
					cs.Components = len(names)
				}
			}
		case FamilyPattern:
			if !allowPattern {
				return ColorSpace{}
			}
			if len(v) > 1 {
				base, err := document.ResolveObject(ctx, m.Doc, m.resolvable(v[1]))
				if err == nil && base != nil {
					b := m.describeColorSpace(ctx, base, false)
					if b.Family != "" {
						cs.Base = &b
						cs.Components = b.Components
					}
				}
			}
		default:
			// An unknown family is a malformed definition; it names nothing a
			// colour can be read in.
			return ColorSpace{}
		}
		return cs
	}
	return ColorSpace{}
}

// familyColorSpace is the space a bare family name selects. Only the device
// families and Pattern can stand alone (ISO 32000-1, 8.6.3); any other name
// yields an unresolved space.
func familyColorSpace(name string) ColorSpace {
	switch name {
	case FamilyDeviceGray:
		return ColorSpace{Family: name, Components: 1}
	case FamilyDeviceRGB:
		return ColorSpace{Family: name, Components: 3}
	case FamilyDeviceCMYK:
		return ColorSpace{Family: name, Components: 4}
	case FamilyPattern:
		return ColorSpace{Family: name}
	}
	return ColorSpace{}
}

// iccComponents returns /N of an ICCBased space's stream, which shall be 1, 3
// or 4 (ISO 32000-1, 8.6.5.5, Table 66), or 0 when it is missing or another
// value.
func (m *Machine) iccComponents(ctx context.Context, def ast.PdfArray) int {
	if len(def) < 2 {
		return 0
	}
	stream, err := document.ResolveStream(ctx, m.Doc, m.resolvable(def[1]))
	if err != nil || stream.Dict == nil {
		return 0
	}
	n, err := document.ResolveInt(ctx, m.Doc, m.resolvable(stream.Dict["N"]))
	if err != nil {
		return 0
	}
	switch n {
	case 1, 3, 4:
		return intconv.Clamp(n)
	}
	return 0
}

// initialColor is the colour cs or CS installs with a space. Per ISO 32000-1,
// 8.6.8 (cs, Table 74) every component starts at 0.0, except DeviceCMYK
// (0 0 0 1), Separation and DeviceN (1.0 each), and Lab and ICCBased, whose 0.0
// is clipped into the component's range. A Pattern space starts with no
// pattern, which paints nothing.
func (m *Machine) initialColor(ctx context.Context, cs ColorSpace) Color {
	c := Color{Space: cs}
	switch cs.Family {
	case FamilyPattern, "":
		return c
	case FamilyDeviceCMYK:
		c.Components = []float64{0, 0, 0, 1}
		return c
	}
	c.Components = make([]float64, cs.Components)
	switch cs.Family {
	case FamilySeparation, FamilyDeviceN:
		for i := range c.Components {
			c.Components[i] = 1
		}
	case FamilyLab:
		// L* ranges over [0, 100] and a*, b* over /Range, default
		// [-100 100 -100 100] (8.6.5.4, Table 65).
		if lo, hi, ok := m.rangeBound(ctx, cs, 0); ok && len(c.Components) == 3 {
			c.Components[1] = clamp(0, lo, hi)
		}
		if lo, hi, ok := m.rangeBound(ctx, cs, 1); ok && len(c.Components) == 3 {
			c.Components[2] = clamp(0, lo, hi)
		}
	case FamilyICCBased:
		for i := range c.Components {
			if lo, hi, ok := m.rangeBound(ctx, cs, i); ok {
				c.Components[i] = clamp(0, lo, hi)
			}
		}
	}
	return c
}

// rangeBound reads pair i of a Lab dictionary's or ICCBased stream's /Range.
func (m *Machine) rangeBound(ctx context.Context, cs ColorSpace, i int) (lo, hi float64, ok bool) {
	def, isArray := cs.Object.(ast.PdfArray)
	if !isArray || len(def) < 2 {
		return 0, 0, false
	}
	var dict ast.PdfDict
	switch cs.Family {
	case FamilyLab:
		d, err := document.ResolveDict(ctx, m.Doc, m.resolvable(def[1]))
		if err != nil {
			return 0, 0, false
		}
		dict = d
	case FamilyICCBased:
		s, err := document.ResolveStream(ctx, m.Doc, m.resolvable(def[1]))
		if err != nil {
			return 0, 0, false
		}
		dict = s.Dict
	}
	if dict == nil {
		return 0, 0, false
	}
	rng, err := document.ResolveArray(ctx, m.Doc, m.resolvable(dict["Range"]))
	if err != nil || len(rng) < 2*i+2 {
		return 0, 0, false
	}
	lo, okLo := document.Number(rng[2*i])
	hi, okHi := document.Number(rng[2*i+1])
	if !okLo || !okHi || lo > hi {
		return 0, 0, false
	}
	return lo, hi, true
}

func clamp(v, lo, hi float64) float64 {
	return min(max(v, lo), hi)
}

// colorFromOperands reads the operands of sc, scn, SC or SCN in the current
// space: the numbers are the components and, in a Pattern space, a trailing
// name selects the pattern (ISO 32000-1, 8.6.8, Table 74). Operands that set
// nothing leave current as it was.
//
// Deliberate leniency: Table 74 requires as many operands as the space has
// components. The machine keeps the numbers it is given and leaves the
// mismatch to the caller converting the colour, which can see both counts.
//
// The pattern's [Color.PatternType] is left for the caller to resolve, since
// it needs the machine's resources.
func colorFromOperands(current Color, operands []ast.PdfObject) Color {
	var vals []float64
	var pattern string
	for _, operand := range operands {
		switch v := operand.(type) {
		case ast.PdfReal, ast.PdfInt:
			vals = append(vals, document.Float(operand))
		case ast.PdfName:
			pattern = string(v)
		}
	}
	next := Color{Space: current.Space}
	if current.Space.Family == FamilyPattern {
		if pattern == "" {
			return current
		}
		next.Pattern = pattern
		next.Components = vals
		return next
	}
	if len(vals) == 0 {
		return current
	}
	next.Components = vals
	return next
}

// selectColor applies sc, scn, SC or SCN to current and, when the operands
// select a pattern, classifies it against the resources in force now (ISO
// 32000-1, 8.7.3.1): a pattern resource is looked up where it is selected.
func (m *Machine) selectColor(ctx context.Context, current Color, operands []ast.PdfObject) Color {
	next := colorFromOperands(current, operands)
	if next.Pattern != "" && len(operands) > 0 {
		if _, named := operands[len(operands)-1].(ast.PdfName); named {
			next.PatternType = m.resolvePatternType(ctx, next.Pattern)
		}
	}
	return next
}

// resolvable returns obj as the machine should resolve it: through
// [Machine.Staged] first, and as nil when obj is a reference and the machine
// has no document to follow it in.
func (m *Machine) resolvable(obj ast.PdfObject) ast.PdfObject {
	obj = m.StagedObject(obj)
	if _, isRef := obj.(ast.PdfIndirectRef); isRef && m.Doc == nil {
		return nil
	}
	return obj
}
