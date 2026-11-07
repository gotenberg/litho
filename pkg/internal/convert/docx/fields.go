package docx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// maxOpenFields caps the complex fields open at once. Anti-DoS guard, not
// spec-mandated: Word nests fields a few levels (an IF around a REF).
const maxOpenFields = 32

// field is a complex field being read (ECMA-376 Part 1, 17.16.18): its
// instruction, whether its result is being read, and what the converter
// does with the result.
type field struct {
	instr strings.Builder
	// result is set once the separate character is read.
	result bool
	// computed hides the cached result: the converter wrote the value.
	computed bool
	// link is set when the result became a hyperlink.
	link bool
}

// top returns the innermost open field, nil for none.
func (s *story) top() *field {
	if len(s.open) == 0 {
		return nil
	}
	return s.open[len(s.open)-1]
}

// hiding reports whether run content is hidden: a field instruction is
// being read, or the cached result of a field the converter computed.
func (s *story) hiding() bool {
	for _, f := range s.open {
		if !f.result || f.computed {
			return true
		}
	}
	return false
}

// fieldChar handles a w:fldChar (17.16.18): begin opens a field, separate
// starts its result, end closes it. The page fields of a header or
// footer are computed; a HYPERLINK makes its result a link; every other
// field shows its cached result, what Word last rendered.
func (d *document) fieldChar(s *story, c *ooxml.Element, st *css.Style, sink *inlineSink) {
	switch attrOr(c, "fldCharType") {
	case "begin":
		if len(s.open) >= maxOpenFields {
			return
		}
		s.open = append(s.open, &field{})
	case "separate":
		if f := s.top(); f != nil && !f.result {
			d.startResult(s, f, st, sink)
		}
	case "end":
		f := s.top()
		if f == nil {
			return
		}
		if !f.result {
			d.startResult(s, f, st, sink)
		}
		if f.link {
			sink.closeLink()
		}
		s.open = s.open[:len(s.open)-1]
	}
}

// startResult starts the result of field f, writing the value of a field
// the converter computes.
func (d *document) startResult(s *story, f *field, st *css.Style, sink *inlineSink) {
	f.result = true
	instr := f.instr.String()
	if v, ok := d.computeField(s, instr); ok {
		f.computed = true
		sink.add(&layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: v}}})
		return
	}
	if href := hyperlinkTarget(instr); href != "" {
		f.link = true
		sink.openLink(href)
	}
}

// simpleField builds a w:fldSimple (17.16.19): its computed value, else
// its cached result, a link for a HYPERLINK.
func (d *document) simpleField(s *story, rp resolvedPara, ps *css.Style, lineMult float64, e *ooxml.Element, sink *inlineSink, depth int) {
	instr := attrOr(e, "instr")
	if v, ok := d.computeField(s, instr); ok {
		st := inlineStyle(ps)
		props := rp.run
		if r := e.Find(w, "rPr"); r != nil {
			props = props.merge(d.parseRunProps(r))
		}
		d.applyRun(st, props, lineMult)
		sink.add(&layout.Node{Kind: layout.Element, Style: st, Children: []*layout.Node{{Kind: layout.Text, Style: st, Text: v}}})
		return
	}
	if href := hyperlinkTarget(instr); href != "" {
		sink.openLink(href)
		d.inlines(s, rp, ps, lineMult, e, sink, depth+1)
		sink.closeLink()
		return
	}
	d.inlines(s, rp, ps, lineMult, e, sink, depth+1)
}

// computeField returns the value of the page fields in a header or
// footer, false for any other field or outside one (17.16.5.43 PAGE,
// 17.16.5.38 NUMPAGES, 17.16.5.55 SECTIONPAGES, with a \* format switch).
func (d *document) computeField(s *story, instr string) (string, bool) {
	if s.fields == nil {
		return "", false
	}
	words := strings.Fields(instr)
	if len(words) == 0 {
		return "", false
	}
	v := s.fields
	var n int
	format := "decimal"
	switch strings.ToUpper(words[0]) {
	case "PAGE":
		n, format = v.page, v.format
	case "NUMPAGES":
		n = v.pages
	case "SECTIONPAGES":
		n = v.sectionPages
	default:
		return "", false
	}
	for i := 1; i+1 < len(words); i++ {
		if words[i] != `\*` {
			continue
		}
		switch words[i+1] {
		case "roman":
			format = "lowerRoman"
		case "ROMAN":
			format = "upperRoman"
		case "alphabetic":
			format = "lowerLetter"
		case "ALPHABETIC":
			format = "upperLetter"
		case "Arabic", "arabic":
			format = "decimal"
		case "Ordinal", "ordinal":
			format = "ordinal"
		case "CardText", "cardtext":
			format = "cardinalText"
		}
	}
	if format == "" {
		format = "decimal"
	}
	return formatNumber(n, format), true
}

// hyperlinkTarget returns the target of a HYPERLINK field instruction
// (17.16.5.25): its URL, "#" and its bookmark for a \l switch, "" for
// another field.
func hyperlinkTarget(instr string) string {
	args := fieldArgs(instr)
	if len(args) == 0 || !strings.EqualFold(args[0], "HYPERLINK") {
		return ""
	}
	var url, loc string
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case `\l`:
			if i+1 < len(args) {
				loc = args[i+1]
				i++
			}
		case `\o`, `\t`:
			i++
		default:
			if !strings.HasPrefix(args[i], `\`) && url == "" {
				url = args[i]
			}
		}
	}
	switch {
	case url != "" && loc != "":
		return url + "#" + loc
	case url != "":
		return url
	case loc != "":
		return "#" + loc
	}
	return ""
}

// fieldArgs splits a field instruction into its arguments, a quoted one
// unquoted (17.16.1).
func fieldArgs(instr string) []string {
	var out []string
	for i := 0; i < len(instr); {
		switch instr[i] {
		case ' ', '\t':
			i++
		case '"':
			j := strings.IndexByte(instr[i+1:], '"')
			if j < 0 {
				out = append(out, instr[i+1:])
				return out
			}
			out = append(out, instr[i+1:i+1+j])
			i += j + 2
		default:
			j := strings.IndexAny(instr[i:], " \t")
			if j < 0 {
				out = append(out, instr[i:])
				return out
			}
			out = append(out, instr[i:i+j])
			i += j
		}
	}
	return out
}

// pageFormat maps a pgNumType fmt (17.6.12) to a number format.
func pageFormat(f string) string {
	if f == "" {
		return "decimal"
	}
	return f
}
