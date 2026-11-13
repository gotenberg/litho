package css

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// formState matches the pseudo-classes of the state of a form control as
// the attributes of a static document give it (HTML Standard, 4.16.3): a
// document no one has edited, its controls' values their value
// attributes and text, nothing chosen but what the attributes choose.
// What only a user's edits decide, tooLong and tooShort among the
// validity states, never holds.
func formState(k pseudoKind, n *html.Node) bool {
	if n.Namespace != "" {
		return k == psReadOnly
	}
	switch k { //nolint:exhaustive // the callers pass the form states alone
	case psRequired:
		return requirable(n) && hasAttr(n, "required")
	case psOptional:
		return requirable(n) && !hasAttr(n, "required")
	case psReadWrite:
		return readWrite(n)
	case psReadOnly:
		return !readWrite(n)
	case psPlaceholderShown:
		return placeholderShown(n)
	case psDefault:
		return isDefault(n)
	case psIndeterminate:
		return indeterminate(n)
	case psValid, psInvalid:
		valid, ok := validity(n, 0)
		return ok && valid == (k == psValid)
	case psInRange, psOutOfRange:
		in, ok := inRange(n)
		return ok && in == (k == psInRange)
	case psOpen:
		return (isHTML(n, "details") || isHTML(n, "dialog")) && hasAttr(n, "open")
	}
	return false
}

// inputType returns the state of an input's type attribute, text for a
// missing or unknown one (HTML Standard, 4.10.5).
func inputType(n *html.Node) string {
	t, _ := attr(n, "type")
	t = lower(strings.TrimSpace(t))
	if _, ok := inputTypes[t]; !ok {
		return "text"
	}
	return t
}

// inputTypes maps each type of input to the attributes and states it takes
// part in: r required, w editable text, p placeholder, c a candidate for
// constraint validation, n a number or date the range applies to.
var inputTypes = map[string]string{
	"text": "rwpc", "search": "rwpc", "url": "rwpc", "tel": "rwpc", "email": "rwpc", "password": "rwpc",
	"date": "rwcn", "month": "rwcn", "week": "rwcn", "time": "rwcn", "datetime-local": "rwcn",
	"number": "rwpcn", "range": "cn", "color": "c", "checkbox": "rc", "radio": "rc", "file": "rc",
	"submit": "c", "image": "c", "reset": "", "button": "", "hidden": "",
}

// typeHas reports whether the input n takes part in what flag names (see
// inputTypes).
func typeHas(n *html.Node, flag byte) bool {
	return strings.IndexByte(inputTypes[inputType(n)], flag) >= 0
}

// requirable reports a control the required attribute applies to (HTML
// Standard, 4.10.5.3.4, 4.10.7 and 4.10.11).
func requirable(n *html.Node) bool {
	return isHTML(n, "input") && typeHas(n, 'r') || isHTML(n, "select") || isHTML(n, "textarea")
}

// readWrite reports an element :read-write matches: an input whose text
// a user may edit, or a textarea, neither read-only nor disabled, and an
// editable element (HTML Standard, 4.16.3).
func readWrite(n *html.Node) bool {
	switch {
	case isHTML(n, "input"):
		return typeHas(n, 'w') && !hasAttr(n, "readonly") && !disabled(n)
	case isHTML(n, "textarea"):
		return !hasAttr(n, "readonly") && !disabled(n)
	}
	for e := n; isElement(e); e = e.Parent {
		if v, ok := attr(e, "contenteditable"); ok {
			switch lower(strings.TrimSpace(v)) {
			case "", "true", "plaintext-only":
				return true
			case "false":
				return false
			}
		}
	}
	return false
}

// valueOf returns the value of the input, textarea or option n: its value
// attribute, or the textarea's text, or an option's text when it has no
// value attribute.
func valueOf(n *html.Node) string {
	v, ok := attr(n, "value")
	if isHTML(n, "textarea") || isHTML(n, "option") && !ok {
		var sb strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.TextNode {
				sb.WriteString(c.Data)
			}
		}
		return sb.String()
	}
	return v
}

