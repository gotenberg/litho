package omml

import (
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// Justify is the justification of a display equation (ECMA-376 Part 1,
// 22.1.2.51 jc and 22.1.2.25 defJc).
type Justify uint8

const (
	// CenterGroup centres the equations of a math paragraph as one
	// group, each left-aligned in it: the default.
	CenterGroup Justify = iota
	// Center centres each equation.
	Center
	// Left aligns each equation to the left margin.
	Left
	// Right aligns each equation to the right margin.
	Right
)

// justifyOf reads an ST_Jc value of Office Math (22.1.2.51), ok false for
// an unknown one.
func justifyOf(v string) (Justify, bool) {
	switch v {
	case "centerGroup":
		return CenterGroup, true
	case "center":
		return Center, true
	case "left":
		return Left, true
	case "right":
		return Right, true
	}
	return CenterGroup, false
}

// Settings are the math properties of a document (ECMA-376 Part 1,
// 22.1.2.62 mathPr).
type Settings struct {
	// Font is the family equations are set in (22.1.2.61 mathFont),
	// Cambria Math by default.
	Font string
	// Justify is the default justification of a math paragraph
	// (22.1.2.25 defJc).
	Justify Justify
	// Display lays math paragraphs out in display style (22.1.2.30
	// dispDef), true by default.
	Display bool
	// LeftMargin and RightMargin narrow math paragraphs, in points
	// (22.1.2.59 lMargin, 22.1.2.90 rMargin).
	LeftMargin, RightMargin float64
	// IntegralLimitsUnder puts the limits of integrals under and over
	// them, false for beside them (22.1.2.49 intLim, subSup by default).
	IntegralLimitsUnder bool
	// NaryLimitsUnder does so for the other n-ary operators (22.1.2.71
	// naryLim, undOvr by default).
	NaryLimitsUnder bool
	// SmallFractions sets fractions in the size of inline ones
	// (22.1.2.98 smallFrac).
	SmallFractions bool
}

// DefaultSettings returns the settings of a document whose mathPr says
// nothing (22.1.2.62).
func DefaultSettings() Settings {
	return Settings{Font: "Cambria Math", Justify: CenterGroup, Display: true, NaryLimitsUnder: true}
}

// ReadSettings reads mathPr, the m:mathPr of a Word document's settings
// part, nil for none, over the defaults.
func ReadSettings(mathPr *ooxml.Element) Settings {
	s := DefaultSettings()
	for _, c := range mathPr.Kids() {
		if c.Space != ooxml.NSM {
			continue
		}
		v, has := c.Attr("val")
		switch c.Local {
		case "mathFont":
			if v != "" {
				s.Font = v
			}
		case "defJc":
			if j, ok := justifyOf(v); ok {
				s.Justify = j
			}
		case "dispDef":
			s.Display = ooxml.OnOff(v, has)
		case "lMargin":
			if t, ok := ooxml.Twips(v); ok {
				s.LeftMargin = min(max(t, 0), maxMargin)
			}
		case "rMargin":
			if t, ok := ooxml.Twips(v); ok {
				s.RightMargin = min(max(t, 0), maxMargin)
			}
		case "intLim":
			s.IntegralLimitsUnder = v == "undOvr"
		case "naryLim":
			s.NaryLimitsUnder = v != "subSup"
		case "smallFrac":
			s.SmallFractions = ooxml.OnOff(v, has)
		}
	}
	return s
}

// maxMargin caps the side margins of math paragraphs at 1584 points, 22
// inches, the widest page Word makes. Anti-DoS guard, not spec-mandated:
// a wider margin leaves an equation no room on any page.
const maxMargin = 1584
