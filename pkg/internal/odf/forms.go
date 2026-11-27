package odf

// FormIndex maps each form:id and xml:id of the form elements under forms,
// an office:forms element (ODF 1.3, Part 3, 13.2), to the first element
// that carries it, in the order a depth-first walk from the last child
// meets them: the element a draw:control's draw:control attribute names
// (10.3.13). One walk serves every control: a walk per control would cost
// the controls times the forms. A nil forms gives an empty index.
func FormIndex(forms *Element) map[string]*Element {
	out := map[string]*Element{}
	stack := []*Element{forms}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		if cur.Name.Space == NSForm {
			for _, id := range []string{cur.A(NSForm, "id"), cur.A(NSXML, "id")} {
				if _, seen := out[id]; id != "" && !seen {
					out[id] = cur
				}
			}
		}
		for _, k := range cur.Children {
			if !k.IsText() {
				stack = append(stack, k)
			}
		}
	}
	return out
}

// CheckedBox reports a form:checkbox that is checked: form:current-state,
// else form:state (ODF 1.3, Part 3, 19.255 and 19.268).
func CheckedBox(el *Element) bool {
	if v, ok := el.Lookup(NSForm, "current-state"); ok {
		return v == "checked"
	}
	return el.A(NSForm, "state") == "checked"
}