// placeholderShown reports an input or textarea that shows its
// placeholder: it has one and its value is empty.
func placeholderShown(n *html.Node) bool {
	takes := isHTML(n, "input") && typeHas(n, 'p') || isHTML(n, "textarea")
	return takes && hasAttr(n, "placeholder") && valueOf(n) == ""
}

// formOwner returns the form n belongs to: its nearest form ancestor, nil
// for none.
func formOwner(n *html.Node) *html.Node {
	for p := parentElement(n); p != nil; p = parentElement(p) {
		if isHTML(p, "form") {
			return p
		}
	}
	return nil
}

// isSubmit reports a submit button (HTML Standard, 4.10.6).
func isSubmit(n *html.Node) bool {
	if isHTML(n, "input") {
		t := inputType(n)
		return t == "submit" || t == "image"
	}
	if !isHTML(n, "button") {
		return false
	}
	t, _ := attr(n, "type")
	t = lower(strings.TrimSpace(t))
	return t != "button" && t != "reset"
}

// isDefault reports an element :default matches: a selected option, a
// checked check box or radio button, and the first submit button of its
// form.
func isDefault(n *html.Node) bool {
	switch {
	case isHTML(n, "option"):
		return hasAttr(n, "selected")
	case isHTML(n, "input") && (inputType(n) == "checkbox" || inputType(n) == "radio"):
		return hasAttr(n, "checked")
	case isSubmit(n):
		f := formOwner(n)
		if f == nil {
			return false
		}
		return firstSubmit(f, 0) == n
	}
	return false
}

// firstSubmit returns the first submit button under e in tree order, at
// depth below the form; nil for none.
func firstSubmit(e *html.Node, depth int) *html.Node {
	if depth >= maxFormDepth {
		return nil
	}
	for c := e.FirstChild; c != nil; c = c.NextSibling {
		if !isElement(c) {
			continue
		}
		if isSubmit(c) {
			return c
		}
		if s := firstSubmit(c, depth+1); s != nil {
			return s
		}
	}
	return nil
}

// maxFormDepth bounds the depth below a form or a fieldset the form
// states search. Anti-DoS guard, not spec-mandated: no form nests its
// controls this deep.
const maxFormDepth = 256

// indeterminate reports an element :indeterminate matches: a radio button
// whose group has none checked, and a progress bar without a value.
func indeterminate(n *html.Node) bool {
	if isHTML(n, "progress") {
		return !hasAttr(n, "value")
	}
	if !isHTML(n, "input") || inputType(n) != "radio" || hasAttr(n, "checked") {
		return false
	}
	return !groupChecked(n)
}

// groupChecked reports a checked radio button in the group of the radio
// button n: its form's, or its tree's when it has none, of one name (HTML
// Standard, 4.10.5.1.18).
func groupChecked(n *html.Node) bool {
	name, ok := attr(n, "name")
	if !ok || name == "" {
		return hasAttr(n, "checked")
	}
	root := formOwner(n)
	if root == nil {
		root = n
		for p := n.Parent; p != nil; p = p.Parent {
			root = p
		}
	}
	found := false
	var walk func(e *html.Node, depth int)
	walk = func(e *html.Node, depth int) {
		for c := e.FirstChild; c != nil && !found && depth < maxFormDepth; c = c.NextSibling {
			if !isElement(c) {
				continue
			}
			if isHTML(c, "input") && inputType(c) == "radio" && hasAttr(c, "checked") {
				if v, _ := attr(c, "name"); v == name && formOwner(c) == formOwner(n) {
					found = true
					return
				}
			}
			walk(c, depth+1)
		}
	}
	walk(root, 0)
	return found
}

// validity returns whether n is valid and whether :valid and :invalid
// apply to it: a candidate for constraint validation, a form or a
// fieldset, which is invalid when a control in it is (HTML Standard,
// 4.10.20).
func validity(n *html.Node, depth int) (valid, ok bool) {
	if isHTML(n, "form") || isHTML(n, "fieldset") {
		return !invalidUnder(n, depth), true
	}
	if !candidate(n) {
		return false, false
	}
	return !suffers(n), true
}

