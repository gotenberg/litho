package formfield

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Control is one form control of a source document.
type Control struct {
	// ID is the ID of the control's layout node, from [NodeID]. The node
	// has the Role Form, so what it shows hangs from a Form structure
	// element of its own.
	ID string
	// Field is the field the control becomes, its Rect, Appearance,
	// OffAppearance and Parent left to [Attach].
	Field typeset.Field
	// Box takes the widget's rectangle from the node's box, for a control
	// of a size of its own laid out as an inline block; false takes it
	// from what the control shows, for a control whose content flows with
	// the text.
	Box bool
	// Other, when not nil, returns the appearance of the state a check box
	// or a radio button does not show, from the fragments of the state it
	// shows, in page coordinates, and the widget's rectangle. nil draws
	// nothing for that state.
	Other func(shown []typeset.Fragment, r typeset.Rect, m *typeset.Measurer) []typeset.Fragment
}

// Printed returns the PrintField attributes of the Form element of a
// control that becomes the field f, or prints as it would show: the
// attributes a reader of the structure tree finds where no widget is
// (ISO 32000-1, 14.8.5.6, Table 348). A text field, a drop-down list and
// a combo box show their value as text, the tv role; a list box takes
// [typeset.PrintList]; a check box and a radio button take cb and rb with
// their state, on or off. The description is the one the field's
// alternate name would carry: f's Description, else its Name, cut at
// [typeset.MaxFieldText] bytes.
func Printed(f *typeset.Field) typeset.PrintField {
	pf := typeset.PrintField{Role: typeset.PrintText, Desc: f.Description}
	if pf.Desc == "" {
		pf.Desc = f.Name
	}
	pf.Desc = Clip(pf.Desc)
	switch f.Kind {
	case typeset.FieldCheckBox, typeset.FieldRadio:
		pf.Role, pf.Checked = typeset.PrintCheckBox, typeset.CheckOff
		if f.Kind == typeset.FieldRadio {
			pf.Role = typeset.PrintRadio
		}
		if f.Checked {
			pf.Checked = typeset.CheckOn
		}
	case typeset.FieldList:
		pf.Role = typeset.PrintList
	case typeset.FieldText, typeset.FieldCombo:
	}
	return pf
}

