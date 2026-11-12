// Package formfield turns the form controls of a laid-out word-processing
// document into the interactive form fields of its pages (ISO 32000-1,
// 12.7). The DOCX and ODT converters share it: a converter gives each
// control's layout node the Role Form and an ID from [NodeID], lays the
// document out, and hands the controls to [Attach], which moves what each
// control shows from its page into the field that draws it.
package formfield

import (
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// Control is one form control of a source document.
type Control struct {
	// ID is the ID of the control's layout node, from [NodeID]. The node
	// has the Role Form, so what it shows hangs from a Form structure
	// element of its own.
	ID string
	// Field is the field the control becomes, its Rect, Appearance,
	// OffAppearance and Parent left to [Attach].
	Field compose.Field
	// Box takes the widget's rectangle from the node's box, for a control
	// of a size of its own laid out as an inline block; false takes it
	// from what the control shows, for a control whose content flows with
	// the text.
	Box bool
	// Other, when not nil, returns the appearance of the state a check box
	// or a radio button does not show, from the fragments of the state it
	// shows, in page coordinates, and the widget's rectangle. nil draws
	// nothing for that state.
	Other func(shown []compose.Fragment, r compose.Rect, m *compose.Measurer) []compose.Fragment
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
	elem *compose.StructElem
	// page is the 0-based page of its first fragment, -1 for none.
	page int
	// at is the origin of its first fragment, in page coordinates.
	at compose.Point
	// pages counts the pages its content lands on.
	pages int
	taken bool
}

// maxWalk bounds the fragments and elements one walk visits. Anti-DoS
// guard, not spec-mandated: the layout holds at most a million fragments
// a page, and a walk past this many gives up on the controls it has not
// placed rather than spend unbounded time.
const maxWalk = 50_000_000

// Attach makes the controls into the form fields of the pages of res: for
// each control whose content landed on one page, it takes that content off
// the page (compose.TakeContent) into the field's appearance, with the
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
	m := compose.NewMeasurer()
	missed := 0
	for _, c := range controls {
		if !attach(res, c, byPage, m) {
			missed++
		}
	}
	return missed
}

// attach makes c a field of its page, false when it cannot.
func attach(res *layout.Result, c *Control, byPage map[int][]*formElem, m *compose.Measurer) bool {
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
		} else if dx < -2 || dx > 2 || dy < -2 || dy > 48 {
			// The first glyph of a control that flows with the text
			// starts where its inline box does, its baseline below the
			// box's top by at most a line of the largest text.
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
	taken := compose.TakeContent(pg, best.elem)
	best.taken = true
	var rect compose.Rect
	if c.Box && hasBox {
		rect = box
	} else if rect, ok = m.Bounds(taken); !ok || !(rect.Width > 0) || !(rect.Height > 0) {
		pg.Content = append(pg.Content, taken...)
		return false
	}
	f := c.Field
	f.Rect, f.Parent = rect, best.elem
	switch f.Kind {
	case compose.FieldCheckBox, compose.FieldRadio:
		var other []compose.Fragment
		if c.Other != nil {
			other = c.Other(taken, rect, m)
		}
		if f.Checked {
			f.Appearance, f.OffAppearance = taken, other
		} else {
			f.Appearance, f.OffAppearance = other, taken
		}
	case compose.FieldText, compose.FieldList, compose.FieldCombo:
		f.Appearance = taken
	}
	pg.Fields = append(pg.Fields, f)
	return true
}

// formElems returns the Form elements of the tree under root in preorder,
// and the Form element each element under one belongs to.
func formElems(root *compose.StructElem) ([]*formElem, map[*compose.StructElem]*formElem) {
	var forms []*formElem
	owner := map[*compose.StructElem]*formElem{}
	type item struct {
		e    *compose.StructElem
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
func locate(pages []*compose.Page, forms []*formElem, owner map[*compose.StructElem]*formElem) {
	if len(forms) == 0 {
		return
	}
	steps := 0
	for i, pg := range pages {
		if pg == nil {
			continue
		}
		seen := map[*formElem]bool{}
		var walk func(frags []compose.Fragment, t compose.Matrix, depth int)
		walk = func(frags []compose.Fragment, t compose.Matrix, depth int) {
			for _, fr := range frags {
				steps++
				if steps > maxWalk {
					return
				}
				var parent *compose.StructElem
				var at compose.Point
				switch v := fr.(type) {
				case *compose.Text:
					parent, at = v.Parent, v.Origin
					if len(v.Glyphs) > 0 {
						at.X += v.Glyphs[0].X
						at.Y += v.Glyphs[0].Y
					}
				case *compose.Image:
					parent, at = v.Parent, compose.Point{X: v.Rect.X, Y: v.Rect.Y}
				case *compose.Path:
					parent = v.Parent
					if len(v.Ops) > 0 {
						at = v.Ops[0].P[0]
					}
				case *compose.Group:
					if v != nil && depth < limits.MaxGroupDepth {
						g := v.Transform
						if g == (compose.Matrix{}) {
							g = compose.Identity
						}
						walk(v.Content, then(g, t), depth+1)
					}
					continue
				}
				f := owner[parent]
				if f == nil || parent == nil {
					continue
				}
				if !seen[f] {
					seen[f] = true
					f.pages++
				}
				if f.page < 0 {
					f.page = i
					f.at = compose.Point{X: t.A*at.X + t.C*at.Y + t.E, Y: t.B*at.X + t.D*at.Y + t.F}
				}
			}
		}
		walk(pg.Content, compose.Identity, 0)
	}
}

// then returns the transform that applies a, then b.
func then(a, b compose.Matrix) compose.Matrix {
	return compose.Matrix{
		A: a.A*b.A + a.B*b.C,
		B: a.A*b.B + a.B*b.D,
		C: a.C*b.A + a.D*b.C,
		D: a.C*b.B + a.D*b.D,
		E: a.E*b.A + a.F*b.C + b.E,
		F: a.E*b.B + a.F*b.D + b.F,
	}
}