// invalidUnder reports an invalid control under e.
func invalidUnder(e *html.Node, depth int) bool {
	if depth >= maxFormDepth {
		return false
	}
	for c := e.FirstChild; c != nil; c = c.NextSibling {
		if !isElement(c) {
			continue
		}
		if candidate(c) && suffers(c) || invalidUnder(c, depth+1) {
			return true
		}
	}
	return false
}

// candidate reports a candidate for constraint validation (HTML Standard,
// 4.10.20.1): an input, select, textarea or submit button that is not
// disabled, read-only, a hidden, reset or plain button input, or inside a
// datalist.
func candidate(n *html.Node) bool {
	switch {
	case isHTML(n, "input"):
		if !typeHas(n, 'c') || hasAttr(n, "readonly") && typeHas(n, 'w') {
			return false
		}
	case isHTML(n, "textarea"):
		if hasAttr(n, "readonly") {
			return false
		}
	case isHTML(n, "select"):
	case isHTML(n, "button"):
		if !isSubmit(n) {
			return false
		}
	default:
		return false
	}
	if disabled(n) {
		return false
	}
	for p := parentElement(n); p != nil; p = parentElement(p) {
		if isHTML(p, "datalist") {
			return false
		}
	}
	return true
}

// suffers reports a candidate whose value suffers from one of the
// validity states its attributes decide (HTML Standard, 4.10.20.2):
// valueMissing, typeMismatch, patternMismatch, rangeUnderflow,
// rangeOverflow and stepMismatch.
func suffers(n *html.Node) bool {
	switch {
	case isHTML(n, "select"):
		return hasAttr(n, "required") && placeholderSelected(n)
	case isHTML(n, "textarea"):
		return hasAttr(n, "required") && valueOf(n) == ""
	case !isHTML(n, "input"):
		return false
	}
	t := inputType(n)
	required := hasAttr(n, "required") && typeHas(n, 'r')
	switch t {
	case "checkbox":
		return required && !hasAttr(n, "checked")
	case "radio":
		return required && !groupChecked(n)
	case "file":
		return required
	}
	v := sanitized(n, t)
	if v == "" {
		return required
	}
	switch t {
	case "email":
		if !hasAttr(n, "multiple") && strings.Contains(v, ",") {
			return true
		}
		for e := range strings.SplitSeq(v, ",") {
			if !validEmail.MatchString(strings.Trim(e, asciiWhitespace)) {
				return true
			}
		}
	case "url":
		if u, err := url.Parse(v); err != nil || u.Scheme == "" {
			return true
		}
	}
	if p, ok := attr(n, "pattern"); ok && typeHas(n, 'p') {
		// The pattern is a JavaScript regular expression; one Go's
		// syntax does not read constrains nothing.
		if re, err := regexp.Compile("^(?:" + p + ")$"); err == nil && !re.MatchString(v) {
			return true
		}
	}
	if t == "number" {
		x, _ := strconv.ParseFloat(v, 64)
		if lo, ok := numberAttr(n, "min"); ok && x < lo {
			return true
		}
		if hi, ok := numberAttr(n, "max"); ok && x > hi {
			return true
		}
		return stepMismatch(n, x)
	}
	if typeHas(n, 'n') {
		in, ok := inRange(n)
		return ok && !in
	}
	return false
}

// validEmail is the valid email address syntax of the HTML Standard,
// 4.10.5.1.5.
var validEmail = regexp.MustCompile(`^[a-zA-Z0-9.!#$%&'*+/=?^_` + "`" + `{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$`)

// dateFormats are the valid value syntaxes of the date and time input
// types (HTML Standard, 2.3.5), which compare as strings.
var dateFormats = map[string]*regexp.Regexp{
	"date":           regexp.MustCompile(`^\d{4,}-\d\d-\d\d$`),
	"month":          regexp.MustCompile(`^\d{4,}-\d\d$`),
	"week":           regexp.MustCompile(`^\d{4,}-W\d\d$`),
	"time":           regexp.MustCompile(`^\d\d:\d\d(:\d\d(\.\d{1,3})?)?$`),
	"datetime-local": regexp.MustCompile(`^\d{4,}-\d\d-\d\d[T ]\d\d:\d\d(:\d\d(\.\d{1,3})?)?$`),
}