// Clip returns s cut at [typeset.MaxFieldText] bytes, on a character's
// boundary, its invalid UTF-8 replaced: a text a PrintField description
// holds.
func Clip(s string) string {
	s = strings.ToValidUTF8(s, "�")
	if len(s) <= typeset.MaxFieldText {
		return s
	}
	cut := typeset.MaxFieldText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// idPrefix starts the IDs [NodeID] makes: a control character no XML
// document holds (XML 1.0, 2.2), so no ID the source names collides.
const idPrefix = "\x1fform-"

// NodeID returns the ID of the layout node of the n-th control.
func NodeID(n int) string {
	return idPrefix + strconv.Itoa(n)
}

// formElem is a Form structure element of the layout and where its
// content first lands.
type formElem struct {
	elem *typeset.StructElem
	// page is the 0-based page of its first fragment, -1 for none.
	page int
	// at is the origin of its first fragment, in page coordinates.
	at typeset.Point
	// pages counts the pages its content lands on.
	pages int
	taken bool
}

// maxWalk bounds the fragments and elements one walk visits. Anti-DoS
// guard, not spec-mandated: the layout holds at most a million fragments
// a page, and a walk past this many gives up on the controls it has not
// placed rather than spend unbounded time.
const maxWalk = 50_000_000

// The room [attach] searches for the first fragment of a control that
// flows with the text, in points from its node's anchor: the first glyph
// starts where the inline box does, within flowSlack, its baseline below
// the box's top by at most maxBaselineDrop, a line of the largest text.
const (
	flowSlack       = 2
	maxBaselineDrop = 48
)

// Attach makes the controls into the form fields of the pages of res: for
// each control whose content landed on one page, it takes that content off
// the page (typeset.TakeContent) into the field's appearance, with the
// field's rectangle, and appends the field to the page's Fields, in the
// order of controls. A control whose Form element it does not find, whose
// content spans pages, or that shows nothing it can measure, stays printed
// as it is, with no field; the count of those is returned.
func Attach(res *layout.Result, controls []*Control) int {
	if len(controls) == 0 || res == nil || res.Structure == nil {
		return len(controls)
	}
	forms, owner := formElems(res.Structure)
	locate(res.Pages, forms, owner)
	byPage := map[int][]*formElem{}
	for _, f := range forms {
		if f.page >= 0 {
			byPage[f.page] = append(byPage[f.page], f)
		}
	}
	m := typeset.NewMeasurer()
	missed := 0
	for _, c := range controls {
		if !attach(res, c, byPage, m) {
			missed++
		}
	}
	return missed
}

// attach makes c a field of its page, false when it cannot.
func attach(res *layout.Result, c *Control, byPage map[int][]*formElem, m *typeset.Measurer) bool {
	anchor, ok := res.Anchors[c.ID]
	if !ok || anchor.Page < 1 || anchor.Page > len(res.Pages) {
		return false
	}
	page := anchor.Page - 1
	box, hasBox := res.Extents[c.ID]
	var best *formElem
	bestD := math.Inf(1)
	for _, f := range byPage[page] {
		if f.taken {
			continue
		}
		dx, dy := f.at.X-anchor.X, f.at.Y-anchor.Y
		if c.Box && hasBox {
			const slack = 1
			if f.at.X < box.X-slack || f.at.X > box.X+box.Width+slack || f.at.Y < box.Y-slack || f.at.Y > box.Y+box.Height+slack {
				continue
			}
		} else if dx < -flowSlack || dx > flowSlack || dy < -flowSlack || dy > maxBaselineDrop {
			continue
		}
		if d := math.Hypot(dx, dy); d < bestD {
			best, bestD = f, d
		}
	}
	if best == nil || best.pages != 1 {
		return false
	}
	pg := res.Pages[page]
	taken := typeset.TakeContent(pg, best.elem)
	best.taken = true
	var rect typeset.Rect
	if c.Box && hasBox {
		rect = box
	} else if rect, ok = m.Bounds(taken); !ok || !numeric.Positive(rect.Width) || !numeric.Positive(rect.Height) {
		pg.Content = append(pg.Content, taken...)
		return false
	}
	f := c.Field
	f.Rect, f.Parent = rect, best.elem
	switch f.Kind {
	case typeset.FieldCheckBox, typeset.FieldRadio:
		var other []typeset.Fragment
		if c.Other != nil {
			other = c.Other(taken, rect, m)
		}
		if f.Checked {
			f.Appearance, f.OffAppearance = taken, other
		} else {
			f.Appearance, f.OffAppearance = other, taken
		}
	case typeset.FieldText, typeset.FieldList, typeset.FieldCombo:
		f.Appearance = taken
	}
	pg.Fields = append(pg.Fields, f)
	return true
}

// formElems returns the Form elements of the tree under root in preorder,
// and the Form element each element under one belongs to.
func formElems(root *typeset.StructElem) ([]*formElem, map[*typeset.StructElem]*formElem) {
	var forms []*formElem
	owner := map[*typeset.StructElem]*formElem{}
	type item struct {
		e    *typeset.StructElem
		form *formElem
	}
	stack := []item{{e: root}}
	for steps := 0; len(stack) > 0 && steps < maxWalk; steps++ {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if it.e == nil {
			continue
		}
		form := it.form
		if form == nil && it.e.Role == "Form" {
			form = &formElem{elem: it.e, page: -1}
			forms = append(forms, form)
		}
		if form != nil {
			owner[it.e] = form
		}
		for i := len(it.e.Kids) - 1; i >= 0; i-- {
			stack = append(stack, item{e: it.e.Kids[i], form: form})
		}
	}
	return forms, owner
}

// locate records, for each Form element, the page and origin of its first
// fragment and the pages its content lands on.
func locate(pages []*typeset.Page, forms []*formElem, owner map[*typeset.StructElem]*formElem) {
	if len(forms) == 0 {
		return
	}
	steps := 0
	for i, pg := range pages {
		if pg == nil {
			continue
		}
		seen := map[*formElem]bool{}
		var walk func(frags []typeset.Fragment, t typeset.Matrix, depth int)
		walk = func(frags []typeset.Fragment, t typeset.Matrix, depth int) {
			for _, fr := range frags {
				steps++
				if steps > maxWalk {
					return
				}
				var parent *typeset.StructElem
				var at typeset.Point
				switch v := fr.(type) {
				case *typeset.Text:
					parent, at = v.Parent, v.Origin
					if len(v.Glyphs) > 0 {
						at.X += v.Glyphs[0].X
						at.Y += v.Glyphs[0].Y
					}
				case *typeset.Image:
					parent, at = v.Parent, typeset.Point{X: v.Rect.X, Y: v.Rect.Y}
				case *typeset.Path:
					parent = v.Parent
					if len(v.Ops) > 0 {
						at = v.Ops[0].P[0]
					}
				case *typeset.Group:
					if v != nil && depth < limits.MaxGroupDepth {
						g := v.Transform
						if g == (typeset.Matrix{}) {
							g = typeset.Identity
						}
						walk(v.Content, then(g, t), depth+1)
					}
					continue
				}
				// owner holds no nil key: a fragment without a parent has no
				// Form element.
				f := owner[parent]
				if f == nil {
					continue
				}
				if !seen[f] {
					seen[f] = true
					f.pages++
				}
				if f.page < 0 {
					f.page = i
					f.at = typeset.Point{X: t.A*at.X + t.C*at.Y + t.E, Y: t.B*at.X + t.D*at.Y + t.F}
				}
			}
		}
		walk(pg.Content, typeset.Identity, 0)
	}
}

// then returns the transform that applies a, then b.
func then(a, b typeset.Matrix) typeset.Matrix {
	return typeset.Matrix{
		A: a.A*b.A + a.B*b.C,
		B: a.A*b.B + a.B*b.D,
		C: a.C*b.A + a.D*b.C,
		D: a.C*b.B + a.D*b.D,
		E: a.E*b.A + a.F*b.C + b.E,
		F: a.E*b.B + a.F*b.D + b.F,
	}
}