// sanitized returns the value of the input n of type t after the value
// sanitization algorithm of its type (HTML Standard, 4.10.5.1): line
// breaks dropped and white space trimmed where the type asks, "" for a
// number or date that does not parse.
func sanitized(n *html.Node, t string) string {
	v := valueOf(n)
	switch t {
	case "text", "search", "tel", "password":
		return strings.NewReplacer("\n", "", "\r", "").Replace(v)
	case "url", "email":
		return strings.Trim(strings.NewReplacer("\n", "", "\r", "").Replace(v), asciiWhitespace)
	case "number", "range":
		if x, err := strconv.ParseFloat(v, 64); err != nil || math.IsInf(x, 0) || math.IsNaN(x) {
			return ""
		}
	}
	if re, ok := dateFormats[t]; ok && !re.MatchString(v) {
		return ""
	}
	return v
}

// numberAttr returns the attribute name of n as a finite number.
func numberAttr(n *html.Node, name string) (float64, bool) {
	v, ok := attr(n, name)
	if !ok {
		return 0, false
	}
	x, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil || math.IsInf(x, 0) || math.IsNaN(x) {
		return 0, false
	}
	return x, true
}

// stepMismatch reports a number input whose value x is not its min plus
// a whole number of steps (HTML Standard, 4.10.5.3.8): its step, 1 by
// default, any for none; the value attribute is the step base without a
// min, so it never mismatches then.
func stepMismatch(n *html.Node, x float64) bool {
	if v, _ := attr(n, "step"); lower(strings.TrimSpace(v)) == "any" {
		return false
	}
	step, ok := numberAttr(n, "step")
	if !ok || step <= 0 {
		step = 1
	}
	base, ok := numberAttr(n, "min")
	if !ok {
		return false
	}
	q := (x - base) / step
	return math.Abs(q-math.Round(q)) > 1e-9
}

// inRange returns whether the input n is in its range and whether the
// range pseudo-classes apply: a candidate of a type with a range, which
// has a min or a max (HTML Standard, 4.16.3). A range input clamps its
// value and is always in range.
func inRange(n *html.Node) (in, ok bool) {
	if !isHTML(n, "input") || !typeHas(n, 'n') || !candidate(n) {
		return false, false
	}
	t := inputType(n)
	minV, hasMin := attr(n, "min")
	maxV, hasMax := attr(n, "max")
	if t == "range" {
		return true, true
	}
	v := sanitized(n, t)
	if t == "number" {
		lo, okLo := numberAttr(n, "min")
		hi, okHi := numberAttr(n, "max")
		if !okLo && !okHi {
			return false, false
		}
		x, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return true, true
		}
		under, over := okLo && x < lo, okHi && x > hi
		return !under && !over, true
	}
	re := dateFormats[t]
	okLo := hasMin && re.MatchString(minV)
	okHi := hasMax && re.MatchString(maxV)
	if !okLo && !okHi {
		return false, false
	}
	if v == "" {
		return true, true
	}
	under, over := okLo && v < minV, okHi && v > maxV
	return !under && !over, true
}

// placeholderSelected reports a select whose selected option is none or
// its placeholder label option, the first option, of an empty value, of
// a select that is neither multiple nor taller than one row (HTML
// Standard, 4.10.7).
func placeholderSelected(n *html.Node) bool {
	var opts []*html.Node
	var walk func(e *html.Node, depth int)
	walk = func(e *html.Node, depth int) {
		for c := e.FirstChild; c != nil && depth < maxFormDepth; c = c.NextSibling {
			switch {
			case isHTML(c, "option"):
				opts = append(opts, c)
			case isHTML(c, "optgroup"):
				walk(c, depth+1)
			}
		}
	}
	walk(n, 0)
	var sel *html.Node
	for _, o := range opts {
		if hasAttr(o, "selected") {
			sel = o
		}
	}
	multiple := hasAttr(n, "multiple")
	size, _ := numberAttr(n, "size")
	if sel == nil && !multiple && size <= 1 {
		for _, o := range opts {
			if !disabled(o) {
				sel = o
				break
			}
		}
	}
	if sel == nil {
		return true
	}
	if multiple || size > 1 || len(opts) == 0 || sel != opts[0] || sel.Parent != n {
		return false
	}
	return valueOf(sel) == ""
}
